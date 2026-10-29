package main

import (
	"testing"
	"time"

	"mayoi/internal/ai"
)

// TestTheLimitsFallBackToTheirDefaults: a settings file that says nothing
// of the steps or the timeout gets the defaults of internal/ai.
func TestTheLimitsFallBackToTheirDefaults(t *testing.T) {
	hs := newHarness(t, true)
	if got := hs.a.steps(); got != ai.DefaultSteps {
		t.Errorf("the steps are %d", got)
	}
	if got := hs.a.commandTimeout(); got != ai.DefaultCommandTimeout {
		t.Errorf("the timeout is %v", got)
	}
}

// TestTheSettingsKeepTheStepsAndTheTimeout: what the dialog says is what
// the next question and the next command get.
func TestTheSettingsKeepTheStepsAndTheTimeout(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	// A dialog without a model cannot save, and the limits are what this
	// one is about: a model is kept for it.
	a.settings.AI.Model = "faux-1"
	a.openAISettings()
	hs.settle()
	a.aiConf.steps, a.aiConf.seconds = 12, 300
	a.saveAISettings()
	hs.settle()
	if a.settings.AI.MaxSteps != 12 || a.settings.AI.CommandSeconds != 300 {
		t.Fatalf("the settings hold %+v", a.settings.AI)
	}
	if got := a.steps(); got != 12 {
		t.Errorf("the steps are %d", got)
	}
	if got := a.commandTimeout(); got != 5*time.Minute {
		t.Errorf("the timeout is %v", got)
	}
}

// TestTheDialogShowsWhatOneQuestionDoes: the fields say the limits in
// force, defaults included, so that a number kept is the one seen.
func TestTheDialogShowsWhatOneQuestionDoes(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.openAISettings()
	hs.settle()
	if a.aiConf.steps != float64(ai.DefaultSteps) || a.aiConf.seconds != ai.DefaultCommandTimeout.Seconds() {
		t.Errorf("the dialog shows %v steps and %v seconds", a.aiConf.steps, a.aiConf.seconds)
	}
	a.aiConf.open = false
	a.settings.AI.MaxSteps, a.settings.AI.CommandSeconds = 40, 45
	a.openAISettings()
	hs.settle()
	if a.aiConf.steps != 40 || a.aiConf.seconds != 45 {
		t.Errorf("the dialog shows %v steps and %v seconds", a.aiConf.steps, a.aiConf.seconds)
	}
}
