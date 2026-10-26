package ai

import (
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

func TestTranscriptTagsTheRoles(t *testing.T) {
	msgs := Transcript([]Message{
		{Role: "user", Text: "one"},
		{Role: "assistant", Text: "two"},
		{Role: "user", Text: "three"},
	})
	if len(msgs) != 3 {
		t.Fatalf("the transcript has %d messages", len(msgs))
	}
	if _, ok := msgs[0].(piai.UserMessage); !ok {
		t.Errorf("the first is %T", msgs[0])
	}
	if _, ok := msgs[1].(piai.AssistantMessage); !ok {
		t.Errorf("the second is %T", msgs[1])
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
