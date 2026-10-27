package ai

import (
	"encoding/json"
	"testing"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/core"
	"github.com/HycJack/pi-ai-go/providers/faux"
)

// fauxModel is a model of the provider that answers without the network.
func fauxModel(t *testing.T) piai.Model {
	t.Helper()
	core.RegisterProvider(core.KnownAPI("faux"), faux.New(), "test")
	t.Cleanup(func() { core.UnregisterProviders("test") })
	return piai.Model{
		ID: "faux-1", API: "faux", Provider: "faux",
		ContextWindow: 100_000, MaxTokens: 4096,
		Input: []piai.Modality{piai.ModalityText},
	}
}

func TestDisplaysSayWhatWasSaidAndWhatRan(t *testing.T) {
	history := []piai.Message{
		piai.UserMessage{Role: "user", Content: "what is in main.go"},
		piai.AssistantMessage{Role: "assistant", Content: []piai.ContentBlock{
			piai.TextContent{Type: "text", Text: "Let me look."},
			piai.ToolCall{Type: "tool_use", ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
		}},
		piai.ToolResultMessage{Role: "toolResult", ToolCallID: "c1", ToolName: "read",
			Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: "package main"}}},
		piai.AssistantMessage{Role: "assistant", Content: []piai.ContentBlock{
			piai.TextContent{Type: "text", Text: "It is a main."},
		}},
	}
	got := Displays(history)
	// The question, the answer that read a file, and the answer that
	// said what it found: the tool's result is not a turn of the chat.
	if len(got) != 3 {
		t.Fatalf("the chat shows %d messages: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Text != "what is in main.go" {
		t.Errorf("the question is %+v", got[0])
	}
	if got[1].Text != "Let me look." || len(got[1].Tools) != 1 || got[1].Tools[0] != "read" {
		t.Errorf("the answer that read is %+v", got[1])
	}
	if got[2].Text != "It is a main." || len(got[2].Tools) != 0 {
		t.Errorf("the answer is %+v", got[2])
	}
}

func TestAnAnswerThatSaidNothingShowsNothing(t *testing.T) {
	got := Displays([]piai.Message{
		piai.AssistantMessage{Role: "assistant", StopReason: piai.StopStop},
	})
	if len(got) != 0 {
		t.Errorf("the chat shows %+v", got)
	}
}

func TestProvidersAndModelsAreListed(t *testing.T) {
	ps := Providers()
	if len(ps) == 0 {
		t.Fatal("no providers")
	}
	found := false
	for _, p := range ps {
		if p == "anthropic" {
			found = true
			if len(Models(p)) == 0 {
				t.Errorf("%s has no models", p)
			}
		}
	}
	if !found {
		t.Errorf("no anthropic among %v", ps)
	}
}

func TestResolveOfAnUnknownModelFails(t *testing.T) {
	if _, err := Resolve("anthropic", "no-such-model"); err == nil {
		t.Fatal("no error")
	}
}
