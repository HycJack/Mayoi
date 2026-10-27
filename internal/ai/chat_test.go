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

func TestTheTwoProtocolsAskTheirOwnService(t *testing.T) {
	ps := Protocols()
	if len(ps) != 2 || ps[0] != OpenAI || ps[1] != Anthropic {
		t.Fatalf("the protocols are %v", ps)
	}
	for _, tc := range []struct{ protocol, base, want string }{
		{OpenAI, "", "https://api.openai.com/v1/models"},
		{Anthropic, "", "https://api.anthropic.com/v1/models"},
		// A base URL is where the protocol's own paths start: a server
		// laid out like OpenAI already ends at its version.
		{OpenAI, "http://localhost:11434/v1/", "http://localhost:11434/v1/models"},
		{Anthropic, "https://proxy.example/v1", "https://proxy.example/v1/models"},
	} {
		if got := ModelsURL(tc.protocol, tc.base); got != tc.want {
			t.Errorf("%s at %q lists at %s, want %s", tc.protocol, tc.base, got, tc.want)
		}
	}
}

func TestATargetSpeaksTheProtocolWhereTheURLPoints(t *testing.T) {
	m, err := Target(Anthropic, "https://proxy.example/", "claude-x")
	if err != nil {
		t.Fatal(err)
	}
	if m.API != piai.APIAnthropicMessages || m.BaseURL != "https://proxy.example" || m.ID != "claude-x" {
		t.Errorf("%s at %s speaks %s", m.ID, m.BaseURL, m.API)
	}
	if m.ContextWindow == 0 || m.MaxTokens == 0 || len(m.Input) == 0 {
		t.Errorf("the model is described as %+v", m)
	}
	if m, err = Target(OpenAI, "", "gpt-x"); err != nil {
		t.Fatal(err)
	}
	if m.API != piai.APIOpenAICompletions || m.BaseURL != DefaultBaseURL(OpenAI) {
		t.Errorf("openai speaks %s at %s", m.API, m.BaseURL)
	}
}

func TestATargetOfNoProtocolOrModelFails(t *testing.T) {
	if _, err := Target("grpc", "", "x"); err == nil {
		t.Error("a protocol neither of the two was taken")
	}
	if _, err := Target(OpenAI, "", "  "); err == nil {
		t.Error("a model that is nothing was taken")
	}
}
