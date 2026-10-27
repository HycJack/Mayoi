package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// The tests start a language server of their own: the test binary, run
// again with LSP_FAKE set, which speaks the protocol from its standard
// input and output. No test needs a server installed, and every test is
// the same on every machine.
func TestMain(m *testing.M) {
	if os.Getenv("LSP_FAKE") != "" {
		fakeServer()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeServer is a language server that answers just enough to be one: it
// publishes a diagnostic for a document it is told of, and it knows where
// a name is defined and used.
func fakeServer() {
	in := bufio.NewReader(os.Stdin)
	write := func(v any) {
		if m, ok := v.(message); ok {
			m.JSONRPC = "2.0"
			v = m
		}
		body, err := json.Marshal(v)
		if err != nil {
			return
		}
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(body))
		os.Stdout.Write(body)
	}
	reply := func(id *json.RawMessage, result any) {
		write(message{ID: id, Result: json.RawMessage(mustJSON(result))})
	}
	// hello is a request of the server's own, which the client answers.
	hello := func() {
		raw := json.RawMessage(`"server-wants"`)
		write(message{ID: &raw, Method: "client/registerCapability", Params: json.RawMessage(`{}`)})
	}
	for {
		body, err := readFrame(in)
		if err != nil {
			return
		}
		if log := os.Getenv("LSP_FAKE_LOG"); log != "" {
			if f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				f.Write(append(body, '\n'))
				f.Close()
			}
		}
		var m message
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		switch m.Method {
		case "initialize":
			reply(m.ID, InitializeResult{Capabilities: ServerCapabilities{TextDocumentSync: 2}})
			hello()
		case "initialized":
		case "textDocument/didOpen":
			var p didOpenParams
			if json.Unmarshal(m.Params, &p) == nil {
				write(message{Method: "textDocument/publishDiagnostics", Params: json.RawMessage(mustJSON(map[string]any{
					"uri": p.TextDocument.URI,
					"diagnostics": []Diagnostic{{
						Range:    Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 9}},
						Severity: SeverityError,
						Message:  "unused variable",
					}},
				}))})
			}
		case "textDocument/didChange":
			var p didChangeParams
			if json.Unmarshal(m.Params, &p) == nil {
				// A document with no problem of its own: the list empties.
				write(message{Method: "textDocument/publishDiagnostics", Params: json.RawMessage(mustJSON(map[string]any{
					"uri":         p.TextDocument.URI,
					"diagnostics": []Diagnostic{},
				}))})
			}
		case "textDocument/definition":
			reply(m.ID, []Location{{
				URI:   "file:///elsewhere/defined.go",
				Range: Range{Start: Position{Line: 10, Character: 1}, End: Position{Line: 10, Character: 7}},
			}})
		case "textDocument/references":
			reply(m.ID, []Location{
				{URI: "file:///elsewhere/one.go", Range: Range{Start: Position{Line: 3, Character: 2}, End: Position{Line: 3, Character: 6}}},
				{URI: "file:///elsewhere/two.go", Range: Range{Start: Position{Line: 7, Character: 0}, End: Position{Line: 7, Character: 4}}},
			})
		case "textDocument/never":
			// A server that says nothing, which a request has to survive.
		case "shutdown":
			reply(m.ID, nil)
		case "exit":
			return
		}
	}
}

// mustJSON is what the fake server sends, which cannot fail.
func mustJSON(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(data)
}

// fakePreset is the preset of the server the tests start, logging what it
// is told to path when path is not empty.
func fakePreset(path string) Preset {
	env := map[string]string{"LSP_FAKE": "1"}
	if path != "" {
		env["LSP_FAKE_LOG"] = path
	}
	return Preset{
		ID: "fake", Name: "Fake Server",
		Command:    []string{os.Args[0], "-test.run=^$"},
		Extensions: []string{"fake"},
		Env:        env,
	}
}
