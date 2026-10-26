package main

import "testing"

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
