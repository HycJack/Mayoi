package ai

import (
	"context"
	"strings"
	"testing"
)

func TestShSaysWhatTheCommandSaid(t *testing.T) {
	out, err := Sh(context.Background(), t.TempDir(), "echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "hello") {
		t.Errorf("run said %q", out)
	}
}

func TestShReportsAFailure(t *testing.T) {
	if _, err := Sh(context.Background(), t.TempDir(), "exit 3"); err == nil {
		t.Error("a failing command reported success")
	}
}
