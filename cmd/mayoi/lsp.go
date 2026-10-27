package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/highlight"
	"mayoi/internal/lsp"
	"mayoi/internal/remote"
	"mayoi/locales"
)

// lspAnswerFor is how long the editor waits for a server to answer where a
// name is: a server that is still reading the project has nothing to say.
const lspAnswerFor = 10 * time.Second

// lspServersFor is how many servers run at a time: each is a program of
// its own, which a folder of many languages would keep all of.
const lspServersFor = 8

// lspState is the language servers of the window: which programs serve
// which files, and the servers running, with what they say of the files.
type lspState struct {
	// off is set while the servers do not start, as the window turned them
	// off: a folder of a big project takes a while to settle.
	off bool
	// missing are the programs that were not found, which are not looked
	// for again until Mayoi starts again.
	missing map[string]bool
	// servers are the servers running, by the program and the folder they
	// serve: a folder asked for twice has one server.
	servers map[string]*lspServer
	// diags are what the servers published of the files, by URI, and
	// touched whether that changed since the frame before.
	diags   map[string][]lsp.Diagnostic
	touched bool
}

// lspServer is a language server of the window. The main thread makes it
// and follows it; the goroutines it starts post what they have.
type lspServer struct {
	key    string
	preset lsp.Preset
	// root is the folder the server serves, and sent what document was
	// told it at which text.
	root  string
	srv   *lsp.Server
	sent  map[string]lspSent
	start bool   // it was started
	err   string // why it did not start

	mu       sync.Mutex
	stopping bool
}

// lspSent is what a server was last told a document says.
type lspSent struct {
	text    string
	version int
}

// lspMarks are what a server said of the text of an editor, as the text it
// said it of, and where it is in the lines the editor showed last.
type lspMarks struct {
	diags   []lsp.Diagnostic
	version uint64
	first   int
	last    int
	count   int // how many of diags were marked, which bounds same
	marks   []diagMark
}

// same reports whether a server said what these marks are of.
func (m *lspMarks) same(diags []lsp.Diagnostic) bool {
	if len(m.diags) != len(diags) {
		return false
	}
	for i := range diags {
		if m.diags[i].Range != diags[i].Range || m.diags[i].Message != diags[i].Message ||
			lspSeverity(m.diags[i]) != lspSeverity(diags[i]) {
			return false
		}
	}
	return true
}

// lspOnce makes the state of the servers, for a window that has none.
func (a *app) lspOnce() {
	if a.lsp.servers == nil {
		a.lsp.servers = map[string]*lspServer{}
		a.lsp.diags = map[string][]lsp.Diagnostic{}
		a.lsp.missing = map[string]bool{}
	}
}

// lspEnabled reports whether servers start: as the settings say, unless
// the window turned them off.
func (a *app) lspEnabled() bool {
	a.lspOnce()
	return a.settings.LSP && !a.lsp.off
}

// lspPreset returns the program that serves a file: the first preset of
// the file's extension that is installed, nil for a file that none serves,
// or whose server is not installed.
func (a *app) lspPreset(path string) *lsp.Preset {
	a.lspOnce()
	ext := lspExt(path)
	if ext == "" {
		return nil
	}
	for _, p := range lsp.Presets() {
		if !slices.Contains(p.Extensions, ext) {
			continue
		}
		name := p.Command[0]
		if a.lsp.missing[name] {
			continue
		}
		if _, err := exec.LookPath(name); err != nil {
			a.lsp.missing[name] = true
			continue
		}
		return &p
	}
	return nil
}

// lspExt is the extension of a file, as presets name it: without its dot.
func lspExt(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// lspRoot is the folder a file's server serves: the project around it, or
// the folder open.
func (a *app) lspRoot(p *lsp.Preset, path string) string {
	return lsp.Root(path, p.Markers, a.folder())
}

// lspServerOf returns the server that serves a document, nil for none.
func (a *app) lspServerOf(d *document) *lspServer {
	if d.path == "" || remote.Is(d.path) || !a.lspEnabled() {
		return nil
	}
	p := a.lspPreset(d.path)
	if p == nil {
		return nil
	}
	a.lspOnce()
	return a.lsp.servers[p.ID+"|"+a.lspRoot(p, d.path)]
}

// lspStart starts the server of a document, or tells the one that serves
// its folder of the document.
func (a *app) lspStart(d *document) {
	if d.path == "" || remote.Is(d.path) || !a.lspEnabled() {
		return
	}
	p := a.lspPreset(d.path)
	if p == nil {
		return
	}
	if s := a.lspServerOf(d); s != nil {
		if s.srv != nil {
			a.lspSend(s, d)
		}
		return
	}
	if len(a.lsp.servers) >= lspServersFor {
		return
	}
	root := a.lspRoot(p, d.path)
	s := &lspServer{key: p.ID + "|" + root, preset: *p, root: root}
	a.lsp.servers[s.key] = s
	a.lspLaunch(s)
}

// lspLaunch starts a server in the background, and has the main thread
// take the documents it serves.
func (a *app) lspLaunch(s *lspServer) {
	s.start = true
	s.mu.Lock()
	s.stopping = false
	s.mu.Unlock()
	go func() {
		defer recoverCrash()
		srv, err := lsp.Start(context.Background(), s.preset, s.root, lsp.Handler{
			Diagnostics: func(uri string, diags []lsp.Diagnostic) {
				a.post(func() { a.lspTakeDiagnostics(uri, diags) })
			},
			Log: func(level int, text string) {
				if level <= lsp.SeverityWarning {
					a.post(func() { a.fail("%s", locales.LspFailed(s.preset.Name, text)) })
				}
			},
		})
		a.post(func() { a.lspStarted(s, srv, err) })
	}()
}

// lspStarted has the main thread take a server that started, or say why it
// did not, and tell it of the documents open that it serves.
func (a *app) lspStarted(s *lspServer, srv *lsp.Server, err error) {
	a.lspOnce()
	s.start = false
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if a.lsp.servers[s.key] != s {
		// The window stopped the servers while this one started.
		if srv != nil {
			srv.Stop(context.Background())
		}
		return
	}
	if err != nil {
		delete(a.lsp.servers, s.key)
		s.err = err.Error()
		if !stopping {
			a.fail("%s", locales.LspFailed(s.preset.Name, err.Error()))
		}
		a.invalidate()
		return
	}
	if stopping {
		srv.Stop(context.Background())
		return
	}
	s.srv = srv
	for _, d := range a.docs {
		if d.path == "" || remote.Is(d.path) {
			continue
		}
		p := a.lspPreset(d.path)
		if p == nil || p.ID != s.preset.ID || a.lspRoot(p, d.path) != s.root {
			continue
		}
		a.lspSend(s, d)
	}
	a.invalidate()
}

// lspSync tells the servers of the documents changed since they were last
// told. It runs once a frame, after the documents took their text.
func (a *app) lspSync() {
	if len(a.lsp.servers) == 0 {
		return
	}
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.ed == nil || t.ed.document.path == "" || t.ed.document.loading {
				continue
			}
			if s := a.lspServerOf(t.ed.document); s != nil && s.srv != nil {
				a.lspSend(s, t.ed.document)
			}
		}
	}
}

// lspSend tells a server what a document says, opening it there first if
// it does not know it.
func (a *app) lspSend(s *lspServer, d *document) {
	srv := s.srv
	if srv == nil {
		return
	}
	uri, text, version := lsp.URI(d.path), d.buf.String(), int(d.buf.Version())+1
	if s.sent == nil {
		s.sent = map[string]lspSent{}
	}
	if old, ok := s.sent[uri]; ok && old.text == text {
		return
	}
	s.sent[uri] = lspSent{text: text, version: version}
	lang := s.preset.LanguageIDs[lspExt(d.path)]
	if lang == "" {
		lang = lspExt(d.path)
	}
	go func() {
		defer recoverCrash()
		var err error
		if srv.Opened(uri) {
			err = srv.DidChange(uri, text)
		} else {
			err = srv.DidOpen(uri, lang, text)
		}
		if err != nil {
			a.post(func() { a.lspSay(err) })
		}
	}()
}

// lspSaved tells the server of a document that it was written.
func (a *app) lspSaved(d *document) {
	s := a.lspServerOf(d)
	if s == nil || s.srv == nil {
		return
	}
	srv, uri := s.srv, lsp.URI(d.path)
	go func() {
		defer recoverCrash()
		if err := srv.DidSave(uri); err != nil {
			a.post(func() { a.lspSay(err) })
		}
	}()
}

// lspClosed tells the server of a document that the editor closed it, and
// drops what it said of it.
func (a *app) lspClosed(d *document) {
	if d.path == "" {
		return
	}
	if s := a.lspServerOf(d); s != nil && s.srv != nil {
		srv, uri := s.srv, lsp.URI(d.path)
		delete(s.sent, uri)
		go func() {
			defer recoverCrash()
			_ = srv.DidClose(uri)
		}()
	}
	a.lspForget(lsp.URI(d.path))
}

// lspTakeDiagnostics takes what a server published for a file.
func (a *app) lspTakeDiagnostics(uri string, diags []lsp.Diagnostic) {
	a.lspOnce()
	a.lsp.touched = true
	if len(diags) == 0 {
		delete(a.lsp.diags, uri)
		return
	}
	slices.SortStableFunc(diags, func(d, e lsp.Diagnostic) int {
		if d.Range.Start.Line != e.Range.Start.Line {
			return d.Range.Start.Line - e.Range.Start.Line
		}
		return d.Range.Start.Character - e.Range.Start.Character
	})
	a.lsp.diags[uri] = diags
}

// lspForget drops what was said of a file.
func (a *app) lspForget(uri string) {
	a.lspOnce()
	if _, ok := a.lsp.diags[uri]; !ok {
		return
	}
	a.lsp.touched = true
	delete(a.lsp.diags, uri)
}

// lspChanged runs once a frame, after the functions the servers posted: it
// draws the view again when they said something.
func (a *app) lspChanged() {
	if !a.lsp.touched {
		return
	}
	a.lsp.touched = false
	a.invalidate()
}

// lspDiagnostics returns what a server says of a document, as bad as the
// settings have it show.
func (a *app) lspDiagnostics(d *document) []lsp.Diagnostic {
	if d.path == "" || !a.settings.LSP {
		return nil
	}
	a.lspOnce()
	diags := a.lsp.diags[lsp.URI(d.path)]
	if len(diags) == 0 {
		return nil
	}
	worst := lspWorst(a.settings.LspDiagnostics)
	out := make([]lsp.Diagnostic, 0, len(diags))
	for _, dg := range diags {
		if lspSeverity(dg) <= worst {
			out = append(out, dg)
		}
	}
	return out
}

// lspWorst is the worst severity a setting has show, in the protocol's
// numbering.
func lspWorst(name string) int {
	switch name {
	case "error":
		return lsp.SeverityError
	case "warning":
		return lsp.SeverityWarning
	case "info":
		return lsp.SeverityInformation
	}
	return lsp.SeverityHint
}

// lspSeverity is how bad a diagnostic is: a server that does not say means
// an error.
func lspSeverity(d lsp.Diagnostic) int {
	if d.Severity == 0 {
		return lsp.SeverityError
	}
	return d.Severity
}

// lspStop stops the servers, as the window turns them off, and drops what
// they said: they come back as it turns them on.
func (a *app) lspStop() {
	a.lspOnce()
	for _, s := range a.lsp.servers {
		s.mu.Lock()
		s.stopping = true
		s.mu.Unlock()
		if srv := s.srv; srv != nil {
			go srv.Stop(context.Background())
		}
	}
	a.lsp.servers = map[string]*lspServer{}
	a.lsp.diags = map[string][]lsp.Diagnostic{}
	a.lsp.touched = true
}

// lspShutdown stops the servers as the window closes, and waits a moment
// for them: a server still reading its project would hold the files it
// reads.
func (a *app) lspShutdown() {
	a.lspOnce()
	var wg sync.WaitGroup
	for _, s := range a.lsp.servers {
		s.mu.Lock()
		s.stopping = true
		s.mu.Unlock()
		if srv := s.srv; srv != nil {
			wg.Go(func() { srv.Stop(context.Background()) })
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(lspStopFor):
	}
	a.lsp = lspState{}
}

// lspStopFor is how long the window waits for its servers to stop as it
// closes, after which they are killed.
const lspStopFor = 5 * time.Second

// toggleLSP turns the language servers on or off.
func (a *app) toggleLSP() {
	if a.lspEnabled() {
		a.lsp.off = true
		a.lspStop()
		a.tell(note{message: locales.LspOff()})
	} else {
		a.lsp.off = false
		a.lspOn()
	}
}

// lspOn starts the server of the file shown, as the window turns the
// servers on.
func (a *app) lspOn() {
	for _, d := range a.docs {
		a.lspStart(d)
	}
	a.invalidate()
}

// lspSay says what a server could not do: a server with nothing to say of a
// file is not an error the window shows, as the file is still read and
// written.
func (a *app) lspSay(err error) {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, errLSPSilent) {
		return
	}
}

// errLSPSilent is what a request a server did not answer with ends in: a
// server that does not know a file, or one that was stopped.
var errLSPSilent = errors.New("lsp: no answer")

// lspGoToDefinition moves the editor to where the name under the caret is
// defined, in a column of its own if it is in another file.
func (a *app) lspGoToDefinition() {
	d := a.textEditor()
	if d == nil {
		return
	}
	s, pos, ok := a.lspAsk(d, "textDocument/definition")
	if !ok {
		return
	}
	srv, uri := s.srv, lsp.URI(d.path)
	go func() {
		defer recoverCrash()
		ctx, cancel := context.WithTimeout(context.Background(), lspAnswerFor)
		defer cancel()
		locs, err := srv.Definition(ctx, uri, pos)
		if err != nil {
			a.post(func() { a.lspSay(err) })
			return
		}
		a.post(func() { a.lspJump(locs) })
	}()
}

// lspFindReferences lists where the name under the caret is used, in the
// sidebar.
func (a *app) lspFindReferences() {
	d := a.textEditor()
	if d == nil {
		return
	}
	s, pos, ok := a.lspAsk(d, "textDocument/references")
	if !ok {
		return
	}
	srv, uri := s.srv, lsp.URI(d.path)
	go func() {
		defer recoverCrash()
		ctx, cancel := context.WithTimeout(context.Background(), lspAnswerFor)
		defer cancel()
		locs, err := srv.References(ctx, uri, pos)
		if err != nil {
			a.post(func() { a.lspSay(err) })
			return
		}
		a.post(func() { a.lspShowReferences(locs) })
	}()
}

// lspAsk returns the server of the file shown, and the place of its caret, trace writes a line of what the editor does with what a server said, to
// the window's standard error, for finding why a request went nowhere.
// trace writes a line of what the editor does with what a server said, to
// the window's standard error, for finding why a request went nowhere.
func trace(format string, args ...any) {
	if os.Getenv("MAYOI_LSP_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "lsp: "+format+"\n", args...)
	}
}

// lspAsk returns the server of the file shown, and the place of its caret,
// having told the server what the file says: it says so itself when no
// server serves the file.
func (a *app) lspAsk(d *editor, method string) (*lspServer, lsp.Position, bool) {
	s := a.lspServerOf(d.document)
	if s == nil || s.srv == nil {
		switch {
		case !a.settings.LSP:
			a.tell(note{message: locales.LspOff()})
		case s != nil && s.start:
			if s.preset.Name != "" {
				a.tell(note{message: locales.LspStarting(s.preset.Name)})
			} else {
				a.tell(note{message: locales.LspStarting(lspLanguage(d))})
			}
		case s != nil:
			// It was tried, and it did not start: once is enough.
			_, _ = method, s
		default:
			a.tell(note{message: locales.LspNoServer(lspLanguage(d))})
		}
		return nil, lsp.Position{}, false
	}
	a.lspSend(s, d.document)
	pos, ok := lspPosition(d.buf.Snapshot(), d.sel[1])
	return s, pos, ok
}

// lspLanguage is the language of an editor as it names it: its server's, or
// its file's kind.
func lspLanguage(d *editor) string {
	if d.hl != nil {
		if name := highlight.DisplayName(d.hl.Lexer()); name != "" {
			return name
		}
	}
	if d.path != "" {
		return lspExt(d.path)
	}
	return ""
}

// lspDoc returns the document of a path the window has open, nil for none.
func (a *app) lspDoc(path string) *document {
	if i := a.docIndex(path); i >= 0 {
		return a.docs[i]
	}
	return nil
}

// lspJump takes the editor to the first of the places a server answered
// with, opening its file if it is not there.
func (a *app) lspJump(locs []lsp.Location) {
	if len(locs) == 0 {
		a.tell(note{message: locales.LspNoDefinition()})
		return
	}
	loc := locs[0]
	path, err := lsp.Path(loc.URI)
	if err != nil || path == "" {
		a.tell(note{message: locales.LspNoDefinition()})
		return
	}
	if d := a.activeEditor(); d == nil || filepath.Clean(d.path) != filepath.Clean(path) {
		// The file opens in the columns of the window, which it enters
		// in a frame of its own: the caret waits for it there.
		a.openFile(path, true)
		a.pendingLSP = &lspJumpTo{path: path, at: loc.Range.Start, end: loc.Range.End}
		a.invalidate()
		return
	}
	a.focusColumn()
	d := a.activeEditor()
	if d == nil || filepath.Clean(d.path) != filepath.Clean(path) {
		return
	}
	a.moveCaretTo(d, loc.Range.Start, loc.Range.End)
}

// lspJumpTo is where the caret of a file that is opening goes: a place a
// server answered with.
type lspJumpTo struct {
	path string
	at   lsp.Position
	end  lsp.Position
}

// moveCaretTo puts the caret of an editor where a server said, and scrolls
// to it.
func (a *app) moveCaretTo(d *editor, at, stop lsp.Position) {
	snap := d.buf.Snapshot()
	from, ok := lspOffset(snap, at)
	if !ok {
		return
	}
	to, ok := lspOffset(snap, stop)
	if !ok || to <= from {
		to = from
	}
	// The area takes the end of a selection for the caret, which keeps
	// it in view: a name's start is where the editor goes, the name
	// selected around it.
	d.pending = &[2]int{from, to}
	d.sel[0], d.sel[1] = from, to
	d.vscroll.Focus()
	a.focus = paneEditor
	a.invalidate()
}

// placeJump puts the caret where a server said, if a file it named opened
// since the last frame: it runs once a frame, before the frame is drawn.
func (a *app) placeJump() {
	j := a.pendingLSP
	if j == nil {
		return
	}
	d := a.activeEditor()
	if d == nil {
		return
	}
	if filepath.Clean(d.path) != filepath.Clean(j.path) {
		return
	}
	a.pendingLSP = nil
	a.moveCaretTo(d, j.at, j.end)
}

// lspShowReferences lists where a name is used in the sidebar, as the
// matches of a search in the files.
func (a *app) lspShowReferences(locs []lsp.Location) {
	if len(locs) == 0 {
		a.tell(note{message: locales.LspNoReferences()})
		return
	}
	s := &a.inFiles
	s.gen++
	s.re, s.fold = nil, foldQuery{}
	s.files, s.count, s.running, s.stopped, s.err = nil, 0, false, false, ""
	s.key = s.searchKey()
	// Places come in the order of their files, and of the text in each.
	lines := map[string][]string{}
	for _, loc := range locs {
		path, err := lsp.Path(loc.URI)
		if err != nil || path == "" {
			continue
		}
		var file *searchFile
		if i := slices.IndexFunc(s.files, func(f *searchFile) bool { return f.path == path }); i >= 0 {
			file = s.files[i]
		} else {
			file = &searchFile{path: path, rel: a.relOrPath(path)}
			s.files = append(s.files, file)
		}
		m := searchMatch{line: loc.Range.Start.Line, col: loc.Range.Start.Character}
		if d := a.lspDoc(path); d != nil {
			snap := d.buf.Snapshot()
			if loc.Range.Start.Line < snap.LineCount() {
				start, stop := snap.LineRange(loc.Range.Start.Line)
				line := snap.Slice(start, stop)
				if at, ok := lspOffset(snap, loc.Range.Start); ok {
					m.start, m.col = at, at-start
					if to, ok := lspOffset(snap, loc.Range.End); ok && to > at {
						m.text = snap.Slice(at, to)
					}
				}
				col := min(m.col, len(line))
				end := min(len(line), col+len(m.text))
				m.pre, m.hit, m.post = line[:col], line[col:end], line[end:]
			}
		} else {
			// A file the editor does not show: its line, read the once.
			if _, ok := lines[path]; !ok {
				lines[path] = readLines(path)
			}
			if l := lines[path]; loc.Range.Start.Line < len(l) {
				line := l[loc.Range.Start.Line]
				col := min(max(loc.Range.Start.Character, 0), len(line))
				m.pre, m.post = line[:col], line[col:]
			}
		}
		file.matches = append(file.matches, m)
		s.count++
	}
	if len(s.files) == 0 {
		a.tell(note{message: locales.LspNoReferences()})
		return
	}
	s.refs, s.row = true, 0
	s.changed()
	a.window.SidebarClosed, a.window.SidebarView = false, "search"
	a.settings.Changed()
	a.invalidate()
}

// readLines returns the lines of a file, nil for one that cannot be read.
func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
}

// waveUnderline draws a wave from x0 to x1 at y in col: what an editor marks
// a diagnostic of a server with.
func waveUnderline(pt *ui.Painter, x0, x1, y float32, col ui.Color) {
	const period, amp = 4.0, 1.5
	var wave ui.Path
	up := true
	for x := x0; x < x1; x += period / 2 {
		next := min(x+period/2, x1)
		dy := float32(0)
		if up {
			dy = -amp
		}
		if x == x0 {
			wave.MoveTo(x, y+dy)
		}
		wave.LineTo(next, y+dy)
		up = !up
	}
	if x1 > x0 {
		wave.LineTo(x1, y)
	}
	pt.StrokePath(&wave, 1.2, col)
}

// lspHintMessage is what a server said of a line of a document, the first
// of the diagnostics it is of: "" for a line it said nothing of.
func lspHintMessage(a *app, d *editor, snap ui.TextSnapshot, line int) string {
	if line < 0 {
		return ""
	}
	for _, dg := range a.lspDiagnostics(d.document) {
		if dg.Range.Start.Line != line {
			continue
		}
		msg := strings.TrimSpace(dg.Message)
		if i := strings.IndexByte(msg, '\n'); i >= 0 {
			msg = strings.TrimSpace(msg[:i]) + "…"
		}
		if dg.Source != "" {
			msg += " (" + dg.Source + ")"
		}
		return msg
	}
	return ""
}

// observeLSP runs once a frame, after the text of the documents changed:
// it starts the servers of the files that show without one, and tells the
// servers what their files say now.
func (a *app) observeLSP() {
	if !a.settings.LSP {
		return
	}
	for _, col := range a.cols {
		for _, t := range col.tabs {
			if t.ed == nil {
				continue
			}
			d := t.ed.document
			if d.path == "" || remote.Is(d.path) || d.loading {
				continue
			}
			a.lspStart(d)
		}
	}
	a.lspSync()
}

// lspCounts counts the diagnostics of a document by how bad they are:
// errors, then warnings.
func (a *app) lspCounts(d *document) (errors, warnings int) {
	for _, dg := range a.lspDiagnostics(d) {
		switch lspSeverity(dg) {
		case lsp.SeverityError:
			errors++
		case lsp.SeverityWarning:
			warnings++
		}
	}
	return errors, warnings
}

// lspStatus is the status bar's part of the editor's diagnostics: what a
// server found wrong with the file shown, a click on it going to the next,
// and what it said of the caret's line.
func (a *app) lspStatus(d *editor, item func(key, label, tip string, icon *ui.SVG, fn func())) {
	if !a.settings.LSP || d.path == "" || remote.Is(d.path) {
		return
	}
	errors, warnings := a.lspCounts(d.document)
	if errors > 0 {
		item("lsp-errors", strconv.Itoa(errors), locales.LspErrors(errors), iconError, func() { a.lspNext(d, lsp.SeverityError) })
	}
	if warnings > 0 {
		item("lsp-warnings", strconv.Itoa(warnings), locales.LspWarnings(warnings), iconWarning, func() { a.lspNext(d, lsp.SeverityWarning) })
	}
	// What it said of the caret's line, where a wave under the text says
	// that something is wrong but not what.
	snap := d.buf.Snapshot()
	line := snap.LineAt(min(max(d.sel[1], 0), snap.Len()))
	if msg := lspHintMessage(a, d, snap, line); msg != "" {
		item("lsp-message", clipRunes(msg, lspMessageRunes), msg, nil, nil)
	}
}

// lspMessageRunes is the length of a diagnostic in the status bar, past
// which it says no more.
const lspMessageRunes = 72

// clipRunes cuts s to n runes, with an ellipsis for what it cut.
func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// lspNext puts the caret on the next diagnostic as bad as severity, down
// the file, or at the first from the end.
func (a *app) lspNext(d *editor, severity int) {
	diags := a.lspDiagnostics(d.document)
	snap := d.buf.Snapshot()
	caret := min(d.sel[1], snap.Len())
	var at []int
	for _, dg := range diags {
		if dg.Severity != 0 && dg.Severity != severity {
			continue
		}
		if off, ok := lspOffset(snap, dg.Range.Start); ok {
			at = append(at, off)
		}
	}
	if len(at) == 0 {
		return
	}
	slices.Sort(at)
	next := at[0]
	for _, off := range at {
		if off > caret {
			next = off
			break
		}
	}
	d.selectRange(next, next, true)
	d.vscroll.Focus()
	a.focus = paneEditor
}

// lspPosition is where an offset sits in a document, as a server says it.
func lspPosition(snap ui.TextSnapshot, at int) (lsp.Position, bool) {
	n := snap.Len()
	if n == 0 {
		return lsp.Position{}, true
	}
	at = min(max(at, 0), n)
	line := snap.LineAt(at)
	start, _ := snap.LineRange(line)
	col := 0
	for _, r := range snap.Slice(start, at) {
		if r > 0xFFFF {
			col += 2
		} else {
			col++
		}
	}
	return lsp.Position{Line: line, Character: col}, true
}

// lspOffset is the byte offset of a place the protocol gives.
func lspOffset(snap ui.TextSnapshot, pos lsp.Position) (int, bool) {
	if pos.Line < 0 || pos.Line >= snap.LineCount() {
		return 0, false
	}
	start, end := snap.LineRange(pos.Line)
	col, at := 0, start
	for _, r := range snap.Slice(start, end) {
		if col >= pos.Character {
			break
		}
		if r > 0xFFFF {
			col += 2
		} else {
			col++
		}
		at += len(string(r))
	}
	return min(at, end), true
}
