package ai

import (
	"path"
	"strings"
)

// builtinPrograms are the shell's own words that run nothing: cd changes
// the folder of the one shell a command gets, and touches no file.
var builtinPrograms = map[string]bool{"cd": true}

// CommandPrograms are the programs a command runs, by name and lower case:
// the first word of each part of it, the parts split at the operators that
// join them, the quotes and the folder it was given by taken off.
//
// A command may run without a card when every one of its programs is
// allowed. The programs after the first count: `go test && rm -rf x` is not
// a command that only runs go.
func CommandPrograms(command string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(command, func(r rune) bool {
		return r == '&' || r == '|' || r == ';' || r == '\n' || r == '\r'
	}) {
		word, rest := firstWord(part)
		// The names a shell sets before a program are the shell's.
		for word != "" && isAssignment(word) {
			word, rest = firstWord(rest)
		}
		name := programName(word)
		if name == "" || builtinPrograms[name] || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

// firstWord is the word a part of a command starts with, and what follows
// it: a word in quotes is one word, spaces and all, so that a program named
// by a folder with a space in it is still one name.
func firstWord(part string) (word, rest string) {
	part = strings.TrimLeft(part, " \t")
	if part == "" {
		return "", ""
	}
	if q := part[0]; q == '"' || q == '\'' {
		if end := strings.IndexByte(part[1:], q); end >= 0 {
			return part[:end+2], part[end+2:]
		}
		return part, ""
	}
	if end := strings.IndexAny(part, " \t"); end >= 0 {
		return part[:end], part[end:]
	}
	return part, ""
}

// programName is the name of the program a word runs, without the quotes
// that may hold it and the folder that may lead to it.
func programName(word string) string {
	word = strings.Trim(word, `"'`)
	word = strings.ReplaceAll(word, `\`, "/")
	name := strings.ToLower(path.Base(word))
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		name = strings.TrimSuffix(name, ext)
	}
	if name == "." || name == "/" {
		return ""
	}
	return name
}

// isAssignment reports whether a word is a name the shell sets for the one
// command, as FOO=bar is.
func isAssignment(word string) bool {
	name, _, ok := strings.Cut(word, "=")
	if !ok || name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z':
		case i > 0 && c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
