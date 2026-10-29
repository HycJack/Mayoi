package main

import (
	"testing"

	"mayoi/internal/ai"
	"mayoi/locales"
)

// TestAModelTypedAndSaved: a name typed into the field is kept by Save,
// whether or not Enter was pressed first. Typing it and pressing Save is
// how a model of an endpoint that lists none gets in.
func TestAModelTypedAndSaved(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	a.aiConf.protocol = ai.OpenAI
	hs.settle()
	if err := hs.tt.Click(locales.AiConfModelType()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.tt.Type("qwen3-coder-30b")
	hs.settle()
	if err := hs.tt.Click(locales.ButtonSave()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.AI.Model != "qwen3-coder-30b" {
		t.Errorf("the settings say %q", a.settings.AI.Model)
	}
	if len(a.settings.AI.Models) != 1 || a.settings.AI.Models[0] != "qwen3-coder-30b" {
		t.Errorf("the kept models are %v", a.settings.AI.Models)
	}
}

// TestAModelClickedIntoAndSaved: a click on the field itself takes the
// keyboard there, and what is typed is kept by Save.
func TestAModelClickedIntoAndSaved(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	a.aiConf.protocol = ai.OpenAI
	hs.settle()
	if err := hs.tt.Click(locales.AiConfModelPlaceholder()); err != nil {
		t.Fatalf("the field cannot be clicked: %v", err)
	}
	hs.settle()
	hs.tt.Type("glm-4.6")
	hs.settle()
	if a.aiConf.typed != "glm-4.6" {
		t.Fatalf("the field says %q", a.aiConf.typed)
	}
	if err := hs.tt.Click(locales.ButtonSave()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.AI.Model != "glm-4.6" {
		t.Errorf("the settings say %q", a.settings.AI.Model)
	}
	// It is the top of the list, so that a list of many shows it.
	if len(a.aiConf.listed) > 0 && a.aiConf.listed[0] != "glm-4.6" {
		t.Errorf("the list starts with %q", a.aiConf.listed[0])
	}
}
