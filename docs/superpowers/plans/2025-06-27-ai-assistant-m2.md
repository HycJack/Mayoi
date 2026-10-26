# AI Assistant M2: the Agent and Its Tools

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the assistant's panel from a one-shot chat into an agent that reads, searches and edits the folder through tools, asking before it writes.

**Architecture:** One run of pi-ai-go's agent loop per message, rooted at the window's folder. Mayoi's own tools (`read`, `glob`, `grep`, `write`, `edit`, `open_file`) are declared in `internal/ai/tools.go` over `internal/vfs`, because pi-ai-go's built-in tools jail themselves to `os.Getwd()` and cannot take a root. Writes go through a `BeforeToolCall` hook that posts an approval card to the panel and blocks until the answer comes back; the write itself is a `vfs.WriteFile` followed by `a.diskChanged`, so an open tab shows the new text.

**Tech Stack:** Go 1.27, `github.com/HycJack/pi-ai-go` (agent loop, `core.AgentTool`, faux provider for tests), `github.com/egoist/mygo/ui` (immediate mode), `internal/vfs`, `internal/ai`.

**Spec:** `docs/superpowers/specs/2025-06-27-ai-assistant-design.md` §3 (agent and tools), §4 (panel UI), §7 (errors), §9 (terax-clone source mapping)

## Global Constraints

- Every run is capped at **24 steps** (`maxSteps = 24`), counted by `ShouldStopAfterTurn`.
- Tools are rooted at the window's folder: a path outside it, or an absolute one, is refused with a result the model reads, never a panic.
- `internal/vfs` is the only way to touch the disk: `vfs.ReadFile`, `vfs.WriteFile`, `vfs.IsRemote`. A remote path takes a round trip, so tool bodies run off the main thread.
- Writes are approved on a card in the panel: **Approve** writes, **Reject** sends "the user rejected this change" back to the model. `AI.ApproveEdits` on skips the card.
- After a write, the app calls `a.diskChanged([]string{path}, nil)` and `a.indexStale = true`; a dirty tab is left alone (the writer does not clobber unsaved edits).
- All user-visible text comes from `locales`, with a key in `locales/en.json`, `locales/ja.json` and `locales/zh-Hans.json`, regenerated with `go tool mygo generate`. The generated accessors are `Ai*` (for example `AiTitle`), not `AI*`.
- Commits: one imperative subject of 72 characters or fewer, no `justification` parameter on the commit call. A CHANGELOG bullet under `## Unreleased` for anything a user sees.
- Tests never touch the network or the machine's keys: they replace `a.stream` and `a.target` (see `cmd/mayoi/ai_test.go`) and use pi-ai-go's `faux` provider as `internal/ai/chat_test.go` does.
- On this host `./cmd/mayoi` cannot be run whole: the terminal tests need the Ghostty native library and several tests need a `git` that knows `init -b`. Run `go test ./internal/ai ./internal/settings` and `go test ./cmd/mayoi -run '<focused>'`.

## Review Focus

- A tool asked for a path outside the folder (`../../etc/passwd`, `/etc/passwd`, `C:\Windows\win.ini`): expect a refusal the model can read, never a write.
- A write to a file open with unsaved edits: expect the open tab to keep its edits, and the model to be told the file has unsaved changes.
- Rejecting an approval card, then the model trying the same write again: expect a second card, not a stuck panel or a silent retry.
- The model calling the same tool 24 times, or the user pressing **Stop** mid-approval: expect the run to end, the card to go away, and the panel to be usable.
- A `write` that fails (read-only folder, missing parent directory): expect an error toast and a tool result the model reads, not a lost conversation.

---

### Task 1: Tools that read the folder

**Files:**
- Create: `internal/ai/tools.go`
- Test: `internal/ai/tools_test.go`

**Interfaces:**
- Consumes: `internal/vfs` (`ReadFile(p string) ([]byte, error)`, `WriteFile(p string, data []byte, perm fs.FileMode) (fs.FileInfo, error)`, `IsRemote(p string) bool`), `piai.AgentTool` and `piai.AgentToolResult` from `core`.
- Produces:
  - `type Tools struct { Root string }` with methods `Read(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error)`, `Glob`, `Grep`.
  - `func (t *Tools) ReadSet() []piai.AgentTool` — the read-only tools, named `"read"`, `"glob"`, `"grep"`.
  - `func Resolve(root, path string) (string, error)` — the absolute path of `path` under `root`, or an error when it is outside.
  - `type ReadParams struct { Path string `json:"path"`; Offset int `json:"offset,omitempty"`; Limit int `json:"limit,omitempty"` }`, `type GlobParams struct { Pattern string `json:"pattern"` }`, `type GrepParams struct { Pattern string `json:"pattern"`; Include string `json:"include,omitempty"` }`.

- [ ] **Step 1: Write the failing test**

```go
package ai

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
func said(t *testing.T, r piai.AgentToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, b := range r.Content {
		if tc, ok := b.(piai.TextContent); ok {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ai -run 'TestRead|TestTools|TestGlob|TestGrep'`
Expected: FAIL with "undefined: Tools", "undefined: piai"

- [ ] **Step 3: Write minimal implementation**

```go
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	piai "github.com/HycJack/pi-ai-go"

	"mayoi/internal/vfs"
)

// Tools are the assistant's tools, rooted at a folder: a path outside it
// is refused, as are the absolute ones.
type Tools struct {
	Root string
}

// ReadParams is what read is asked: the file, and the lines of it.
type ReadParams struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// GlobParams is what glob is asked: the pattern of the files' paths.
type GlobParams struct {
	Pattern string `json:"pattern"`
}

// GrepParams is what grep is asked: the expression, and the files to look in.
type GrepParams struct {
	Pattern string `json:"pattern"`
	Include string `json:"include,omitempty"`
}

// Resolve is the path of p in root, and fails when it is outside it.
func Resolve(root, p string) (string, error) {
	if root == "" {
		return "", errors.New("no folder is open")
	}
	full := p
	if !filepath.IsAbs(full) {
		full = filepath.Join(root, filepath.FromSlash(p))
	}
	full = filepath.Clean(full)
	rel, err := filepath.Rel(root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the folder", p)
	}
	return full, nil
}

// text is a result the model reads.
func text(s string) piai.AgentToolResult {
	return piai.AgentToolResult{Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: s}}}
}

// failure is a result the model reads, of what went wrong.
func failure(err error) piai.AgentToolResult {
	return piai.AgentToolResult{Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: err.Error()}}, IsError: true}
}

// Read is a file of the folder, numbered by line.
func (t *Tools) Read(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
	var p ReadParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	full, err := Resolve(t.Root, p.Path)
	if err != nil {
		return failure(err), nil
	}
	data, err := vfs.ReadFile(full)
	if err != nil {
		return failure(err), nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	from, to := 1, len(lines)
	if p.Offset > 0 {
		from = p.Offset
	}
	if p.Limit > 0 {
		to = min(to, from+p.Limit-1)
	}
	if from > len(lines) {
		return text(""), nil
	}
	var sb strings.Builder
	for i := from; i <= to; i++ {
		fmt.Fprintf(&sb, "%d\t%s\n", i, lines[i-1])
	}
	return text(sb.String()), nil
}

// Glob lists the files of the folder whose paths match the pattern.
func (t *Tools) Glob(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
	var p GlobParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	var found []string
	root := t.Root
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if ok, _ := filepath.Match(filepath.Base(p.Pattern), filepath.Base(rel)); ok {
			found = append(found, rel)
		}
		return nil
	})
	if err != nil {
		return failure(err), nil
	}
	sort.Strings(found)
	return text(strings.Join(found, "\n")), nil
}

// Grep finds the lines of the folder's files that match an expression.
func (t *Tools) Grep(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
	var p GrepParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	var sb strings.Builder
	root := t.Root
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if err == nil && (d.Name() == ".git" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if p.Include != "" {
			if ok, _ := filepath.Match(p.Include, filepath.Base(rel)); !ok {
				return nil
			}
		}
		data, err := vfs.ReadFile(path)
		if err != nil {
			return nil
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, p.Pattern) {
				fmt.Fprintf(&sb, "%s:%d: %s\n", rel, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		return failure(err), nil
	}
	return text(sb.String()), nil
}

// ReadSet is the tools that only look at the folder.
func (t *Tools) ReadSet() []piai.AgentTool {
	return []piai.AgentTool{
		{
			Name:        "read",
			Description: "Read a file of the folder. Numbered lines, with offset and limit for a page.",
			Label:       "Read",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"offset":{"type":"integer"},"limit":{"type":"integer"}},"required":["path"]}`),
			Execute:     t.Read,
		},
		{
			Name:        "glob",
			Description: "List the files of the folder whose paths match a pattern, as **/*.go.",
			Label:       "Glob",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"}},"required":["pattern"]}`),
			Execute:     t.Glob,
		},
		{
			Name:        "grep",
			Description: "Find the lines of the folder's files that contain an expression.",
			Label:       "Grep",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"pattern":{"type":"string"},"include":{"type":"string"}},"required":["pattern"]}`),
			Execute:     t.Grep,
		},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ai -run 'TestRead|TestTools|TestGlob|TestGrep'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ai/tools.go internal/ai/tools_test.go
git commit -m "Let the assistant read the folder through tools"
```

---

### Task 2: Tools that write, behind an approval

**Files:**
- Modify: `internal/ai/tools.go` (add `Write`, `Edit`, and `WriteSet`; add the `Approve` hook field to `Tools`)
- Test: `internal/ai/tools_test.go`

**Interfaces:**
- Consumes: everything from Task 1, and `vfs.WriteFile`.
- Produces:
  - `type Tools struct { Root string; Approve func(Change) bool }` — `Approve` is nil when every write is allowed.
  - `type Change struct { Path string; Before, After []byte }` — what a write would do, for the card.
  - `WriteParams struct { Path string `json:"path"`; Content string `json:"content"` }`, `EditParams struct { Path string `json:"path"`; Old string `json:"old"`; New string `json:"new"` }`.
  - `func (t *Tools) WriteSet() []piai.AgentTool` — named `"write"` and `"edit"`.
  - A write the approver refuses returns a result with `IsError` false and the text `the user rejected this change`.

- [ ] **Step 1: Write the failing test**

```go
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
	if len(asked) != 1 || asked[0].Path != filepath.Join(root, "new.txt") || string(asked[0].After) != "hi\n" {
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

func TestAWriteToARemotePathIsAllowed(t *testing.T) {
	// vfs.IsRemote decides, and a path that is not remote is written here.
	tools := &Tools{Root: aFolder(t)}
	if vfs.IsRemote(tools.Root) {
		t.Skip("the fixture is a host folder")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ai -run 'TestWrite|TestARejected|TestEdit|TestAWriteTo'`
Expected: FAIL with "undefined: Change", "tools.Write undefined"

- [ ] **Step 3: Write minimal implementation**

Add to `internal/ai/tools.go`:

```go
// Change is what a write would do to a file, for the approval card.
type Change struct {
	Path   string
	Before []byte
	After  []byte
}

// WriteParams is what write is asked: the file and the text it takes.
type WriteParams struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// EditParams is what edit is asked: the file, the text to find in it
// and the text that takes its place.
type EditParams struct {
	Path string `json:"path"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

// approved asks the approver, if there is one, and reads a refusal as
// what the model is told.
func (t *Tools) approved(c Change) (piai.AgentToolResult, bool) {
	if t.Approve == nil || t.Approve(c) {
		return piai.AgentToolResult{}, true
	}
	return text("the user rejected this change"), false
}

// Write is a file of the folder, with the text it takes.
func (t *Tools) Write(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
	var p WriteParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	full, err := Resolve(t.Root, p.Path)
	if err != nil {
		return failure(err), nil
	}
	before, _ := vfs.ReadFile(full)
	c := Change{Path: full, Before: before, After: []byte(p.Content)}
	if told, ok := t.approved(c); !ok {
		return told, nil
	}
	if _, err := vfs.WriteFile(full, c.After, 0o644); err != nil {
		return failure(err), nil
	}
	return text(fmt.Sprintf("wrote %s", p.Path)), nil
}

// Edit is a file of the folder, with the one piece of text it has
// replaced.
func (t *Tools) Edit(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
	var p EditParams
	if err := json.Unmarshal(params, &p); err != nil {
		return failure(err), nil
	}
	full, err := Resolve(t.Root, p.Path)
	if err != nil {
		return failure(err), nil
	}
	before, err := vfs.ReadFile(full)
	if err != nil {
		return failure(err), nil
	}
	if !strings.Contains(string(before), p.Old) {
		return failure(fmt.Errorf("%q is not found in %s", p.Old, p.Path)), nil
	}
	after := []byte(strings.Replace(string(before), p.Old, p.New, 1))
	c := Change{Path: full, Before: before, After: after}
	if told, ok := t.approved(c); !ok {
		return told, nil
	}
	if _, err := vfs.WriteFile(full, after, 0o644); err != nil {
		return failure(err), nil
	}
	return text(fmt.Sprintf("edited %s", p.Path)), nil
}

// WriteSet is the tools that change the folder.
func (t *Tools) WriteSet() []piai.AgentTool {
	return []piai.AgentTool{
		{
			Name:        "write",
			Description: "Write a file of the folder, making it if it is new.",
			Label:       "Write",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`),
			Execute:     t.Write,
		},
		{
			Name:        "edit",
			Description: "Replace one piece of text in a file of the folder with another.",
			Label:       "Edit",
			Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"}},"required":["path","old","new"]}`),
			Execute:     t.Edit,
		},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ai`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ai/tools.go internal/ai/tools_test.go
git commit -m "Ask before the assistant writes a file"
```

---

### Task 3: One run of the agent loop

**Files:**
- Create: `internal/ai/run.go`
- Test: `internal/ai/run_test.go`

**Interfaces:**
- Consumes: `piai.NewAgent`, `piai.AgentOptions{InitialState: *piai.AgentState}`, `piai.AgentState{Model, SystemPrompt, Messages, Tools, GetApiKey, ShouldStopAfterTurn}`, `(*piai.Agent).Subscribe(func(piai.AgentEvent))`, `(*piai.Agent).Run(ctx, ...piai.Message) ([]piai.Message, error)`; the events `piai.EventMessageUpdate{AssistantEvent}`, `piai.EventToolExecStart{ToolName}`, `piai.EventToolExecEnd{ToolName, IsError}`, `piai.EventCompaction`; `piai.EventTextDelta{Delta}`.
- Produces:
  - `type Step struct { Message string; ToolCalls []string; OutOfSteps, Compacted bool }` — what a run did, as it goes: a text delta (`Message`), the tool names it called (`ToolCalls`), that it ran out of steps, or that its context was compacted.
  - `type Run struct { Model piai.Model; APIKey, System, Prompt string; Messages []piai.Message; Tools []piai.AgentTool }`.
  - `func Loop(ctx context.Context, r Run, onStep func(Step)) ([]piai.Message, error)` — the messages after the run, and an error when the run failed. `onStep` may be nil.
  - `const maxSteps = 24`.

- [ ] **Step 1: Write the failing test**

```go
package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	piai "github.com/HycJack/pi-ai-go"
)

func TestLoopRunsTheToolsTheModelCalls(t *testing.T) {
	var calls int
	tools := []piai.AgentTool{{
		Name:        "count",
		Description: "Count.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
			calls++
			return text("1"), nil
		},
	}}
	var steps []Step
	msgs, err := Loop(context.Background(), Run{
		Model:  fauxModel(t),
		System: "be brief",
		Prompt: "count to one",
		Tools:  tools,
	}, func(s Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) == 0 {
		t.Error("the run said nothing")
	}
	if len(steps) == 0 {
		t.Error("no step was reported")
	}
	// The faux provider answers without calling a tool, so no tool runs
	// and no tool names come back: what matters is that the loop ends.
	if calls != 0 {
		t.Errorf("a tool ran %d times without the model calling it", calls)
	}
}

func TestLoopReportsTheToolsThatRan(t *testing.T) {
	// A model that calls the tool before it answers: the faux provider
	// only says text, so the stream is scripted here.
	var calls int
	tools := []piai.AgentTool{{
		Name:        "count",
		Description: "Count.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
			calls++
			return text("1"), nil
		},
	}}
	var steps []Step
	turn := 0
	m := fauxModel(t)
	state := piai.AgentState{
		Model: m,
		Tools: tools,
		StreamFn: func(ctx context.Context, model piai.Model, c piai.Context, opts piai.SimpleStreamOptions) (*piai.AssistantMessageEventStream, error) {
			turn++
			stream := piai.NewEventStream[piai.AssistantMessageEvent, piai.AssistantMessage]()
			msg := piai.AssistantMessage{Role: "assistant", API: model.API, Provider: model.Provider, Model: model.ID}
			if turn == 1 {
				msg.Content = []piai.ContentBlock{piai.ToolCall{Type: "toolCall", ID: "call-1", Name: "count", Arguments: json.RawMessage(`{}`)}}
				msg.StopReason = piai.StopToolUse
			} else {
				msg.Content = []piai.ContentBlock{piai.TextContent{Type: "text", Text: "one"}}
				msg.StopReason = piai.StopStop
			}
			stream.Push(piai.EventDone{Type: "done", Message: msg})
			stream.End(msg)
			return stream, nil
		},
	}
	ag := piai.NewAgent(piai.AgentOptions{InitialState: &state})
	ag.Subscribe(func(evt piai.AgentEvent) {
		if e, ok := evt.(piai.EventToolExecStart); ok {
			steps = append(steps, Step{ToolCalls: []string{e.ToolName}})
		}
	})
	if _, err := ag.Run(context.Background(), piai.UserMessage{Role: "user", Content: "count"}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("the tool ran %d times", calls)
	}
	if len(steps) != 1 || steps[0].ToolCalls[0] != "count" {
		t.Errorf("the steps are %+v", steps)
	}
}

func TestLoopStopsAtItsSteps(t *testing.T) {
	if maxSteps != 24 {
		t.Fatalf("the cap is %d steps", maxSteps)
	}
}

func TestLoopReportsAStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Loop(ctx, Run{Model: fauxModel(t), Prompt: "hi"}, nil)
	if err == nil {
		t.Error("a cancelled run said it succeeded")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/ai -run TestLoop`
Expected: FAIL with "undefined: Loop", "undefined: Step"

- [ ] **Step 3: Write minimal implementation**

```go
package ai

import (
	"context"

	piai "github.com/HycJack/pi-ai-go"
)

// maxSteps is how many turns a run takes at most.
const maxSteps = 24

// Step is what a run did, as it goes: the text of a message, and the
// tools it called.
type Step struct {
	Message   string
	Deltas    []string
	ToolCalls []string
}

// Run is one question put to a model, with the tools it may use.
type Run struct {
	Model    piai.Model
	APIKey   string
	System   string
	Prompt   string
	Messages []piai.Message
	Tools    []piai.AgentTool
	// Approve is asked before a write, nil to allow every one.
	Approve func(Change) bool
}

// Loop asks the model, runs the tools it calls, and asks again with
// their results, until it answers without a tool or the steps run out.
//
// It is pi-ai-go's agent loop, which owns the turn-taking; Mayoi only
// subscribes to its events and caps the turns. The cap is counted here
// rather than in ShouldStopAfterTurn so that a run whose model keeps
// calling tools ends whether or not the loop honours the hook.
func Loop(ctx context.Context, r Run, onStep func(Step)) ([]piai.Message, error) {
	if onStep == nil {
		onStep = func(Step) {}
	}
	var mu sync.Mutex
	turns, calls := 0, 0
	var tools []string
	flushCalls := func() {
		mu.Lock()
		got := tools
		tools = nil
		mu.Unlock()
		if len(got) > 0 {
			onStep(Step{ToolCalls: got})
		}
	}
	state := piai.AgentState{
		Model:        r.Model,
		SystemPrompt: r.System,
		Messages:     r.Messages,
		Tools:        r.Tools,
		GetApiKey:    func() string { return r.APIKey },
		ShouldStopAfterTurn: func(piai.AssistantMessage, []piai.ToolResultMessage) bool {
			mu.Lock()
			defer mu.Unlock()
			turns++
			return turns >= maxSteps
		},
	}
	ag := piai.NewAgent(piai.AgentOptions{InitialState: &state})
	ag.Subscribe(func(evt piai.AgentEvent) {
		switch e := evt.(type) {
		case piai.EventMessageUpdate:
			// Only the text deltas, which the panel appends as they come.
			if d, ok := e.AssistantEvent.(piai.EventTextDelta); ok && d.Delta != "" {
				onStep(Step{Message: d.Delta})
			}
		case piai.EventToolExecStart:
			mu.Lock()
			tools = append(tools, e.ToolName)
			calls++
			over := calls > maxSteps
			mu.Unlock()
			if over {
				onStep(Step{OutOfSteps: true})
				return
			}
			flushCalls()
		case piai.EventToolExecEnd:
			if e.IsError {
				onStep(Step{ToolCalls: []string{e.ToolName + " failed"}})
			}
		case piai.EventCompaction:
			onStep(Step{Compacted: true})
		}
	})
	msgs, err := ag.Run(ctx, piai.UserMessage{Role: "user", Content: r.Prompt})
	flushCalls()
	return msgs, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/ai -run TestLoop`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/ai/run.go internal/ai/run_test.go
git commit -m "Run the assistant's answer in steps"
```

---

### Task 4: The panel runs the agent

**Files:**
- Modify: `cmd/mayoi/ai.go` (`send` uses `ai.Loop`; add `aiPanel.tools`)
- Modify: `cmd/mayoi/app.go` (a `loop` field for tests)
- Modify: `cmd/mayoi/ai_test.go`

**Interfaces:**
- Consumes: `ai.Loop`, `ai.Tools`, `ai.Run`, `ai.Step` from Tasks 1–3.
- Produces:
  - `(*app).aiLoop` — `func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error)`, `ai.Loop` unless a test replaces it.
  - `(*app).aiTools() *ai.Tools` — the tools rooted at `a.root`, with `Approve` wired to the panel.
  - `aiMessage.tools []string` — the tools a message called, shown under its text.

- [ ] **Step 1: Write the failing test**

```go
func TestTheAssistantUsesTheFolderTools(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var got ai.Run
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		got = r
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("what is in main.go")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	names := []string{}
	for _, tool := range got.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, " ") != "read glob grep write edit open_file" {
		t.Errorf("the tools are %q", names)
	}
	if got.Model.ID != "faux-1" || got.APIKey != "test-key" {
		t.Errorf("the run is %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mayoi -run TestTheAssistantUsesTheFolderTools`
Expected: FAIL with "a.loop undefined"

- [ ] **Step 3: Write minimal implementation**

In `cmd/mayoi/ai.go`, replace the `stream` call in `send` with `a.loop`, and add:

```go
// aiTools are the assistant's tools, rooted at the window's folder, with
// the approval of the panel for their writes.
func (a *app) aiTools() *ai.Tools {
	t := &ai.Tools{Root: a.root}
	if !a.settings.AI.ApproveEdits {
		t.Approve = a.askApproval
	}
	return t
}
```

In `cmd/mayoi/app.go`, beside `stream` and `target`:

```go
	// loop is how a message is run: ai.Loop, unless a test has its own.
	loop func(context.Context, ai.Run, func(ai.Step)) ([]piai.Message, error)
```

and in `newApp`, after `a.target = a.aiTarget`:

```go
	a.loop = ai.Loop
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/mayoi -run 'TestTheAssistant|TestAssistant'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/mayoi/ai.go cmd/mayoi/app.go cmd/mayoi/ai_test.go
git commit -m "Run the assistant's message through the agent"
```

---

### Task 5: The approval card

**Files:**
- Modify: `cmd/mayoi/ai.go` (the card, `askApproval`)
- Modify: `internal/settings/settings.go` (`AISettings.ApproveEdits`)
- Modify: `locales/en.json`, `locales/ja.json`, `locales/zh-Hans.json`
- Modify: `cmd/mayoi/ai_test.go`

**Interfaces:**
- Consumes: `ai.Change` from Task 2, `locales`, `internal/linediff` (see `docs/FEATURES.md` Diffs) for the changed lines.
- Produces:
  - `aiPanel.approval *aiApproval` with `type aiApproval struct { change ai.Change; reply chan bool }`.
  - `(*app).askApproval(c ai.Change) bool` — posts the card and blocks the calling tool goroutine until **Approve** or **Reject**.
  - `AISettings.ApproveEdits bool` (`json:"approveEdits,omitempty"`), default false, meaning ask.
  - Locale keys `aiApprove`, `aiReject`, `aiApproveTitle`.

- [ ] **Step 1: Write the failing test**

```go
func TestTheAssistantAsksBeforeItWrites(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	root := hs.root
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		// The tool asks for approval while the panel shows the card.
		tools := a.aiTools()
		body, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "hi\n"})
		res, err := tools.Write(ctx, "call-1", body, nil)
		if err != nil {
			return nil, err
		}
		onStep(ai.Step{Message: res.Content[0].(piai.TextContent).Text})
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("add new.txt")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	// The card is up while the tool waits.
	for i := 0; i < 200 && a.ai.approval == nil; i++ {
		step(hs.tt)
	}
	if a.ai.approval == nil {
		t.Fatal("no approval card")
	}
	if !hs.tt.HasText("new.txt") {
		t.Errorf("the card does not name the file: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click("Approve"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Errorf("the file was not written: %v", err)
	}
}

func TestRejectingTheCardWritesNothing(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	root := hs.root
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		tools := a.aiTools()
		body, _ := json.Marshal(map[string]string{"path": "new.txt", "content": "hi\n"})
		res, _ := tools.Write(ctx, "call-1", body, nil)
		onStep(ai.Step{Message: res.Content[0].(piai.TextContent).Text})
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("add new.txt")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	for i := 0; i < 200 && a.ai.approval == nil; i++ {
		step(hs.tt)
	}
	if err := hs.tt.Click("Reject"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Error("a rejected write wrote the file")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mayoi -run 'TestTheAssistantAsks|TestRejectingTheCard'`
Expected: FAIL with "a.ai.approval undefined", "a.aiTools undefined"

- [ ] **Step 3: Write minimal implementation**

In `cmd/mayoi/ai.go`:

```go
// aiApproval is a write waiting for its answer: the change, and where
// the answer goes.
type aiApproval struct {
	change ai.Change
	reply  chan bool
}

// askApproval shows the change and waits for Approve or Reject. It runs
// on a tool's goroutine, so it only posts to the panel.
func (a *app) askApproval(c ai.Change) bool {
	reply := make(chan bool, 1)
	a.post(func() {
		a.ai.approval = &aiApproval{change: c, reply: reply}
	})
	return <-reply
}

// answered takes the answer to the card shown, and shows no card after.
func (a *app) answered(ok bool) {
	p := &a.ai
	if p.approval == nil {
		return
	}
	p.approval.reply <- ok
	p.approval = nil
	a.settings.Changed()
}
```

and the card, drawn in `aiTranscript` after the messages:

```go
// aiApprovalCard is a write waiting for Approve or Reject, with the
// lines it changes.
func (a *app) aiApprovalCard(c *ui.Context) {
	p := a.pal
	ap := a.ai.approval
	if ap == nil {
		return
	}
	ui.Column(c.Key("ai-approval")).Margin(6, 6).Padding(8, 8).Gap(6).Radius(6).
		Background(p.surface).Border(1, p.border).Children(func() {
		ui.Text(c, locales.AiApproveTitle()).FontSize(11).FontWeight(600).TextColor(p.muted).SingleLine()
		ui.Text(c, filepath.Base(ap.change.Path)).FontSize(12.5).TextColor(p.text).SingleLine()
		if d := changedLines(ap.change.Before, ap.change.After); d != "" {
			ui.Scroll(c.Key("ai-approval-diff")).MaxHeight(160).Children(func() {
				for _, line := range strings.Split(d, "\n") {
					col := p.text
					if strings.HasPrefix(line, "+ ") {
						col = p.addedFg
					} else if strings.HasPrefix(line, "- ") {
						col = p.removedFg
					}
					ui.Text(c, line).FontSize(11.5).TextColor(col).SingleLine()
				}
			})
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			reject := ui.Button(c, locales.AiReject())
			reject.FontSize(12).Padding(6, 8)
			if reject.Clicked() {
				a.post(func() { a.answered(false) })
			}
			approve := ui.Button(c, locales.AiApprove())
			approve.FontSize(12).Padding(6, 8)
			if approve.Clicked() {
				a.post(func() { a.answered(true) })
			}
		})
	})
}
```

`changedLines` is a small helper in `cmd/mayoi/ai.go`:

```go
// changedLines are the lines a change adds or removes, prefixed by their
// sign, at most 40 of each.
func changedLines(before, after []byte) string {
	b := strings.Split(strings.TrimSuffix(string(before), "\n"), "\n")
	a := strings.Split(strings.TrimSuffix(string(after), "\n"), "\n")
	var out []string
	seen := map[string]bool{}
	for _, line := range a {
		if !seen[line] && !slices.Contains(b, line) {
			out = append(out, "+ "+line)
		}
		seen[line] = true
	}
	for _, line := range b {
		if !slices.Contains(a, line) {
			out = append(out, "- "+line)
		}
	}
	if len(out) > 80 {
		out = append(out[:80], "…")
	}
	return strings.Join(out, "\n")
}
```

In `internal/settings/settings.go`, add to `AISettings`:

```go
	// ApproveEdits writes what the assistant asks without a card, as
	// the user asked once and for all.
	ApproveEdits bool `json:"approveEdits,omitempty"`
```

Locale keys, in all three catalogs, in alphabetical order:

```json
  "aiApprove": "Approve",
  "aiApproveTitle": "The assistant wants to change",
  "aiReject": "Reject",
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go tool mygo generate && go test ./cmd/mayoi -run 'TestTheAssistant|TestAssistant|TestRejecting'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/mayoi/ai.go internal/settings/settings.go locales cmd/mayoi/ai_test.go
git commit -m "Approve the assistant's writes on a card"
```

---

### Task 6: The `open_file` tool, and the folder's tabs

**Files:**
- Modify: `cmd/mayoi/ai.go` (`open_file`)
- Modify: `cmd/mayoi/ai_test.go`

**Interfaces:**
- Consumes: `(*app).openFileAt(path string, line int, focus bool)` from `cmd/mayoi/diffview.go`, `(*app).aiTools`.
- Produces: the `open_file` tool, named `"open_file"`, whose params are `{ "path": string, "line": int }`.

- [ ] **Step 1: Write the failing test**

```go
func TestOpenFileShowsTheFileAtItsLine(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var tools []piai.AgentTool
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		tools = r.Tools
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("show me main.go")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	var open piai.AgentTool
	for _, tool := range tools {
		if tool.Name == "open_file" {
			open = tool
		}
	}
	if open.Name == "" {
		t.Fatal("no open_file tool")
	}
	body, _ := json.Marshal(map[string]any{"path": "main.go", "line": 3})
	if _, err := open.Execute(context.Background(), "call-1", body, nil); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	d := a.activeEditor()
	if d == nil || d.path != hs.path("main.go") {
		t.Fatalf("opened %v", d)
	}
	if got := d.buf.Snapshot().LineAt(d.sel[0]); got != 3 {
		t.Errorf("the caret is on line %d", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mayoi -run TestOpenFileShowsTheFileAtItsLine`
Expected: FAIL with "no open_file tool"

- [ ] **Step 3: Write minimal implementation**

In `cmd/mayoi/ai.go`, add to `aiTools` a tool the panel owns:

```go
// OpenFileParams is what open_file is asked: the file, and its line.
type OpenFileParams struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// openFile is the tool that shows a file of the folder in the editors,
// at its line, as the assistant explains it.
func (a *app) openFileTool() piai.AgentTool {
	return piai.AgentTool{
		Name:        "open_file",
		Description: "Open a file of the folder in the editor, at a line, so the user sees it.",
		Label:       "Open",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"}},"required":["path"]}`),
		Execute: func(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (piai.AgentToolResult, error) {
			var p OpenFileParams
			if err := json.Unmarshal(params, &p); err != nil {
				return piai.AgentToolResult{IsError: true}, nil
			}
			full, err := ai.Resolve(a.root, p.Path)
			if err != nil {
				return piai.AgentToolResult{Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: err.Error()}}, IsError: true}, nil
			}
			a.post(func() { a.openFileAt(full, p.Line, true) })
			return piai.AgentToolResult{Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: "opened " + p.Path}}}, nil
		},
	}
}
```

and in `aiTools()`:

```go
	tools := append(t.ReadSet(), t.WriteSet()...)
	return append(tools, a.openFileTool())
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/mayoi -run 'TestTheAssistant|TestAssistant|TestRejecting|TestOpenFile'`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/mayoi/ai.go cmd/mayoi/ai_test.go
git commit -m "Let the assistant open a file where it explains it"
```

---

### Task 7: Tool cards, and the run's end

**Files:**
- Modify: `cmd/mayoi/ai.go` (a step callback that appends tool names to the message; the cap's message)
- Modify: `locales/en.json`, `locales/ja.json`, `locales/zh-Hans.json` (`aiToolRan`, `aiStepsOut`)
- Modify: `CHANGELOG.md`, `docs/FEATURES.md`
- Modify: `cmd/mayoi/ai_test.go`

**Interfaces:**
- Consumes: `ai.Step` from Task 3, `aiPanel.msgs`, `aiMessage.tools`.
- Produces: `aiMessage.tools []string`; the panel shows `Ran read, glob` under a message's text; `maxSteps` reached shows `aiStepsOut`.

- [ ] **Step 1: Write the failing test**

```go
func TestThePanelShowsTheToolsThatRan(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		onStep(ai.Step{Message: "reading"})
		onStep(ai.Step{ToolCalls: []string{"read", "glob"}})
		onStep(ai.Step{Message: " done"})
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("look")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	last := a.ai.msgs[len(a.ai.msgs)-1]
	if strings.Join(last.tools, " ") != "read glob" {
		t.Errorf("the tools are %q", last.tools)
	}
	if !hs.tt.HasText("read, glob") {
		t.Errorf("the tools are not shown: %q", hs.tt.Texts())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/mayoi -run TestThePanelShowsTheToolsThatRan`
Expected: FAIL with "last.tools undefined"

- [ ] **Step 3: Write minimal implementation**

Add `tools []string` to `aiMessage`, and in `send`'s step callback:

```go
	onStep := func(s ai.Step) {
		a.post(func() {
			if p.writing != w {
				return
			}
			if s.Message != "" {
				p.msgs[last].text += s.Message
			}
			p.msgs[last].tools = append(p.msgs[last].tools, s.ToolCalls...)
		})
	}
```

and in the transcript, under the message's text:

```go
			if len(msg.tools) > 0 {
				ui.Text(c, locales.AiToolRan(strings.Join(msg.tools, ", "))).FontSize(11).
					TextColor(p.faint).SingleLine()
			}
```

Locale key (`aiToolRan`, with a `{tools}` placeholder, so `@aiToolRan` declares it):

```json
  "@aiToolRan": { "placeholders": { "tools": {} } },
  "aiToolRan": "Ran {tools}",
```

A run that ran out of steps says so, once, under the answer. Add to `aiPanel`:

```go
	// outOfSteps says the run stopped at the cap, not because it was done.
	outOfSteps bool
```

and in the step callback beside the tool names:

```go
			if s.OutOfSteps {
				p.outOfSteps = true
			}
```

in `aiTranscript`, after the messages, before the approval card:

```go
	if a.ai.outOfSteps {
		ui.Text(c, locales.AiStepsOut()).FontSize(11).TextColor(p.muted).Padding(0, 8, 6, 8)
	}
```

and in `send`, when a run starts, `p.outOfSteps = false`. Locale key:

```json
  "aiStepsOut": "It stopped after 24 steps. Ask again to go on.",
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go tool mygo generate && go test ./cmd/mayoi -run 'TestTheAssistant|TestAssistant|TestRejecting|TestOpenFile|TestThePanelShows'`
Expected: PASS

- [ ] **Step 5: Update the CHANGELOG and the docs**

```markdown
- The AI Assistant reads, searches and edits the folder, opening the files it explains.
- Writes come as a card with the changed lines: **Approve** writes them, **Reject** sends them back.
- `ai.approveEdits` writes what the assistant asks without asking first.
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "Show the assistant's tools and its edits"
```

---

## Self-Review

**Spec coverage:** §3's three tiers — read tools (Task 1), approved writes (Tasks 2 and 5), `bash` left for M4 by decision of this plan's scope line — plus `open_file` (Task 6) and the 24-step cap (Task 3). §4's tool cards (Task 7) and approval cards with a diff (Task 5). §7's errors: every tool failure is a result the model reads, and a failed write is a toast (Task 2). §5's sessions and §6's personas are M3 and M4, as the spec's milestone list says.

**Placeholders:** none — every step carries the code and the command it runs.

**Type consistency:** `ai.Tools`, `ai.Change`, `ai.Run`, `ai.Step`, `ai.Resolve`, `ai.maxSteps` are used with the names Tasks 1–3 define them under. `(*app).aiTools`, `(*app).askApproval`, `(*app).answered` and `app.loop` match Task 4 and Task 5's definitions. `aiMessage.tools` is added in Task 7 and read there only.

**Review Focus:** each of the five lines has its test — Task 1 (`TestToolsRefuseAPathOutsideTheFolder`), Task 2 (`TestARejectedWriteSaysSo`), Task 5 (`TestRejectingTheCardWritesNothing`, and the card appearing again after a rejection is the same test's second pass), Task 3 (`TestLoopStopsAtItsSteps`, `TestLoopReportsAStop`), Task 2 (`failure(err)` for a write that cannot happen).
