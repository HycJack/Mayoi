package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"mayoi/internal/ai"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/egoist/mygo/ui"
)

// fakeStream replaces the assistant's stream for a test: it says reply
// piece by piece, and reports err after them. A nil reply hangs until the
// context is cancelled, as a model that thinks for long.
func (hs *harness) fakeStream(reply []string, err error) *[]ai.Request {
	hs.t.Helper()
	var sent []ai.Request
	asked := &sent
	a := hs.a
	old := a.stream
	a.stream = func(ctx context.Context, req ai.Request, onText func(string)) (string, error) {
		sent = append(sent, req)
		if reply == nil && err == nil {
			// A model that thinks for long: nothing until it is stopped.
			<-ctx.Done()
			return "", ctx.Err()
		}
		for _, s := range reply {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			onText(s)
		}
		return strings.Join(reply, ""), err
	}
	hs.t.Cleanup(func() { a.stream = old })
	// The assistant asks a model the settings chose, so the keys of this
	// machine play no part in a test.
	oldTarget := a.target
	a.target = func() (piai.Model, string, error) { return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil }
	hs.t.Cleanup(func() { a.target = oldTarget })
	return asked
}

func TestTheAssistantSendsWhatWasTyped(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	sent := hs.fakeStream([]string{"Hello", " there."}, nil)
	a.toggleAI()
	hs.settle()
	hs.tt.Type("what is this")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()

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
	a.toggleAI()
	hs.settle()
	hs.tt.Type("hi")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if a.ai.writing == nil {
		t.Fatal("the assistant is not answering")
	}
	a.run("ai.stop")
	hs.settle()
	if a.ai.writing != nil {
		t.Errorf("still writing: %+v", a.ai)
	}
}
