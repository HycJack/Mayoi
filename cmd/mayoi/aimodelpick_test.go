package main

import (
	"errors"
	"strings"
	"testing"

	"mayoi/internal/ai"
	"mayoi/locales"
)

// TestThePanelPicksTheModel: the head of the panel lists the kept models,
// and one of them clicks into the one that answers.
func TestThePanelPicksTheModel(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Models = []string{"faux-1", "faux-2"}
	a.settings.AI.Model = "faux-1"
	a.toggleAI()
	hs.settle()
	hs.tt.Click(locales.AiModelTitle())
	hs.settle()
	if !a.ai.models {
		t.Fatal("the list of models did not open")
	}
	hs.tt.Click("faux-2")
	hs.settle()
	if got := a.settings.AI.Model; got != "faux-2" {
		t.Fatalf("the model is %q", got)
	}
	if a.ai.models {
		t.Error("the list of models stayed open")
	}
}

// TestThePanelModelListKeepsWhatAnswers: a settings file that names a model
// it does not keep still shows that model, so that the list is never empty
// of the one that answers.
func TestThePanelModelListKeepsWhatAnswers(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Models = nil
	a.settings.AI.Model = "faux-3"
	if got := a.aiModelNames(); len(got) != 1 || got[0] != "faux-3" {
		t.Fatalf("the list is %v", got)
	}
}

// TestTheModelListOpensTheSettings: the end of the list is the dialog that
// adds and drops models.
func TestTheModelListOpensTheSettings(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Models = []string{"faux-1"}
	a.toggleAI()
	hs.settle()
	hs.tt.Click(locales.AiModelTitle())
	hs.settle()
	hs.tt.Click(locales.AiModelMore())
	hs.settle()
	if !a.aiConf.open {
		t.Error("the settings did not open")
	}
	if a.ai.models {
		t.Error("the list of models stayed open")
	}
}

// TestAFetchThatFailsStillTakesATypedModel: an endpoint that does not list
// its models is no reason to stop, so the dialog says how to go on and
// keeps what was typed.
func TestAFetchThatFailsStillTakesATypedModel(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Model = "faux-1"
	a.openAISettings()
	hs.settle()
	// What a gateway that has no list answers.
	said := a.aiFetchSaid(&ai.FetchError{URL: "https://x/v1/models", Status: "404 Not Found"})
	if said != locales.AiConfNoModelList() {
		t.Errorf("the failure says %q", said)
	}
	a.aiConf.failed, a.aiConf.fetch = true, said
	if err := hs.tt.Click(locales.AiConfModelPlaceholder()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.tt.Type("typed-one")
	hs.settle()
	a.saveAISettings()
	hs.settle()
	if got := a.settings.AI.Model; got != "typed-one" {
		t.Errorf("the model is %q after a fetch that failed", got)
	}
}

// TestTheFetchFailureNamesItsReason: a failure the dialog cannot turn into
// advice says the status, which is what the user can look up.
func TestTheFetchFailureNamesItsReason(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	said := a.aiFetchSaid(&ai.FetchError{URL: "https://x/v1/models", Status: "500 Internal Server Error"})
	if !strings.Contains(said, "500") {
		t.Errorf("the failure says %q", said)
	}
}

// TestThePanelSaysWhatToFix: a key a gateway refuses and a model it has
// never heard of are said by what the user can change, not by the words
// the gateway used.
func TestThePanelSaysWhatToFix(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Model = "faux-9"
	key := errors.New(`openai: API error 401: {"error":{"message":"invalid api key"}}`)
	if got, want := a.aiSaid(key), locales.AiKeyRefused(); got != want {
		t.Errorf("a refused key says %q", got)
	}
	missing := errors.New("openai: API error 404: {\"error\":{\"message\":\"The model `faux-9` does not exist\"}}")
	if got, want := a.aiSaid(missing), locales.AiModelUnknown("faux-9"); got != want {
		t.Errorf("an unknown model says %q", got)
	}
	// Another failure keeps the words it came with, since the user may know
	// what they mean.
	other := errors.New("openai: API error 418: teapot")
	if got := a.aiSaid(other); got != locales.AiChatFailed(other.Error()) {
		t.Errorf("another failure says %q", got)
	}
}
