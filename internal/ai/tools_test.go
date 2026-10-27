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

func TestWriteAsksBeforeItWrites(t *testing.T) {
	root := aFolder(t)
	var asked []Change
	tools := &Tools{Root: root, Approve: func(c Change) bool {
		asked = append(asked, c)
		return true
	}}
	body, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "hi\n"})
	if got, err := tools.Write(context.Background(), "call-1", body, nil); err != nil || got.IsError {
		t.Fatalf("write: %v %+v", err, got)
	}
	// The path comes back with its links resolved, which on Windows is
	// the long name of a folder the system handed out short.
	want, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(asked) != 1 || asked[0].Path != filepath.Join(want, "new.txt") || string(asked[0].After) != "hi\n" {
		t.Errorf("asked %+v", asked)
	}
	if data, err := os.ReadFile(filepath.Join(root, "new.txt")); err != nil || string(data) != "hi\n" {
		t.Errorf("the file is %q, %v", data, err)
	}
}

func TestARejectedWriteSaysSo(t *testing.T) {
	root := aFolder(t)
	tools := &Tools{Root: root, Approve: func(Change) bool { return false }}
	body, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "hi\n"})
	got, err := tools.Write(context.Background(), "call-1", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.IsError || !strings.Contains(said(t, got), "rejected") {
		t.Errorf("write said %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Error("a rejected write wrote the file")
	}
}

func TestEditReplacesOneOccurrence(t *testing.T) {
	root := aFolder(t)
	tools := &Tools{Root: root}
	body, _ := json.Marshal(map[string]string{"path": "main.go", "old": "println(\"hi\")", "new": "println(\"bye\")"})
	got, err := tools.Edit(context.Background(), "call-1", body, nil)
	if err != nil || got.IsError {
		t.Fatalf("edit: %v %+v", err, got)
	}
	data, _ := os.ReadFile(filepath.Join(root, "main.go"))
	if !strings.Contains(string(data), "println(\"bye\")") {
		t.Errorf("the file is %q", data)
	}
}

func TestEditRefusesTextItCannotFind(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	body, _ := json.Marshal(map[string]string{"path": "main.go", "old": "nothing like this", "new": "x"})
	got, err := tools.Edit(context.Background(), "call-1", body, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsError || !strings.Contains(said(t, got), "not found") {
		t.Errorf("edit said %+v", got)
	}
}

func TestAWriteOutsideTheFolderIsRefused(t *testing.T) {
	root := aFolder(t)
	var asked bool
	tools := &Tools{Root: root, Approve: func(Change) bool { asked = true; return true }}
	body, _ := json.Marshal(map[string]string{"path": "../escape.txt", "content": "hi\n"})
	got, _ := tools.Write(context.Background(), "call-1", body, nil)
	if !got.IsError || asked {
		t.Errorf("write: %+v, asked=%v", got, asked)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "escape.txt")); !os.IsNotExist(err) {
		t.Error("a write escaped the folder")
	}
}

func TestBashIsNotOfferedUnlessCommandsAreAllowed(t *testing.T) {
	tools := &Tools{Root: aFolder(t)}
	for _, tool := range tools.Sets() {
		if tool.Name == "bash" {
			t.Fatal("bash is offered with no Exec")
		}
	}
	tools.Exec = func(ctx context.Context, dir, command string) (string, error) { return "ok", nil }
	var found bool
	for _, tool := range tools.Sets() {
		found = found || tool.Name == "bash"
	}
	if !found {
		t.Error("bash is not offered with an Exec")
	}
}

func TestBashAsksWithTheCommand(t *testing.T) {
	var asked Change
	tools := &Tools{
		Root:    aFolder(t),
		Approve: func(c Change) bool { asked = c; return true },
		Exec:    func(ctx context.Context, dir, command string) (string, error) { return "hi\n", nil },
	}
	got, err := tools.Bash(context.Background(), "call-1", json.RawMessage(`{"command": "echo hi"}`), nil)
	if err != nil || got.IsError {
		t.Fatalf("bash: %v %+v", err, got)
	}
	if asked.Command != "echo hi" {
		t.Errorf("asked %+v", asked)
	}
	if !strings.Contains(said(t, got), "hi") {
		t.Errorf("bash said %q", said(t, got))
	}
}

func TestARejectedCommandRunsNothing(t *testing.T) {
	var ran bool
	tools := &Tools{
		Root:    aFolder(t),
		Approve: func(Change) bool { return false },
		Exec:    func(ctx context.Context, dir, command string) (string, error) { ran = true; return "", nil },
	}
	got, _ := tools.Bash(context.Background(), "call-1", json.RawMessage(`{"command": "rm -rf /"}`), nil)
	if ran {
		t.Error("a rejected command ran")
	}
	if !strings.Contains(said(t, got), "rejected") {
		t.Errorf("bash said %+v", got)
	}
}

func TestALinkOutOfTheFolderIsRefused(t *testing.T) {
	root := aFolder(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("links are not made here: %v", err)
	}
	tools := &Tools{Root: root}
	got, err := tools.Read(context.Background(), "call-1", json.RawMessage(`{"path": "link/secret.txt"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsError || !strings.Contains(said(t, got), "outside") {
		t.Errorf("the read said %q", said(t, got))
	}
	if _, err := Under(root, "link/secret.txt"); err == nil {
		t.Error("the path passes the guard")
	}
}

func TestADriveRelativePathIsRefused(t *testing.T) {
	root := aFolder(t)
	if _, err := Under(root, `C:file.txt`); err == nil {
		t.Error("a drive's own folder passes the guard")
	}
}

func TestAQuotedPathIsThePath(t *testing.T) {
	root := aFolder(t)
	if err := os.WriteFile(filepath.Join(root, "a b.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Under(root, `"a b.txt"`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(got); err != nil {
		t.Errorf("the quoted path is %q", got)
	}
}
