package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Server is one language server, running for one folder.
type Server struct {
	// Name is the command that was started, for a message that says which
	// server is not answering.
	Name string
	Root string

	cmd *exec.Cmd
	w   *wire
	// stderr keeps what the server said it could not do, for the error a
	// failed handshake reports: a missing module, a server that refuses
	// a folder.
	stderr *tailBuffer
	// opened are the documents the server was told about, with the text
	// it was last told, so that a change sends only what changed.
	opened map[string]string

	mu     sync.Mutex
	done   chan struct{}
	exited error
}

// tailBuffer keeps the last of what a program wrote, which is what a
// failed start is explained by.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTail(max int) *tailBuffer { return &tailBuffer{max: max} }

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}

// handshakeTimeout bounds the initialize: a server that does not answer in
// this is not going to, and the editor should not wait on it.
const handshakeTimeout = 30 * time.Second

// Start starts a language server: the preset says what to run and what to
// tell it, and root is the folder it serves.
func Start(ctx context.Context, p Preset, root string, h Handler) (*Server, error) {
	command := p.Command
	if len(command) == 0 {
		return nil, errors.New("lsp: no command")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	s := &Server{
		Name:   p.Name,
		Root:   abs,
		stderr: newTail(8 << 10),
		opened: map[string]string{},
		done:   make(chan struct{}),
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = abs
	cmd.Env = envWith(p.Env)
	cmd.Stderr = s.stderr
	hideConsole(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("lsp: %s: %w", command[0], err)
	}
	s.cmd = cmd
	s.w = &wire{w: stdin, pending: map[int]chan message{}, handler: h}
	go s.w.readLoop(stdout)
	go func() {
		s.exited = cmd.Wait()
		close(s.done)
		s.w.fail(errClosed)
	}()

	initCtx, cancel := context.WithTimeout(ctx, handshakeTimeout)
	defer cancel()
	if err := s.initialize(initCtx, p.Options); err != nil {
		s.Stop(context.Background())
		return nil, err
	}
	return s, nil
}

// envWith is Mayoi's environment with the server's own over it.
func envWith(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// initialize is the handshake: what Mayoi is, what it can do, which folder
// this server serves, and the options this server wants.
func (s *Server) initialize(ctx context.Context, options any) error {
	params := InitializeParams{
		ProcessID:             os.Getpid(),
		ClientInfo:            &ClientInfo{Name: "Mayoi"},
		RootURI:               URI(s.Root),
		RootPath:              s.Root,
		WorkspaceFolders:      []WorkspaceFolder{{URI: URI(s.Root), Name: filepath.Base(s.Root)}},
		InitializationOptions: options,
	}
	params.Capabilities.Workspace.WorkspaceFolders = true
	params.Capabilities.TextDocument.Synchronization.DidSave = true
	params.Capabilities.TextDocument.PublishDiagnostics.RelatedInformation = false
	params.Capabilities.TextDocument.Definition.LinkSupport = true
	params.Capabilities.General.PositionEncodings = []string{"utf-16"}
	var out InitializeResult
	if err := s.w.request(ctx, "initialize", params, &out); err != nil {
		return s.explain(err)
	}
	if err := s.w.notify("initialized", struct{}{}); err != nil {
		return s.explain(err)
	}
	return nil
}

// explain adds what the server said of itself to a failure, which is
// usually the reason it did not answer.
func (s *Server) explain(err error) error {
	if ctx := s.stderr.String(); ctx != "" {
		return fmt.Errorf("%s: %w: %s", s.Name, err, firstLine(ctx))
	}
	return fmt.Errorf("%s: %w", s.Name, err)
}

// firstLine is the first line of what a server said, which says the most.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Opened reports whether the server was told of a document, and not told
// it closed.
func (s *Server) Opened(uri string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.opened[uri]
	return ok
}

// DidOpen tells the server that a document is shown, with its text.
func (s *Server) DidOpen(uri, languageID, text string) error {
	s.mu.Lock()
	s.opened[uri] = text
	s.mu.Unlock()
	return s.w.notify("textDocument/didOpen", didOpenParams{TextDocument: TextDocumentItem{
		URI: uri, LanguageID: languageID, Version: 1, Text: text,
	}})
}

// DidChange tells the server what a document became: the one range that
// changed, so that a server which reads changes one by one, and a file of
// megabytes typed in, costs the change rather than the file.
func (s *Server) DidChange(uri, text string) error {
	s.mu.Lock()
	before, opened := s.opened[uri]
	if !opened {
		s.mu.Unlock()
		return nil
	}
	s.opened[uri] = text
	s.mu.Unlock()
	if before == text {
		return nil
	}
	r, newText := changeOf(before, text)
	return s.w.notify("textDocument/didChange", didChangeParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []Change{{Range: r, Text: newText}},
	})
}

// changeOf is the one edit that turns before into after: where they part,
// where they meet again, and what goes between.
func changeOf(before, after string) (*Range, string) {
	start := 0
	for start < len(before) && start < len(after) && before[start] == after[start] {
		start++
	}
	endBefore, endAfter := len(before), len(after)
	for endBefore > start && endAfter > start && before[endBefore-1] == after[endAfter-1] {
		endBefore--
		endAfter--
	}
	r := Range{Start: positionOf(before, start), End: positionOf(before, endBefore)}
	return &r, after[start:endAfter]
}

// DidSave tells the server that a document was written.
func (s *Server) DidSave(uri string) error {
	return s.w.notify("textDocument/didSave", didSaveParams{TextDocument: TextDocumentIdentifier{URI: uri}})
}

// DidClose tells the server that a document is no longer shown.
func (s *Server) DidClose(uri string) error {
	s.mu.Lock()
	delete(s.opened, uri)
	s.mu.Unlock()
	return s.w.notify("textDocument/didClose", didCloseParams{TextDocument: TextDocumentIdentifier{URI: uri}})
}

// Request asks the server something, and reads the answer into out, which
// nil leaves unread. It is the way to a request Mayoi has no type for.
func (s *Server) Request(ctx context.Context, method string, params, out any) error {
	if err := s.w.request(ctx, method, params, out); err != nil {
		return s.explain(err)
	}
	return nil
}

// Definition is where the name at pos is defined: the places of it, which
// a server may give as locations or as links of its own.
func (s *Server) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	var raw json.RawMessage
	if err := s.w.request(ctx, "textDocument/definition", textDocumentPosition{
		TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos,
	}, &raw); err != nil {
		return nil, s.explain(err)
	}
	return locationsOf(raw)
}

// References is where the name at pos is used, its definition included.
func (s *Server) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	var raw []Location
	if err := s.w.request(ctx, "textDocument/references", struct {
		textDocumentPosition
		Context struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		} `json:"context"`
	}{
		textDocumentPosition: textDocumentPosition{
			TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos,
		},
		Context: struct {
			IncludeDeclaration bool `json:"includeDeclaration"`
		}{true},
	}, &raw); err != nil {
		return nil, s.explain(err)
	}
	return raw, nil
}

// locationsOf reads the answer of definition, which is a location, a list
// of them, or a list of links that name their target.
func locationsOf(raw json.RawMessage) ([]Location, error) {
	var list []Location
	if err := json.Unmarshal(raw, &list); err == nil {
		return list, nil
	}
	var one Location
	if err := json.Unmarshal(raw, &one); err == nil && one.URI != "" {
		return []Location{one}, nil
	}
	var links []struct {
		TargetURI   string `json:"targetUri"`
		TargetRange Range  `json:"targetRange"`
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, err
	}
	out := make([]Location, 0, len(links))
	for _, l := range links {
		out = append(out, Location{URI: l.TargetURI, Range: l.TargetRange})
	}
	return out, nil
}

// Stop ends the server: it is asked to shut down, and killed if it does
// not, with whatever it started.
func (s *Server) Stop(ctx context.Context) {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	select {
	case <-s.done:
		return
	default:
	}
	stopCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	s.w.request(stopCtx, "shutdown", nil, nil)
	s.w.notify("exit", nil)
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
		killTree(s.cmd)
		<-s.done
	}
}

// Exited is what the server exited with, if it has.
func (s *Server) Exited() error {
	select {
	case <-s.done:
		return s.exited
	default:
		return nil
	}
}

// URI is the path of a file as the protocol names it: file:///C:/x/y.go on
// Windows, file:///x/y.go elsewhere, with the characters a URL cannot hold
// escaped.
func URI(path string) string {
	slash := filepath.ToSlash(path)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	return u.String()
}

// Path is the file a URI names, the way back from URI.
func Path(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("lsp: %q is not a file", uri)
	}
	p := u.Path
	if runtime.GOOS == "windows" {
		p = strings.TrimPrefix(p, "/")
	}
	if p == "" {
		return "", fmt.Errorf("lsp: %q names no file", uri)
	}
	return filepath.FromSlash(p), nil
}

// hideConsole starts the server without a console window of its own, which
// Windows would otherwise show beside the editor.
func hideConsole(cmd *exec.Cmd) {
	setHideWindow(cmd)
}
