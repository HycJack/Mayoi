package ai

import (
	"encoding/base64"

	"github.com/HycJack/pi-ai-go/core"
)

// Picture is a picture a question comes with, which the model is shown as
// the picture it is: the bytes, and what they are.
type Picture struct {
	// Name is what the picture is called in the conversation, for the
	// words of the question to point at it.
	Name string
	// MIMEType is "image/png", "image/jpeg", "image/gif" or "image/webp".
	MIMEType string
	Data     []byte
}

// userMessage is the question as the model reads it: the text, and the
// pictures it comes with. A question without pictures is the text alone,
// which every model of every protocol takes.
func userMessage(r Run) core.Message {
	if len(r.Pictures) == 0 {
		return core.UserMessage{Role: "user", Content: r.Prompt}
	}
	blocks := make([]core.ContentBlock, 0, len(r.Pictures)+1)
	blocks = append(blocks, core.TextContent{Type: "text", Text: r.Prompt})
	for _, p := range r.Pictures {
		blocks = append(blocks, core.ImageContent{
			Type:     "image",
			Data:     base64.StdEncoding.EncodeToString(p.Data),
			MimeType: p.MIMEType,
		})
	}
	return core.UserMessage{Role: "user", Content: blocks}
}

// WithoutPictures is a conversation as a chat keeps it: the pictures a
// question came with stay in the run that read them, and the file beside
// the chat says what they were. A picture is megabytes of base64, and a
// chat reopened pages later is read for what was said.
func WithoutPictures(msgs []core.Message) []core.Message {
	out := make([]core.Message, 0, len(msgs))
	for _, msg := range msgs {
		user, ok := msg.(core.UserMessage)
		if !ok {
			out = append(out, msg)
			continue
		}
		blocks, ok := user.Content.([]core.ContentBlock)
		if !ok {
			out = append(out, msg)
			continue
		}
		kept := make([]core.ContentBlock, 0, len(blocks))
		for _, b := range blocks {
			if _, picture := b.(core.ImageContent); picture {
				kept = append(kept, core.TextContent{Type: "text", Text: "[a picture]"})
				continue
			}
			kept = append(kept, b)
		}
		user.Content = kept
		out = append(out, user)
	}
	return out
}
