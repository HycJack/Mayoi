package lsp

import (
	"sort"
	"strings"
)

// Position is a place in a document, as the protocol counts it: a line from
// 0, and the character in it in UTF-16 code units, which is what a server
// indexes by.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is from Start to End, End not included.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is a place in a file, which the server names by URI.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// Diagnostic is what a server says is wrong with a range, or worth
// knowing: its severity says which.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity,omitempty"`
	Code     any    `json:"code,omitempty"`
	Source   string `json:"source,omitempty"`
	Message  string `json:"message"`
}

// Severity is how bad a diagnostic is, in the protocol's numbering.
const (
	SeverityError       = 1
	SeverityWarning     = 2
	SeverityInformation = 3
	SeverityHint        = 4
)

// TextDocumentItem is a document as the server first sees it.
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentIdentifier names a document in a request.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier names a document at a version.
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// textDocumentPosition is a place in a document, for the requests that ask
// about one.
type textDocumentPosition struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// InitializeParams is what Mayoi tells a server of itself. RootURI is
// deprecated in the protocol but every server still reads it, and it is
// what tells a server which folder it is the server of.
type InitializeParams struct {
	ProcessID             int                `json:"processId"`
	ClientInfo            *ClientInfo        `json:"clientInfo,omitempty"`
	RootURI               string             `json:"rootUri"`
	RootPath              string             `json:"rootPath,omitempty"`
	WorkspaceFolders      []WorkspaceFolder  `json:"workspaceFolders,omitempty"`
	Capabilities          ClientCapabilities `json:"capabilities"`
	InitializationOptions any                `json:"initializationOptions,omitempty"`
}

// ClientInfo names the program that asks.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// WorkspaceFolder is a folder a server serves.
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// ClientCapabilities is what Mayoi can do, which a server reads to decide
// how much to send. Mayoi asks for diagnostics of documents it opened, and
// for the places of a name.
type ClientCapabilities struct {
	TextDocument struct {
		Synchronization struct {
			DynamicRegistration bool `json:"dynamicRegistration"`
			DidSave             bool `json:"didSave"`
		} `json:"synchronization"`
		PublishDiagnostics struct {
			RelatedInformation bool `json:"relatedInformation"`
			VersionSupport     bool `json:"versionSupport"`
		} `json:"publishDiagnostics"`
		Definition struct {
			LinkSupport bool `json:"linkSupport"`
		} `json:"definition"`
		References struct{} `json:"references"`
	} `json:"textDocument"`
	Workspace struct {
		WorkspaceFolders bool `json:"workspaceFolders"`
	} `json:"workspace"`
	// General says the position encoding Mayoi counts in, which is UTF-16
	// as the protocol first said.
	General struct {
		PositionEncodings []string `json:"positionEncodings"`
	} `json:"general"`
}

// ServerCapabilities is what a server says it can do, of which Mayoi reads
// whether it accepts changes in the text as it goes.
type ServerCapabilities struct {
	TextDocumentSync any `json:"textDocumentSync"`
	Definition       any `json:"definitionProvider"`
	References       any `json:"referencesProvider"`
}

// InitializeResult is the answer to the handshake.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// Change is one edit of a document: the range it replaces, and what
// replaces it. A change with no range is the whole document.
type Change struct {
	Range *Range `json:"range,omitempty"`
	Text  string `json:"text"`
}

// didOpenParams is what a server is told of a document it has not seen.
type didOpenParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// didChangeParams is what a document became.
type didChangeParams struct {
	TextDocument   VersionedTextDocumentIdentifier `json:"textDocument"`
	ContentChanges []Change                        `json:"contentChanges"`
}

// didSaveParams tells a server a document was written.
type didSaveParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

// didCloseParams tells a server a document is no longer shown.
type didCloseParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// utf16Column is the column of a byte offset in its line, as the protocol
// counts: UTF-16 code units, so that a rune outside the basic plane counts
// two, as it does in the editors the protocol was written for.
func utf16Column(line string, byteOffset int) int {
	if byteOffset <= 0 {
		return 0
	}
	if byteOffset > len(line) {
		byteOffset = len(line)
	}
	n := 0
	for _, r := range line[:byteOffset] {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// byteOffset is where a column the protocol counts starts in its line: the
// way back from utf16Column. A column in the middle of a rune that counts
// two is that rune's end, as a server means the rune there.
func byteOffset(line string, column int) int {
	if column <= 0 {
		return 0
	}
	n := 0
	for i, r := range line {
		if n >= column {
			return i
		}
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return len(line)
}

// offsetOf is the byte offset of a position in text, the way the editor
// keeps them: the text split by "\n", and the line's start added.
func offsetOf(text string, pos Position) int {
	offset := 0
	for i := 0; i < pos.Line; i++ {
		next := strings.IndexByte(text[offset:], '\n')
		if next < 0 {
			return len(text)
		}
		offset += next + 1
	}
	end := strings.IndexByte(text[offset:], '\n')
	if end < 0 {
		end = len(text) - offset
	}
	return offset + byteOffset(text[offset:offset+end], pos.Character)
}

// positionOf is the position of a byte offset in text.
func positionOf(text string, offset int) Position {
	if offset > len(text) {
		offset = len(text)
	}
	if offset < 0 {
		offset = 0
	}
	line := strings.Count(text[:offset], "\n")
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	return Position{Line: line, Character: utf16Column(text[start:offset], offset-start)}
}

// sortDiagnostics puts diagnostics in the order the editor reads them and
// the problems list shows them: by line, then by column.
func sortDiagnostics(diags []Diagnostic) {
	sort.SliceStable(diags, func(i, j int) bool {
		a, b := diags[i].Range.Start, diags[j].Range.Start
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Character < b.Character
	})
}


