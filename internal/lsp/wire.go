// Package lsp speaks the Language Server Protocol to the language servers
// a folder has: it starts one, tells it what the editor shows, and asks it
// where a name is defined and where it is used.
//
// It carries only what Mayoi asks for: the framing of the messages, the
// handshake, the text of the documents the editor shows, and the
// diagnostics the server publishes. A server Mayoi cannot find is not an
// error the user has to answer: the editor works without it.
package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// message is a message of the protocol, either way: a request asks and
// carries an id, a notification only says, a response carries the id of
// the request it answers.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

// responseError is what a server says instead of a result.
type responseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *responseError) Error() string {
	if e.Code == 0 {
		return e.Message
	}
	return fmt.Sprintf("%s (%d)", e.Message, e.Code)
}

// errClosed is what a request fails with once the server is gone.
var errClosed = errors.New("lsp: the server is not running")

// wire is the stream of messages of one server: writes are serialized, and
// the requests waiting for an answer are remembered by their id.
type wire struct {
	w io.Writer
	// write serializes the writes: a header and its body are one message,
	// and two goroutines writing one would interleave them.
	writeMu sync.Mutex

	mu      sync.Mutex
	nextID  int
	pending map[int]chan message
	closed  bool
	// handler reads what the server says without being asked: the
	// diagnostics it publishes, and the errors it logs.
	handler Handler
}

// Handler takes what a server says without being asked.
type Handler struct {
	// Diagnostics is every diagnostic a file has, as the server last
	// published them: an empty slice says the file is well again.
	Diagnostics func(uri string, diags []Diagnostic)
	// Log is a message the server logged, for a user who wants to know
	// why it is not answering.
	Log func(level int, text string)
}

// readLoop reads the messages of a server until it closes, handing each to
// the request that waits for it or to the handler.
func (w *wire) readLoop(r io.Reader) {
	defer w.fail(errClosed)
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		body, err := readFrame(br)
		if err != nil {
			if !errors.Is(err, io.EOF) && !w.isClosed() {
				w.log(1, "lsp: "+err.Error())
			}
			return
		}
		var m message
		if err := json.Unmarshal(body, &m); err != nil {
			w.log(1, "lsp: a message that is not JSON: "+err.Error())
			continue
		}
		w.dispatch(m)
	}
}

// maxFrame bounds a message a server may send: a server that says it sends
// a gigabyte is not one to trust with the memory.
const maxFrame = 64 << 20

// readFrame reads one message: headers, then the body their Content-Length
// gives.
func readFrame(br *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || n < 0 || n > maxFrame {
			return nil, fmt.Errorf("lsp: a length of %q", value)
		}
		length = n
	}
	if length < 0 {
		return nil, errors.New("lsp: a message with no length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}

// dispatch hands a message to whoever waits for it: an answer to a request,
// or, from the server, a notification to the handler, or a request of its
// own, which is answered that Mayoi has nothing for it.
func (w *wire) dispatch(m message) {
	switch {
	case m.Method == "" && m.ID != nil:
		id, ok := idOf(*m.ID)
		if !ok {
			return
		}
		w.mu.Lock()
		ch, waiting := w.pending[id]
		delete(w.pending, id)
		w.mu.Unlock()
		if waiting {
			ch <- m
		}
	case m.Method != "" && m.ID != nil:
		// The server asks: of what Mayoi can do, it can do nothing yet,
		// and saying so keeps the server from waiting.
		w.reply(*m.ID, nil)
	case m.Method != "":
		w.fromServer(m)
	}
}

// fromServer is what the server says without being asked.
func (w *wire) fromServer(m message) {
	switch m.Method {
	case "textDocument/publishDiagnostics":
		var p struct {
			URI         string       `json:"uri"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if json.Unmarshal(m.Params, &p) == nil && p.URI != "" && w.handler.Diagnostics != nil {
			w.handler.Diagnostics(p.URI, p.Diagnostics)
		}
	case "window/logMessage", "window/showMessage":
		var p struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(m.Params, &p) == nil {
			w.log(p.Type, p.Message)
		}
	case "$/progress", "telemetry/event", "window/workDoneProgress/create":
		// Nothing to say of these.
	}
}

// log tells the handler what the server said, if anyone listens.
func (w *wire) log(level int, text string) {
	if w.handler.Log != nil && text != "" {
		w.handler.Log(level, text)
	}
}

// send writes one message.
func (w *wire) send(m message) error {
	m.JSONRPC = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	if _, err := fmt.Fprintf(w.w, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = w.w.Write(body)
	return err
}

// request asks, and waits for the answer, or for ctx to end, or for the
// server to go: the first of those.
func (w *wire) request(ctx context.Context, method string, params any, out any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return errClosed
	}
	w.nextID++
	id := w.nextID
	ch := make(chan message, 1)
	w.pending[id] = ch
	w.mu.Unlock()

	rawID := json.RawMessage(strconv.Itoa(id))
	if err := w.send(message{ID: &rawID, Method: method, Params: raw}); err != nil {
		w.forget(id)
		return err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return errClosed
		}
		if m.Error != nil {
			return m.Error
		}
		if out != nil && len(m.Result) > 0 {
			if err := json.Unmarshal(m.Result, out); err != nil {
				return fmt.Errorf("lsp: %s: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		w.forget(id)
		// The server may still answer: say that the answer is not wanted,
		// as the protocol asks, so that it stops working on it.
		w.cancel(id)
		return ctx.Err()
	}
}

// cancel says that an answer is no longer wanted, as the protocol asks, so
// that the server stops working on it.
func (w *wire) cancel(id int) {
	w.notify("$/cancelRequest", json.RawMessage(`{"id":`+strconv.Itoa(id)+`}`))
}

// reply answers a request of the server.
func (w *wire) reply(id json.RawMessage, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		return
	}
	id = append(json.RawMessage(nil), id...)
	w.send(message{ID: &id, Result: raw})
}

// notify tells the server something, without an answer.
func (w *wire) notify(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return w.send(message{Method: method, Params: raw})
}

// forget drops a request nobody waits for any more.
func (w *wire) forget(id int) {
	w.mu.Lock()
	delete(w.pending, id)
	w.mu.Unlock()
}

// fail ends every request still waiting, with err.
func (w *wire) fail(err error) {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	pending := w.pending
	w.pending = map[int]chan message{}
	w.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
}

// isClosed says whether the wire is done, without waiting for it.
func (w *wire) isClosed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

// idOf reads an id, which the protocol allows to be a number or a string.
func idOf(raw json.RawMessage) (int, bool) {
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	return 0, false
}
