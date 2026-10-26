package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
)

// aFolder is a folder of files a tool can read, as a workspace.
func aFolder(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("main.go", "package main\n\nfunc main() {\n\tprintln(\"hi\")\n}\n")
	write("internal/a/a.go", "package a\n\nfunc A() {}\n")
	return root
}

// said is the text of a tool result.
func said(t *testing.T, r agent.AgentToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, b := range r.Content {
		if tc, ok := b.(core.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestReadReturnsTheFileNumbered(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	got, err := tools.Read(context.Background(), "call-1", json.RawMessage(`{"path": "main.go"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	text := said(t, got)
	if !strings.Contains(text, "1\tpackage main") || !strings.Contains(text, "3\tfunc main() {") {
		t.Errorf("read said %q", text)
	}
	if got.IsError {
		t.Errorf("read failed: %s", text)
	}
}

func TestReadTakesAPage(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	got, err := tools.Read(context.Background(), "call-1", json.RawMessage(`{"path": "main.go", "offset": 3, "limit": 2}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	text := said(t, got)
	if !strings.Contains(text, "3\tfunc main() {") || strings.Contains(text, "package main") {
		t.Errorf("page said %q", text)
	}
}

func TestToolsRefuseAPathOutsideTheFolder(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	for _, path := range []string{"../outside.txt", "/etc/passwd", "internal/../../outside.txt"} {
		body, _ := json.Marshal(map[string]string{"path": path})
		got, err := tools.Read(context.Background(), "call-1", body, nil)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !got.IsError || !strings.Contains(said(t, got), "outside") {
			t.Errorf("%s: read was allowed: %+v", path, got)
		}
	}
}

func TestGlobListsTheFiles(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	got, err := tools.Glob(context.Background(), "call-1", json.RawMessage(`{"pattern": "**/*.go"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	text := said(t, got)
	if !strings.Contains(text, "main.go") || !strings.Contains(text, "internal/a/a.go") {
		t.Errorf("glob said %q", text)
	}
}

func TestGrepFindsTheLines(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	got, err := tools.Grep(context.Background(), "call-1", json.RawMessage(`{"pattern": "func A"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	text := said(t, got)
	if !strings.Contains(text, "internal/a/a.go:3") || !strings.Contains(text, "func A") {
		t.Errorf("grep said %q", text)
	}
}
