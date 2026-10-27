package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mayoi/internal/lsp"
)

// The tests of the language servers start one of their own: this binary,
// run again with MAYOI_FAKE_LSP set, which speaks the protocol from its
// standard input and output. No test needs a server installed.
func lspFakeServer() {
	in := bufio.NewReader(os.Stdin)
	// root is the folder of the window, which the fake server serves.
	var root string
	write := func(v any) {
		body, err := json.Marshal(v)
		if err != nil {
			return
		}
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(body))
		os.Stdout.Write(body)
	}
	reply := func(id *json.RawMessage, result any) {
		write(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	log := func(line string) {
		if path := os.Getenv("MAYOI_FAKE_LSP_LOG"); path != "" {
			if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				fmt.Fprintln(f, line)
				f.Close()
			}
		}
	}
	// The definition of a name of a file is in the file it uses beside
	// it, internal/a/a.go for main.go in the folder of the window: the
	// server is told the file it is at, and answers with the root's.
	for {
		body, err := readLSPFrame(in)
		if err != nil {
			return
		}
		var m struct {
			JSONRPC string           `json:"jsonrpc"`
			ID      *json.RawMessage `json:"id"`
			Method  string           `json:"method"`
			Params  json.RawMessage  `json:"params"`
		}
		if json.Unmarshal(body, &m) != nil {
			continue
		}
		log(m.Method)
		switch m.Method {
		case "initialize":
			root, _ = lsp.Path(mustParams(m.Params, "rootUri"))
			reply(m.ID, map[string]any{"capabilities": map[string]any{"textDocumentSync": 2}})
		case "textDocument/didOpen":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				write(map[string]any{
					"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics",
					"params": map[string]any{
						"uri": p.TextDocument.URI,
						"diagnostics": []map[string]any{{
							"range":    map[string]any{"start": map[string]int{"line": 3, "character": 1}, "end": map[string]int{"line": 3, "character": 9}},
							"severity": 1,
							"message":  "undeclared name: greeting",
							"source":   "fake",
						}},
					},
				})
			}
		case "textDocument/definition":
			// A name is defined in the file of the window the server
			// serves, internal/a/a.go, at its Answer.
			reply(m.ID, []map[string]any{{
				"uri":   lsp.URI(filepath.Join(root, "internal", "a", "a.go")),
				"range": map[string]any{"start": map[string]int{"line": 2, "character": 6}, "end": map[string]int{"line": 2, "character": 12}},
			}})
		case "textDocument/references":
			var p struct {
				TextDocument struct {
					URI string `json:"uri"`
				} `json:"textDocument"`
			}
			json.Unmarshal(m.Params, &p)
			reply(m.ID, []map[string]any{
				{"uri": p.TextDocument.URI, "range": map[string]any{"start": map[string]int{"line": 2, "character": 6}, "end": map[string]int{"line": 2, "character": 12}}},
				{"uri": lsp.URI(filepath.Join(root, "main.go")), "range": map[string]any{"start": map[string]int{"line": 6, "character": 6}, "end": map[string]int{"line": 6, "character": 12}}},
			})
		case "shutdown":
			reply(m.ID, nil)
		case "exit":
			return
		}
	}
}

// mustParams is a string field of the parameters of a message, "" for one
// that has none.
func mustParams(params json.RawMessage, field string) string {
	var m map[string]any
	if json.Unmarshal(params, &m) != nil {
		return ""
	}
	s, _ := m[field].(string)
	return s
}

// readLSPFrame reads one message of the protocol.
func readLSPFrame(in *bufio.Reader) ([]byte, error) {
	length := 0
	for {
		line, err := in.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if v, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			fmt.Sscanf(strings.TrimSpace(v), "%d", &length)
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(in, body); err != nil {
		return nil, err
	}
	return body, nil
}

// fakeLSP has the windows of a test start the fake server for files of the
// extensions named, logging what it is told to a file the test reads.
func (hs *harness) fakeLSP(extensions ...string) (log string) {
	hs.t.Helper()
	log = filepath.Join(hs.t.TempDir(), "lsp.log")
	lsp.UseFake(extensions, []string{os.Args[0], "-test.run=^$"},
		map[string]string{"MAYOI_FAKE_LSP": "1", "MAYOI_FAKE_LSP_LOG": log})
	hs.t.Cleanup(lsp.RestorePresets)
	// The window asked before the server existed, as it starts: it asks
	// again.
	hs.a.settings.LSP = true
	hs.a.lsp.missing = map[string]bool{}
	hs.a.lsp.off = false
	return log
}

// TestALanguageServerSaysWhatIsWrong opens a file whose server says a line
// of it is wrong: the editor marks it, and the status bar counts it.
func TestALanguageServerSaysWhatIsWrong(t *testing.T) {
	hs := newHarness(t, true)
	hs.fakeLSP("go")
	hs.a.openFile(hs.path("main.go"), true)
	waitFor(t, hs, func() bool {
		d := hs.a.activeEditor()
		return d != nil && len(hs.a.lspDiagnostics(d.document)) == 1
	})
	hs.settle()
	d := hs.a.activeEditor()
	marks := d.diagMarks(hs.a, d.buf.Snapshot(), 0, d.buf.LineCount()-1)
	if len(marks) != 1 || marks[0].line != 3 || marks[0].severity != lsp.SeverityError {
		t.Fatalf("the marks of the diagnostics: %+v", marks)
	}
	if !hs.tt.HasText("1") {
		t.Errorf("the status bar does not count the error")
	}
	if errors, warnings := hs.a.lspCounts(d.document); errors != 1 || warnings != 0 {
		t.Errorf("counts: %d errors, %d warnings", errors, warnings)
	}
}

// TestTheEditorGoesToTheDefinition has the server of the file shown say
// where the name at the caret is defined, in another file: the editor
// opens it, at the line the server named.
func TestTheEditorGoesToTheDefinition(t *testing.T) {
	hs := newHarness(t, true)
	hs.fakeLSP("go")
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	hs.settle()
	waitFor(t, hs, func() bool {
		s := a.lspServerOf(a.activeEditor().document)
		return s != nil && s.srv != nil
	})
	a.textEditor().vscroll.Focus()
	hs.settle()
	a.run("go.definition")
	waitFor(t, hs, func() bool { return a.activeEditor().path == hs.path("internal/a/a.go") })
	d := a.activeEditor()
	hs.settle()
	waitFor(t, hs, func() bool { return d.pending == nil })
	snap := d.buf.Snapshot()
	at, _ := lspOffset(snap, lsp.Position{Line: 2, Character: 6})
	if d.sel[0] != at {
		t.Fatalf("the caret is at %d, want %d", d.sel[0], at)
	}
	if got := snap.Slice(d.sel[0], d.sel[1]); got != "Answer" {
		t.Errorf("the jump selects %q", got)
	}
	if snap.LineAt(d.sel[0]) != 2 {
		t.Errorf("the caret is on line %d, want 2", snap.LineAt(d.sel[0]))
	}
}

// TestTheReferencesOfAName lists where the server says a name is used, in
// the sidebar.
func TestTheReferencesOfAName(t *testing.T) {
	hs := newHarness(t, true)
	hs.fakeLSP("go")
	a := hs.a
	a.openFile(hs.path("internal/a/a.go"), true)
	hs.settle()
	waitFor(t, hs, func() bool {
		s := a.lspServerOf(a.activeEditor().document)
		return s != nil && s.srv != nil
	})
	a.textEditor().vscroll.Focus()
	hs.settle()
	a.run("go.references")
	waitFor(t, hs, func() bool { return a.inFiles.refs && a.inFiles.count == 2 })
	if got := a.window.SidebarView; got != "search" {
		t.Fatalf("the sidebar shows %q", got)
	}
	hs.settle()
	if !hs.tt.HasText("2 references") {
		t.Errorf("the view does not count the references")
	}
}

// TestTheEditorWorksWithoutAServer leaves the file without a server that
// knows it: what asks a server says so, quietly, and the editor goes on.
func TestTheEditorWorksWithoutAServer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.LSP = true
	// No server serves Markdown.
	a.openFile(hs.path("README.md"), true)
	hs.settle()
	// A note shows in the frame after it is told, and is taken by it.
	a.run("go.definition")
	hs.tt.Frame()
	d := a.activeEditor()
	if d == nil || d.path != hs.path("README.md") {
		t.Fatalf("the editor left the file")
	}
	if len(a.errors) != 0 {
		t.Errorf("the window says the server is missing: %v", a.errors)
	}
	if !hs.tt.HasText("No language server serves") {
		t.Errorf("the command said nothing")
	}
}

// TestTheServerIsToldOfTheEdits changes a file whose server watches it: the
// server is told, once per frame at most.
func TestTheServerIsToldOfTheEdits(t *testing.T) {
	hs := newHarness(t, true)
	log := hs.fakeLSP("go")
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	waitFor(t, hs, func() bool {
		s := a.lspServerOf(a.activeEditor().document)
		return s != nil && s.srv != nil
	})
	hs.settle()
	d := a.activeEditor()
	d.selectRange(0, 0, false)
	hs.settle()
	hs.tt.Type("// a comment")
	hs.settle()
	if !strings.HasPrefix(d.buf.String(), "// a comment") {
		t.Fatalf("the text is %q", d.buf.String())
	}
	waitFor(t, hs, func() bool {
		data, err := os.ReadFile(log)
		return err == nil && strings.Contains(string(data), "textDocument/didChange")
	})
}

// TestLanguageServersAreTurnedOff turns the servers off: the marks of their
// diagnostics go, and the server of a file opened after does not start.
func TestLanguageServersAreTurnedOff(t *testing.T) {
	hs := newHarness(t, true)
	hs.fakeLSP("go")
	a := hs.a
	a.openFile(hs.path("main.go"), true)
	waitFor(t, hs, func() bool {
		d := a.activeEditor()
		return d != nil && len(a.lspDiagnostics(d.document)) == 1
	})
	a.run("view.lsp")
	hs.settle()
	d := a.activeEditor()
	if got := a.lspDiagnostics(d.document); len(got) != 0 {
		t.Errorf("the diagnostics stand: %+v", got)
	}
	if len(a.lsp.servers) != 0 {
		t.Errorf("the servers stand: %d", len(a.lsp.servers))
	}
	a.run("view.lsp")
	waitFor(t, hs, func() bool { return len(a.lsp.servers) > 0 })
	waitFor(t, hs, func() bool { return len(a.lspDiagnostics(a.activeEditor().document)) == 1 })
	time.Sleep(10 * time.Millisecond)
}
