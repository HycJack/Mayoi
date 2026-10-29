package main

import (
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"mayoi/internal/ai"
	"mayoi/internal/vfs"
	"mayoi/locales"
)

// aiAttachment is a file a question comes with: a picture the model is
// shown, or a text it reads, as the file was when it was attached.
type aiAttachment struct {
	// Path is the file in the machine, Name what the question calls it.
	Path string
	Name string
	// Picture says the file is an image the model is shown; Data its
	// bytes, and MIMEType what they are.
	Picture  bool
	MIMEType string
	Data     []byte
	// Text is the file's text, for a file that is not a picture.
	Text string
}

// The most a question carries: a picture within what the services take, a
// text within what a prompt reads, and a few of each rather than a folder.
const (
	maxPicture  = 4 << 20
	maxText     = 256 << 10
	maxAttached = 6
)

// attach is the most a text file counts in a prompt: the model reads the
// start of a file far longer than any question is worth, with a word at
// the end saying where it stopped.
const attachTextInPrompt = 64 << 10

// readAttachment reads a file the user gave a question: a picture, which
// the model is shown, or a text, which it reads.
func readAttachment(path string) (aiAttachment, error) {
	info, err := os.Stat(path)
	if err != nil {
		return aiAttachment{}, err
	}
	if info.IsDir() {
		return aiAttachment{}, errors.New(locales.AiAttachFolder(path))
	}
	name := filepath.Base(path)
	picture := looksPicture(path)
	// Nothing beyond what a prompt carries is read at all: a file of a
	// hundred megabytes is a mistake, not a question.
	if picture && info.Size() > maxPicture {
		return aiAttachment{}, errors.New(locales.AiAttachTooBig(name, "4 MB"))
	}
	if !picture && info.Size() > maxText {
		return aiAttachment{}, errors.New(locales.AiAttachTooBig(name, "256 KB"))
	}
	data, err := vfs.ReadFile(path)
	if err != nil {
		return aiAttachment{}, err
	}
	if mimeType := pictureType(path, data); mimeType != "" {
		return aiAttachment{Path: path, Name: name, Picture: true, MIMEType: mimeType, Data: data}, nil
	}
	// A file of text is read as it is; one of bytes would be a question
	// about what the model cannot see.
	if !utf8.Valid(data) || bytesHasNUL(data) {
		return aiAttachment{}, errors.New(locales.AiAttachBinary(name))
	}
	return aiAttachment{Path: path, Name: name, Text: string(data)}, nil
}

// looksPicture says whether the name of a file says it is a picture, which
// is a file read whatever its size is, while it is within maxPicture.
func looksPicture(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	}
	return false
}

// pictureType is what the picture at path is, "" for a file that is not
// one: the name it has, and what its bytes say, so that a picture saved
// without its extension is still shown as the picture it is.
func pictureType(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	if !looksPicture(path) {
		return ""
	}
	// The bytes of the file are what the service reads: a name says only
	// which of the four it is meant to be.
	if t, _, err := mime.ParseMediaType(http.DetectContentType(data)); err == nil {
		switch t {
		case "image/png", "image/jpeg", "image/gif", "image/webp":
			return t
		}
	}
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	}
	return "image/webp"
}

// bytesHasNUL reports whether data holds a zero byte, which text of a
// question does not.
func bytesHasNUL(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// pictures are the pictures of what is attached, as the run shows them.
func pictures(attached []aiAttachment) []ai.Picture {
	var out []ai.Picture
	for _, att := range attached {
		if att.Picture {
			out = append(out, ai.Picture{Name: att.Name, MIMEType: att.MIMEType, Data: att.Data})
		}
	}
	return out
}

// attachmentNames are what the question calls the files it comes with, as
// the panel says them under the question.
func attachmentNames(attached []aiAttachment) []string {
	var names []string
	for _, att := range attached {
		names = append(names, att.Name)
	}
	return names
}

// filesNote is what the question says about the files it comes with: the
// names of the pictures the model is shown, and the text of the files it
// reads, each with its path in the folder.
func (a *app) filesNote(attached []aiAttachment) string {
	var pictures []string
	for _, att := range attached {
		if att.Picture {
			pictures = append(pictures, att.Name)
		}
	}
	note := ""
	if len(pictures) > 0 {
		note = "\n\nPictures attached, in this order: " + strings.Join(pictures, ", ") + "."
	}
	if block := filesBlock(attached, func(path string) string { return relTo(a.root, path) }); block != "" {
		note += "\n\n" + block
	}
	return note
}

// relTo is path as the folder root sees it, or its name when it is outside.
func relTo(root, path string) string {
	if root == "" {
		return filepath.Base(path)
	}
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(path)
}

// filesBlock is what the attached files that are not pictures say to the
// model: their path in the folder, and their text, as a block of code of
// the language the name gives.
func filesBlock(attached []aiAttachment, rel func(string) string) string {
	var sb strings.Builder
	for _, att := range attached {
		if att.Picture {
			continue
		}
		text := att.Text
		cut := ""
		if len(text) > attachTextInPrompt {
			text = text[:attachTextInPrompt]
			// A cut in the middle of a character would be a byte the
			// model cannot read.
			for len(text) > 0 && !utf8.ValidString(text) {
				text = text[:len(text)-1]
			}
			cut = "[the file goes on, and the question holds this much of it]"
		}
		sb.WriteString("File: " + rel(att.Path) + "\n```" + languageOf(att.Name) + "\n")
		sb.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			sb.WriteString("\n")
		}
		sb.WriteString("```\n")
		if cut != "" {
			sb.WriteString(cut + "\n")
		}
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// languageOf is the language of a file of code, as its name says, for the
// block of the prompt that holds it.
func languageOf(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".go":
		return "go"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".ts":
		return "typescript"
	case ".tsx", ".jsx":
		return "tsx"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	case ".java":
		return "java"
	case ".c", ".h":
		return "c"
	case ".cc", ".cpp", ".hpp":
		return "cpp"
	case ".cs":
		return "csharp"
	case ".rb":
		return "ruby"
	case ".php":
		return "php"
	case ".sh", ".bash":
		return "bash"
	case ".ps1":
		return "powershell"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".md":
		return "markdown"
	case ".html", ".htm":
		return "html"
	case ".css":
		return "css"
	case ".sql":
		return "sql"
	}
	return ""
}
