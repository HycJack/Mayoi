package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"mayoi/internal/ai"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/egoist/mygo/ui"
)

// fakeStream replaces the assistant's stream for a test: it says reply
// piece by piece, and reports err after them. A nil reply hangs until the
// context is cancelled, as a model that thinks for long, and says reply
// first if it has one.
func (hs *harness) fakeStream(reply []string, err error) *[]ai.Request {
	hs.t.Helper()
	var sent []ai.Request
	asked := &sent
	a := hs.a
	old := a.stream
	a.stream = func(ctx context.Context, req ai.Request, onText func(string)) (string, error) {
		sent = append(sent, req)
		for _, s := range reply {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			onText(s)
		}
		if reply == nil && err == nil {
			// A model that thinks for long: nothing until it is stopped.
			<-ctx.Done()
			return "", ctx.Err()
		}
		return strings.Join(reply, ""), err
	}
	hs.t.Cleanup(func() {
		a.stream = old
		// A stream left waiting would hold the harness's settle up.
		a.ai.cancel()
	})
	// The assistant asks a model the settings chose, so the keys of this
	// machine play no part in a test.
	oldTarget := a.target
	a.target = func() (piai.Model, string, error) { return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil }
	hs.t.Cleanup(func() { a.target = oldTarget })
	return asked
}

// ask types a question into the assistant and presses Enter.
func (hs *harness) ask(question string) {
	hs.t.Helper()
	hs.a.toggleAI()
	hs.settle()
	hs.tt.Type(question)
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
}

func TestTheAssistantToggleOpensAndCloses(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	if a.window.AIOpen {
		t.Fatal("the assistant starts open")
	}
	a.toggleAI()
	hs.settle()
	if !a.window.AIOpen {
		t.Fatal("the assistant did not open")
	}
	a.toggleAI()
	hs.settle()
	if a.window.AIOpen {
		t.Fatal("the assistant did not close")
	}
}

func TestTheAssistantShowsItsEmptyState(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.toggleAI()
	hs.settle()
	if !hs.tt.HasText("Ask about the code") {
		t.Errorf("no empty state: %q", hs.tt.Texts())
	}
}

func TestTheAssistantSaysWhatItNeedsToAnswer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.target = func() (piai.Model, string, error) { return piai.Model{}, "", aiNoKey{} }
	hs.ask("hi")
	if !hs.tt.HasText("Add an API key") {
		t.Errorf("no word about the key: %q", hs.tt.Texts())
	}
	// What was typed stays in the field, to send once the key is there.
	a.target = func() (piai.Model, string, error) {
		return piai.Model{}, "", aiNoKey{wanted: true, provider: "anthropic"}
	}
	a.send()
	hs.settle()
	if !hs.tt.HasText("Add an API key for anthropic") {
		t.Errorf("the provider is not named: %q", hs.tt.Texts())
	}
	a.target = func() (piai.Model, string, error) {
		return piai.Model{}, "", aiUnknownModel{provider: "anthropic", model: "nope"}
	}
	a.send()
	hs.settle()
	if !hs.tt.HasText("anthropic/nope is not a model") {
		t.Errorf("the model is not named: %q", hs.tt.Texts())
	}
}

func TestTheAssistantSendsWhatWasTyped(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	sent := hs.fakeStream([]string{"Hello", " there."}, nil)
	hs.ask("what is this")

	if len(*sent) != 1 {
		t.Fatalf("sent %d requests", len(*sent))
	}
	if got := *sent; got[0].Messages[len(got[0].Messages)-1].(piai.UserMessage).Content != "what is this" {
		t.Errorf("sent %+v", got[0].Messages)
	}
	if len(a.ai.msgs) != 2 || a.ai.msgs[0].role != "user" || a.ai.msgs[0].text != "what is this" {
		t.Fatalf("the conversation is %+v", a.ai.msgs)
	}
	if m := a.ai.msgs[1]; m.role != "assistant" || m.text != "Hello there." {
		t.Errorf("the answer is %+v", m)
	}
	if !hs.tt.HasText("Hello there.") {
		t.Errorf("the answer is not shown: %q", hs.tt.Texts())
	}
}

func TestAssistantFailuresShowUp(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream(nil, errors.New("no route"))
	a.toggleAI()
	hs.settle()
	hs.tt.Type("hi")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if !hs.tt.HasText("no route") {
		t.Errorf("the failure is not shown: %q", hs.tt.Texts())
	}
}

func TestTheAssistantStopsAnAnswer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream(nil, nil) // hangs until cancelled
	hs.ask("hi")
	if a.ai.writing == nil {
		t.Fatal("the assistant is not answering")
	}
	a.run("view.ai.stop")
	hs.settle()
	if a.ai.writing != nil {
		t.Errorf("still writing: %+v", a.ai)
	}
	// The answer keeps its place, marked, rather than being dropped.
	last := a.ai.msgs[len(a.ai.msgs)-1]
	if last.role != "assistant" || !last.stopped {
		t.Errorf("the stopped answer is %+v", last)
	}
	if !hs.tt.HasText("Stopped.") {
		t.Errorf("the stop is not shown: %q", hs.tt.Texts())
	}
}

func TestStoppingKeepsWhatWasSaidAndTakesNoMore(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream(nil, nil) // stubs the model and its key as well
	// It says a word, then waits: the stop must keep the word, and a
	// delta still on its way after the stop must land nowhere.
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	a.stream = func(ctx context.Context, req ai.Request, onText func(string)) (string, error) {
		onText("Half an ")
		<-release
		onText("answer.")
		<-ctx.Done()
		return "Half an answer.", ctx.Err()
	}
	t.Cleanup(letGo)
	a.toggleAI()
	hs.settle()
	hs.tt.Type("hi")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	// The stream never returns, so the harness must not settle on it.
	for i := 0; i < 200 && a.ai.writing == nil; i++ {
		step(hs.tt)
	}
	if a.ai.writing == nil {
		t.Fatal("the assistant is not answering")
	}
	a.run("view.ai.stop")
	for i := 0; i < 5; i++ {
		step(hs.tt)
	}
	said := a.ai.msgs[len(a.ai.msgs)-1]
	if said.text != "Half an " || said.stopped {
		t.Errorf("the stopped answer is %+v", said)
	}
	// The dead streamer's last delta changes nothing.
	letGo()
	for i := 0; i < 20; i++ {
		step(hs.tt)
	}
	if got := a.ai.msgs[len(a.ai.msgs)-1].text; got != "Half an " {
		t.Errorf("a delta landed after the stop: %q", got)
	}
	if len(a.ai.msgs) != 2 {
		t.Errorf("the conversation is %+v", a.ai.msgs)
	}
}
