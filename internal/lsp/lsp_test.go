package lsp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector gathers what a server publishes, as the editor would.
type collector struct {
	mu    sync.Mutex
	diags map[string][]Diagnostic
	logs  []string
}

func (c *collector) handler() Handler {
	return Handler{
		Diagnostics: func(uri string, diags []Diagnostic) {
			c.mu.Lock()
			defer c.mu.Unlock()
			if c.diags == nil {
				c.diags = map[string][]Diagnostic{}
			}
			c.diags[uri] = diags
		},
		Log: func(level int, text string) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.logs = append(c.logs, text)
		},
	}
}

// at is the diagnostics the collector has for uri, waiting a moment for
// them to arrive.
func (c *collector) at(t *testing.T, uri string) []Diagnostic {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		diags, ok := c.diags[uri]
		c.mu.Unlock()
		if ok {
			return diags
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no diagnostics arrived for %s", uri)
	return nil
}

// startFake starts the fake server in dir.
func startFake(t *testing.T, dir string) (*Server, *collector) {
	t.Helper()
	c := &collector{}
	s, err := Start(context.Background(), fakePreset(""), dir, c.handler())
	if err != nil {
		t.Fatalf("the server did not start: %v", err)
	}
	t.Cleanup(func() { s.Stop(context.Background()) })
	return s, c
}

func TestTheHandshakeSaysWhichFolderIsServed(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "what-the-server-heard.log")
	c := &collector{}
	s, err := Start(context.Background(), fakePreset(log), dir, c.handler())
	if err != nil {
		t.Fatalf("the server did not start: %v", err)
	}
	defer s.Stop(context.Background())

	sent := messages(t, log)
	if len(sent) == 0 || sent[0].Method != "initialize" {
		t.Fatalf("the server heard %v first", sent)
	}
	var init InitializeParams
	if err := json.Unmarshal(sent[0].Params, &init); err != nil {
		t.Fatal(err)
	}
	want, err := Path(init.RootURI)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(want, dir) {
		t.Errorf("the root is %q, not %q", want, dir)
	}
	if init.ProcessID != os.Getpid() {
		t.Errorf("the process is %d", init.ProcessID)
	}
	if init.ClientInfo == nil || init.ClientInfo.Name != "Mayoi" {
		t.Errorf("the client is %+v", init.ClientInfo)
	}
	// The handshake goes on once the answer arrives, after which the
	// server may ask something of its own: the notification is there,
	// wherever the asks fall.
	said := false
	for _, m := range sent {
		if m.Method == "initialized" {
			said = true
		}
	}
	if !said {
		t.Errorf("the server was never told the client is ready: %v", sent)
	}
}

// messages is what a server was told, as the fake logged it.
func messages(t *testing.T, path string) []message {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the server heard nothing: %v", err)
	}
	var out []message
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var m message
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

func TestTheDiagnosticsOfAnOpenedFileArrive(t *testing.T) {
	s, c := startFake(t, t.TempDir())
	uri := URI(filepath.Join(t.TempDir(), "a.go"))
	if err := s.DidOpen(uri, "go", "package a\n"); err != nil {
		t.Fatal(err)
	}
	diags := c.at(t, uri)
	if len(diags) != 1 {
		t.Fatalf("the diagnostics are %+v", diags)
	}
	d := diags[0]
	if d.Message != "unused variable" || d.Severity != SeverityError {
		t.Errorf("the diagnostic is %+v", d)
	}
	if d.Range.Start.Line != 2 || d.Range.Start.Character != 4 {
		t.Errorf("it is at %+v", d.Range.Start)
	}
}

func TestAFileThatIsWellAgainHasNoDiagnostics(t *testing.T) {
	s, c := startFake(t, t.TempDir())
	uri := URI(filepath.Join(t.TempDir(), "a.go"))
	if err := s.DidOpen(uri, "go", "package a\n"); err != nil {
		t.Fatal(err)
	}
	c.at(t, uri)
	if err := s.DidChange(uri, "package a\nvar x int\n"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		n := len(c.diags[uri])
		c.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the diagnostics of the file stayed")
}

func TestAChangeIsTheRangeThatChanged(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "heard.log")
	c := &collector{}
	s, err := Start(context.Background(), fakePreset(log), dir, c.handler())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	uri := URI(filepath.Join(dir, "a.txt"))
	before := "one two\nthree four\nfive\n"
	if err := s.DidOpen(uri, "plaintext", before); err != nil {
		t.Fatal(err)
	}
	after := "one two\nthree FOUR\nfive\n"
	if err := s.DidChange(uri, after); err != nil {
		t.Fatal(err)
	}
	var change didChangeParams
	for _, m := range messages(t, log) {
		if m.Method == "textDocument/didChange" {
			if err := json.Unmarshal(m.Params, &change); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(change.ContentChanges) != 1 {
		t.Fatalf("the change is %+v", change)
	}
	ch := change.ContentChanges[0]
	if ch.Text != "FOUR" {
		t.Errorf("the text is %q", ch.Text)
	}
	if ch.Range == nil || ch.Range.Start.Line != 1 || ch.Range.Start.Character != 6 ||
		ch.Range.End.Line != 1 || ch.Range.End.Character != 10 {
		t.Errorf("the range is %+v", ch.Range)
	}
}

func TestADefinitionIsAPlaceInAFile(t *testing.T) {
	s, _ := startFake(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	uri := URI(filepath.Join(t.TempDir(), "a.go"))
	places, err := s.Definition(ctx, uri, Position{Line: 3, Character: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 1 {
		t.Fatalf("the places are %+v", places)
	}
	path, err := Path(places[0].URI)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(path) != "/elsewhere/defined.go" && !strings.HasSuffix(filepath.ToSlash(path), "elsewhere/defined.go") {
		t.Errorf("the place is %q", path)
	}
	if places[0].Range.Start.Line != 10 {
		t.Errorf("it is at %+v", places[0].Range.Start)
	}
}

func TestTheReferencesOfANameAreWhereItIsUsed(t *testing.T) {
	s, _ := startFake(t, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	places, err := s.References(ctx, URI(filepath.Join(t.TempDir(), "a.go")), Position{Line: 1, Character: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(places) != 2 {
		t.Fatalf("the places are %+v", places)
	}
	if !strings.HasSuffix(places[1].URI, "two.go") {
		t.Errorf("the second place is %q", places[1].URI)
	}
}

func TestARequestTheServerNeverAnswersEnds(t *testing.T) {
	dir := t.TempDir()
	s, err := Start(context.Background(), fakePreset(""), dir, (&collector{}).handler())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	err = s.w.request(ctx, "textDocument/never", nil, nil)
	if err == nil {
		t.Fatal("the request came back with no answer")
	}
	if !strings.Contains(err.Error(), "deadline") && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("the request failed with %v", err)
	}
}

func TestAStandedRequestFailsTheOthers(t *testing.T) {
	dir := t.TempDir()
	s, err := Start(context.Background(), fakePreset(""), dir, (&collector{}).handler())
	if err != nil {
		t.Fatal(err)
	}
	killTree(s.cmd)
	select {
	case <-s.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not end")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := s.Definition(ctx, URI(filepath.Join(dir, "a.go")), Position{}); err == nil {
		t.Fatal("a request went through a server that is not running")
	}
}

func TestAServerThatAsksIsAnswered(t *testing.T) {
	// The fake asks for a capability as soon as it is initialized, and
	// waits for the answer as it goes on serving: a client that ignored
	// the request would still be answered here, but one that confused it
	// with a response would never open a document.
	s, c := startFake(t, t.TempDir())
	uri := URI(filepath.Join(t.TempDir(), "a.go"))
	if err := s.DidOpen(uri, "go", "package a\n"); err != nil {
		t.Fatal(err)
	}
	c.at(t, uri)
}

func TestTheURIOfAPathAndThePathOfAURI(t *testing.T) {
	paths := []string{`/home/me/a b/c.go`}
	if runtime.GOOS == "windows" {
		paths = []string{`C:\Users\me\a b\c.go`, `C:\a\b#c?d.go`}
	}
	for _, path := range paths {
		uri := URI(path)
		if !strings.HasPrefix(uri, "file://") {
			t.Errorf("%q is %q", path, uri)
		}
		if strings.ContainsAny(uri, " #?") {
			t.Errorf("%q is not escaped: %q", path, uri)
		}
		back, err := Path(uri)
		if err != nil {
			t.Fatalf("%q: %v", uri, err)
		}
		if filepath.ToSlash(back) != filepath.ToSlash(path) {
			t.Errorf("%q went to %q and came back as %q", path, uri, back)
		}
	}
}

func TestAPositionCountsUTF16Units(t *testing.T) {
	text := "aé中😀b\nsecond\n"
	// 😀 is two units of UTF-16, so b is at column 1+1+1+2 = 5.
	pos := positionOf(text, strings.Index(text, "b"))
	if pos.Line != 0 || pos.Character != 5 {
		t.Errorf("b is at %+v", pos)
	}
	if got := offsetOf(text, pos); got != strings.Index(text, "b") {
		t.Errorf("that offset is %d, not %d", got, strings.Index(text, "b"))
	}
	// A column inside the emoji is its end, since no rune starts there.
	inside := offsetOf(text, Position{Line: 0, Character: 4})
	if inside != strings.Index(text, "😀")+4 {
		t.Errorf("the column inside the emoji is %d", inside)
	}
	if p := positionOf(text, len(text)-1); p.Line != 1 || p.Character != 6 {
		t.Errorf("the end is at %+v", p)
	}
}

func TestTheServersOfAFileAreAskedForInOrder(t *testing.T) {
	got := For("/x/main.py")
	if len(got) != 2 || got[0].ID != "pyright" || got[1].ID != "ruff" {
		t.Fatalf("the servers of a Python file are %v", got)
	}
	if id := got[0].LanguageID("/x/main.py"); id != "python" {
		t.Errorf("the language is %q", id)
	}
	if got := For("/x/notes.txt"); len(got) != 0 {
		t.Errorf("a text file has the servers %v", got)
	}
	if got := For("/x/App.tsx"); len(got) != 1 || got[0].LanguageID("/x/App.tsx") != "typescriptreact" {
		t.Errorf("a .tsx file is %v", got)
	}
}

func TestTheRootIsTheFolderWithTheMarker(t *testing.T) {
	dir := t.TempDir()
	project := filepath.Join(dir, "project")
	deep := filepath.Join(project, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(deep, "main.go")
	got := Root(file, []string{"go.mod"}, dir)
	if !strings.EqualFold(got, project) {
		t.Errorf("the root is %q, not %q", got, project)
	}
	// With no marker, the folder the editor has open.
	if got := Root(file, []string{"Cargo.toml"}, dir); !strings.EqualFold(got, dir) {
		t.Errorf("with no marker the root is %q", got)
	}
}

func TestAServerThatIsNotInstalledIsPassedOver(t *testing.T) {
	p := Preset{ID: "none", Command: []string{"a-server-that-is-not-installed-anywhere"}}
	if p.Installed() {
		t.Error("a server that is not there is said to be")
	}
	if _, ok := Best("/x/a.fake"); ok {
		t.Error("a file whose server is not installed has one")
	}
}
