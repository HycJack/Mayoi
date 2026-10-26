# Mayoi AI 助手 M1（基础聊天）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Mayoi 右侧出现一个可开合的 AI 面板，能在其中与 pi-ai-go 支持的任一模型流式对话。

**Architecture:** `internal/ai` 是唯一接触 pi-ai-go 的包，负责密钥文件、模型目录、
一次流式请求；`cmd/mayoi/ai.go` 持有面板状态并在 `view.go` 的窗口行末绘制面板，
发送走注入的 `ai.Stream`（生产实现为 `ai.Live`），因此测试不需要网络。

**Tech Stack:** Go 1.27、MyGo 即时模式 UI（`github.com/egoist/mygo/ui`）、
`github.com/HycJack/pi-ai-go`（本地 replace）。

**Spec:** `docs/superpowers/specs/2025-06-27-ai-assistant-design.md`

## Global Constraints

- 只有 `internal/ai` 可以 import `github.com/HycJack/pi-ai-go`；`cmd/mayoi` 只用 `mayoi/internal/ai`。
- go.mod 用相对路径 `replace github.com/HycJack/pi-ai-go => ../../pi-ai-go`，require `v0.0.0`。
- API key 只存 `ai-keys.json`（与 settings.json 同目录，权限 0600），**绝不**进 settings.json。
- 所有新 UI 文案走 `locales`：`en.json`、`ja.json`、`zh-Hans.json` 三份都要加，然后 `go tool mygo generate`。
- 面板状态存 `settings.Window`（`AIOpen`、`AIWidth`），随窗口恢复。
- 提交信息一行祈使句、72 字符内（AGENTS.md）。
- `CHANGELOG.md` 在 `## Unreleased` 下加面向用户的条目，每条约 100 字符。
- 远程（SSH）文件夹 M1 不支持：面板顶部显示不可用提示，不发送请求。

## Review Focus

- **无 key 时发送**：必须给出一条可读的提示并恢复输入框，不能静默失败或永久禁用发送。
- **流式中途取消**（Esc）：必须停止 goroutine 并把已收到的文本留在消息里，之后的增量不能再写进来。
- **面板状态持久化**：关窗重开后 `AIOpen`/`AIWidth` 与关窗前一致，宽度被夹在合理区间。
- **多字节文本**：流式增量按 rune 边界拼接，中文回复不能出现乱码或半个字符。
- **翻译缺失**：`go tool mygo generate` 后三份目录都应有新键，缺一份会在启动时报错。

---

### Task 1: 依赖与密钥文件

**Files:**
- Modify: `go.mod`
- Create: `internal/ai/keys.go`
- Test: `internal/ai/keys_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `ai.Keys`（`map[string]string`）、`ai.LoadKeys(path string) (Keys, error)`、`ai.SaveKeys(path string, k Keys) error`

- [ ] **Step 1: 加依赖**

`go.mod` 的 require 块加一行，文件末尾另起 replace：

```
require github.com/HycJack/pi-ai-go v0.0.0

replace github.com/HycJack/pi-ai-go => ../../pi-ai-go
```

- [ ] **Step 2: 写失败的测试**

`internal/ai/keys_test.go`：

```go
package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeysRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-keys.json")
	if err := SaveKeys(path, Keys{"anthropic": "sk-ant", "openai": "sk-oa"}); err != nil {
		t.Fatal(err)
	}
	k, err := LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if k["anthropic"] != "sk-ant" || k["openai"] != "sk-oa" {
		t.Fatalf("keys are %v", k)
	}
}

func TestKeysOfNoFileAreEmpty(t *testing.T) {
	k, err := LoadKeys(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 0 {
		t.Fatalf("keys are %v", k)
	}
}

func TestSaveKeysIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ai-keys.json")
	if err := SaveKeys(path, Keys{"x": "y"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the file is %v", perm)
	}
}
```

- [ ] **Step 3: 跑测试看它失败**

Run: `go test ./internal/ai -run TestKeys -v`
Expected: FAIL，`undefined: SaveKeys`

- [ ] **Step 4: 实现**

`internal/ai/keys.go`：

```go
// Package ai is Mayoi's AI assistant: the models it talks to, the keys
// it talks to them with, and the chat it keeps with them.
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Keys holds the API key of each provider, by the provider's name.
type Keys map[string]string

// LoadKeys reads the keys at path. A file that is not there has none.
func LoadKeys(path string) (Keys, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Keys{}, nil
	}
	if err != nil {
		return nil, err
	}
	k := Keys{}
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("ai: %s: %w", path, err)
	}
	return k, nil
}

// SaveKeys writes the keys to path, and the folders before it, for the
// user alone to read.
func SaveKeys(path string, k Keys) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
```

- [ ] **Step 5: 跑测试看它通过**

Run: `go test ./internal/ai -run TestKeys -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add go.mod go.sum internal/ai/keys.go internal/ai/keys_test.go
git commit -m "Add pi-ai-go and the assistant's keys file"
```

---

### Task 2: 模型目录与一次流式请求

**Files:**
- Create: `internal/ai/catalog.go`、`internal/ai/chat.go`
- Test: `internal/ai/chat_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `ai.Providers() []string`、`ai.Models(provider string) []piai.Model`、
  `ai.Resolve(provider, model string) (piai.Model, error)`、
  `ai.Message{Role, Text string}`、`ai.Transcript(msgs []ai.Message) []piai.Message`、
  `ai.Request{Model piai.Model; APIKey, System string; Messages []piai.Message}`、
  `type ai.Stream func(ctx context.Context, req Request, onText func(string)) (string, error)`、
  `ai.Live(ctx, req, onText)`（`ai.Stream` 的实现）

- [ ] **Step 1: 写失败的测试**

`internal/ai/chat_test.go`：

```go
package ai

import (
	"context"
	"strings"
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

func TestLiveStreamsTheAnswer(t *testing.T) {
	var got strings.Builder
	text, err := Live(context.Background(), Request{
		Model:    fauxModel(t),
		Messages: Transcript([]Message{{Role: "user", Text: "hi"}}),
	}, func(s string) { got.WriteString(s) })
	if err != nil {
		t.Fatal(err)
	}
	if text == "" || got.String() != text {
		t.Fatalf("text %q, deltas %q", text, got.String())
	}
	if !strings.Contains(text, "hi") {
		t.Errorf("the answer %q does not answer %q", text, "hi")
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
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/ai -run 'TestLive|TestTranscript|TestProviders|TestResolve' -v`
Expected: FAIL，`undefined: Live`

- [ ] **Step 3: 实现目录**

`internal/ai/catalog.go`：

```go
package ai

import (
	"fmt"
	"sort"

	piai "github.com/HycJack/pi-ai-go"
)

// Providers lists, alphabetically, the providers that offer chat models.
func Providers() []string {
	var out []string
	for _, p := range piai.GetProviders() {
		if len(piai.GetModels(p)) > 0 {
			out = append(out, string(p))
		}
	}
	sort.Strings(out)
	return out
}

// Models lists the chat models of a provider.
func Models(provider string) []piai.Model {
	return piai.GetModels(piai.KnownProvider(provider))
}

// Resolve is the model that provider offers as model.
func Resolve(provider, model string) (piai.Model, error) {
	m, err := piai.GetModel(piai.KnownProvider(provider), model)
	if err != nil {
		return piai.Model{}, fmt.Errorf("ai: %w", err)
	}
	return m, nil
}
```

- [ ] **Step 4: 实现请求与流**

`internal/ai/chat.go`：

```go
package ai

import (
	"context"
	"strings"

	piai "github.com/HycJack/pi-ai-go"
)

// Message is a turn of a chat, in words the panel shows.
type Message struct {
	Role string // "user" or "assistant"
	Text string
}

// Transcript is the chat as the models take it.
func Transcript(msgs []Message) []piai.Message {
	out := make([]piai.Message, 0, len(msgs))
	for _, m := range msgs {
		switch m.Role {
		case "user":
			out = append(out, piai.UserMessage{Role: "user", Content: m.Text})
		case "assistant":
			out = append(out, piai.AssistantMessage{
				Role:       "assistant",
				Content:    []piai.ContentBlock{piai.TextContent{Type: "text", Text: m.Text}},
				StopReason: piai.StopStop,
			})
		}
	}
	return out
}

// Request is one chat sent to a model.
type Request struct {
	Model    piai.Model
	APIKey   string
	System   string
	Messages []piai.Message
}

// Stream sends req to its model and calls onText with each piece of the
// answer as it arrives. It returns the whole answer.
type Stream func(ctx context.Context, req Request, onText func(string)) (string, error)

// Live is the Stream of the providers pi-ai-go knows.
func Live(ctx context.Context, req Request, onText func(string)) (string, error) {
	s, err := piai.StreamSimpleWithContext(ctx, req.Model, piai.Context{
		SystemPrompt: req.System,
		Messages:     req.Messages,
	}, piai.SimpleStreamOptions{StreamOptions: piai.StreamOptions{APIKey: req.APIKey}})
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	_, err = s.ForEach(ctx, func(evt piai.AssistantMessageEvent) error {
		switch e := evt.(type) {
		case piai.EventTextDelta:
			sb.WriteString(e.Delta)
			if onText != nil {
				onText(e.Delta)
			}
		case piai.EventError:
			return e.Error
		}
		return nil
	})
	return sb.String(), err
}
```

- [ ] **Step 5: 跑测试**

Run: `go test ./internal/ai -v`
Expected: PASS（faux 提供者离线作答，不需要网络与 key）

- [ ] **Step 6: 提交**

```bash
git add internal/ai/catalog.go internal/ai/chat.go internal/ai/chat_test.go
git commit -m "Add the assistant's model catalog and its stream"
```

---

### Task 3: 设置里的面板状态与模型

**Files:**
- Modify: `internal/settings/settings.go`（`Prefs`、`Window`、`normalize`、`AIPath`）
- Test: `internal/settings/settings_test.go`

**Interfaces:**
- Consumes: 无
- Produces: `settings.AISettings{Provider, Model string}`、`Prefs.AI`、
  `Window.AIOpen bool`、`Window.AIWidth float32`、`(*Settings).AIPath() string`

- [ ] **Step 1: 写失败的测试**

加到 `internal/settings/settings_test.go`：

```go
func TestTheAssistantOpensAtItsWidth(t *testing.T) {
	s := Default()
	w := s.NewWindow()
	if w.AIWidth != 360 {
		t.Errorf("the assistant is %.0f wide", w.AIWidth)
	}
	if w.AIOpen {
		t.Error("the assistant starts open")
	}
}

func TestTheAssistantPathIsBesideTheSettings(t *testing.T) {
	s := Default()
	s.path = filepath.Join("where", "settings.json")
	if got, want := s.AIPath(), filepath.Join("where", "ai-keys.json"); got != want {
		t.Errorf("the keys are at %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/settings -run TestTheAssistant -v`
Expected: FAIL，`s.AIWidth undefined`

- [ ] **Step 3: 实现**

`Prefs` 末尾加（`SyncIgnored` 之后）：

```go
	// AI is the assistant's model. Its keys are secrets, and live in
	// AIPath beside this file rather than here, where the account would
	// sync them.
	AI AISettings `json:"ai,omitempty"`
```

`TabStyle` 之前加：

```go
// AISettings is the AI assistant's settings, which hold no secrets.
type AISettings struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}
```

`Window` 末尾加：

```go
	// AIOpen and AIWidth are the assistant's panel, open or not, and its
	// width.
	AIOpen  bool    `json:"aiOpen,omitempty"`
	AIWidth float32 `json:"aiWidth,omitempty"`
```

`Default()` 里给初值（找到构造 `Window` 或 Prefs 的地方，加一行）：

```go
	AIWidth: 360,
```

`normalize()` 里加：

```go
	if s.AIWidth != 0 {
		s.AIWidth = clamp(s.AIWidth, 240, 1200)
	}
```

`Path()` 之后加：

```go
// AIPath is the file of the assistant's API keys, beside the settings:
// they are secrets, and the account syncs the settings.
func (s *Settings) AIPath() string {
	return filepath.Join(filepath.Dir(s.path), "ai-keys.json")
}
```

落点说明：`AIWidth` 属于 `Window`，其默认值在 `(*Settings).NewWindow()` 里设置
（`s.AIWidth` 不存在）；窗口宽度的夹取在面板绘制时用 `clamp` 完成，`normalize()` 不必改。

- [ ] **Step 4: 跑测试**

Run: `go test ./internal/settings -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/settings
git commit -m "Keep the assistant's panel and model in the settings"
```

---

### Task 4: 面板的文案

**Files:**
- Modify: `locales/en.json`、`locales/ja.json`、`locales/zh-Hans.json`、`locales/catalog_gen.go`（生成）
- Test: 无（`go tool mygo generate` 本身是检查）

**Interfaces:**
- Produces: `locales.CmdViewAI()`、`locales.AITitle()`、`locales.AIEmpty()`、
  `locales.AIPlaceholder()`、`locales.AINoKey()`、`locales.AIFailed()`、`locales.AIStop()`

- [ ] **Step 1: 三份目录各加六个键**

`locales/en.json`（键按字母序插入）：

```json
"aiEmpty": "Ask about the code in this folder.",
"aiFailed": "The assistant failed: {error}",
"aiNoKey": "Add an API key for {provider} to settings.json to chat with it.",
"aiPlaceholder": "Ask the assistant",
"aiStop": "Stop",
"aiTitle": "AI",
"cmdViewAI": "AI Assistant",
```

`locales/zh-Hans.json`：

```json
"aiEmpty": "问问这个文件夹里的代码。",
"aiFailed": "助手出错了：{error}",
"aiNoKey": "在 settings.json 里为 {provider} 添加 API key 才能和它对话。",
"aiPlaceholder": "问助手",
"aiStop": "停止",
"aiTitle": "AI",
"cmdViewAI": "AI 助手",
```

`locales/ja.json`：

```json
"aiEmpty": "このフォルダーのコードについて聞いてください。",
"aiFailed": "アシスタントが失敗しました: {error}",
"aiNoKey": "{provider} の API key を settings.json に追加すると対話できます。",
"aiPlaceholder": "アシスタントに聞く",
"aiStop": "停止",
"aiTitle": "AI",
"cmdViewAI": "AI アシスタント",
```

- [ ] **Step 2: 生成访问器**

Run: `go tool mygo generate`
Expected: 重新生成 `locales/catalog_gen.go`，其中出现 `func AITitle()` 等。

- [ ] **Step 3: 检查目录**

Run: `go tool mygo i18n check`
Expected: 无缺失键的报告。

- [ ] **Step 4: 提交**

```bash
git add locales
git commit -m "Write the assistant's words in every language"
```

---

### Task 5: 面板的骨架与开关

**Files:**
- Create: `cmd/mayoi/ai.go`
- Modify: `cmd/mayoi/app.go`（`ai` 字段）、`cmd/mayoi/view.go`（行末插入面板）、
  `cmd/mayoi/commands.go`（命令）
- Test: `cmd/mayoi/ai_test.go`

**Interfaces:**
- Consumes: Task 3 的 `Window.AIOpen`/`AIWidth`，Task 4 的文案
- Produces: `app.ai *aiPanel`、`(*app).toggleAI()`、`(*app).aiPanelView(c *ui.Context)`

- [ ] **Step 1: 写失败的测试**

`cmd/mayoi/ai_test.go`：

```go
package main

import (
	"testing"
)

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
	if _, ok := hs.tt.Find("AI"); !ok {
		t.Errorf("no title: %q", hs.tt.Texts())
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
	if _, ok := hs.tt.Find("Ask about the code"); !ok {
		t.Errorf("no empty state: %q", hs.tt.Texts())
	}
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./cmd/mayoi -run TestTheAssistant -v`
Expected: FAIL，`a.toggleAI undefined`

- [ ] **Step 3: 实现面板**

`cmd/mayoi/ai.go`：

```go
package main

import (
	"context"

	"github.com/egoist/mygo/ui"

	"mayoi/internal/ai"
	"mayoi/locales"
)

// aiMinW is the least width the assistant's panel takes, and aiDefW the
// width it opens at when the settings know none.
const (
	aiMinW = 240
	aiDefW = 360
)

// aiPanel is the assistant, at the right of the window: the chat it kept
// and the field that asks it.
type aiPanel struct {
	msgs []ai.Message
	in   string
	err  string
	run  bool
	stop context.CancelFunc
	// stream is the model to talk to, nil for the providers pi-ai-go
	// knows: a test sets its own.
	stream ai.Stream
	// last is the request the panel sent last, that Retry sends again.
	last ai.Request
}

// toggleAI opens the assistant's panel, or closes it.
func (a *app) toggleAI() {
	a.window.AIOpen = !a.window.AIOpen
	if a.window.AIOpen && a.window.AIWidth < aiMinW {
		a.window.AIWidth = aiDefW
	}
	a.settings.Changed()
	a.invalidate()
}

// aiPanelView is the assistant at the window's right edge, its splitter
// along the edge facing the editors.
func (a *app) aiPanelView(c *ui.Context) {
	p, w := a.pal, a.window
	width, _ := c.Size()
	room := max(width-320, aiMinW)
	size := clamp(w.AIWidth, aiMinW, room)
	box := ui.Column(c.Key("ai")).Shrink(0).Width(size).
		BorderWidth(0, 1, 0, 0).BorderColor(p.border)
	box.Children(func() {
		a.aiBody(c)
		a.splitter(c, edgeLeft, func(d float32) {
			w.AIWidth = clamp(size-d, aiMinW, room)
		})
	})
}

// aiBody is the assistant's title, its chat and its field.
func (a *app) aiBody(c *ui.Context) {
	p := a.pal
	ui.Column(c).Fill().Background(p.chrome).Children(func() {
		ui.Row(c).Height(34).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.AITitle()).FontSize(11).FontWeight(600).LetterSpacing(titleSpacing).
				TextColor(p.muted).Grow(1).SingleLine()
		})
		body := ui.Column(c).Grow(1).MinHeight(0).Padding(0, 10)
		body.Children(func() {
			if len(a.ai.msgs) == 0 {
				ui.Text(c, locales.AIEmpty()).FontSize(12.5).TextColor(p.faint).
					Padding(8, 4)
				return
			}
			ui.Scroll(c.Key("ai-chat")).Grow(1).Children(func() {
				for i, m := range a.ai.msgs {
					col := p.text
					if m.Role == "user" {
						col = p.accent
					}
					ui.Text(c, m.Text).Key(ui.Key("ai-msg-" + strconv.Itoa(i))).
						FontSize(12.5).TextColor(col).Padding(6, 4)
				}
			})
		})
	})
}
```

（`strconv` 需要加进 import。）

- [ ] **Step 4: 接到窗口上**

`app` 结构体加字段：

```go
	ai *aiPanel
```

`newApp` 里加：

```go
	a.ai = &aiPanel{}
```

`view.go` 的 `ui.Row(c).Grow(1)...Children(func() { ... })` 内，最后一段

```go
			if side == "right" {
				a.sidePanel(c, true)
			}
```

之后加：

```go
			if a.window.AIOpen {
				a.aiPanelView(c)
			}
```

- [ ] **Step 5: 加命令**

`commands.go` 的视图命令区（`view.*` 附近）加：

```go
		{"view.ai", t.CmdViewAI(), "Alt+CmdOrCtrl+A", a.toggleAI},
```

- [ ] **Step 6: 跑测试**

Run: `go test ./cmd/mayoi -run TestTheAssistant -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add cmd/mayoi/ai.go cmd/mayoi/ai_test.go cmd/mayoi/app.go cmd/mayoi/view.go cmd/mayoi/commands.go
git commit -m "Show the assistant's panel at the right of the window"
```

---

### Task 6: 发送、流式回显与取消

**Files:**
- Modify: `cmd/mayoi/ai.go`
- Test: `cmd/mayoi/ai_test.go`

**Interfaces:**
- Consumes: `ai.Stream`、`ai.Live`、`ai.Resolve`、`ai.LoadKeys`、`ai.Transcript`
- Produces: `(*app).aiSend(text string)`、`(*app).aiCancel()`

- [ ] **Step 1: 写失败的测试**

`cmd/mayoi/ai_test.go` 追加：

```go
// fakeStream answers each request with the words it is given.
func fakeStream(words ...string) ai.Stream {
	return func(ctx context.Context, req ai.Request, onText func(string)) (string, error) {
		var sb strings.Builder
		for _, w := range words {
			if ctx.Err() != nil {
				return sb.String(), ctx.Err()
			}
			sb.WriteString(w)
			if onText != nil {
				onText(w)
			}
		}
		return sb.String(), nil
	}
}

func TestSendingAQuestionShowsTheAnswer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.ai.stream = fakeStream("the ", "answer")
	a.toggleAI()
	a.aiSend("question")
	hs.settle()
	if len(a.ai.msgs) != 2 {
		t.Fatalf("the chat is %+v", a.ai.msgs)
	}
	if a.ai.msgs[0].Role != "user" || a.ai.msgs[0].Text != "question" {
		t.Errorf("the question is %+v", a.ai.msgs[0])
	}
	if a.ai.msgs[1].Text != "the answer" {
		t.Errorf("the answer is %q", a.ai.msgs[1].Text)
	}
	if a.ai.run {
		t.Error("the panel still runs")
	}
	if _, ok := hs.tt.Find("the answer"); !ok {
		t.Errorf("no answer on screen: %q", hs.tt.Texts())
	}
}

func TestAFailedRequestSaysWhy(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.ai.stream = func(ctx context.Context, req ai.Request, onText func(string)) (string, error) {
		return "", errors.New("no key")
	}
	a.toggleAI()
	a.aiSend("question")
	hs.settle()
	if !strings.Contains(a.ai.err, "no key") {
		t.Errorf("the panel says %q", a.ai.err)
	}
	if a.ai.run {
		t.Error("the panel still runs")
	}
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./cmd/mayoi -run 'TestSending|TestAFailed' -v`
Expected: FAIL，`a.aiSend undefined`

- [ ] **Step 3: 实现发送**

`cmd/mayoi/ai.go` 追加：

```go
// aiSend asks the assistant text, and shows its answer as it arrives.
func (a *app) aiSend(text string) {
	text = strings.TrimSpace(text)
	if text == "" || a.ai.run {
		return
	}
	a.ai.msgs = append(a.ai.msgs, ai.Message{Role: "user", Text: text})
	a.ai.in = ""
	a.ai.err = ""
	a.ai.msgs = append(a.ai.msgs, ai.Message{Role: "assistant"})
	at := len(a.ai.msgs) - 1
	req, err := a.aiRequest()
	if err != nil {
		a.ai.err = err.Error()
		a.ai.msgs = a.ai.msgs[:at]
		a.invalidate()
		return
	}
	a.ai.last = req
	stream := a.ai.stream
	if stream == nil {
		stream = ai.Live
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.ai.stop, a.ai.run = cancel, true
	a.invalidate()
	go func() {
		answer, err := stream(ctx, req, func(d string) {
			a.post(func() {
				a.ai.msgs[at].Text += d
				a.invalidate()
			})
		})
		a.post(func() {
			cancel()
			a.ai.run, a.ai.stop = false, nil
			if answer != "" {
				a.ai.msgs[at].Text = answer
			}
			if err != nil && ctx.Err() == nil {
				a.ai.err = err.Error()
				if a.ai.msgs[at].Text == "" {
					a.ai.msgs = a.ai.msgs[:at]
				}
			} else if a.ai.msgs[at].Text == "" {
				a.ai.msgs = a.ai.msgs[:at]
			}
			a.invalidate()
		})
	}()
}

// aiRequest is the request the panel's last message asks, with the model
// and the key the settings chose.
func (a *app) aiRequest() (ai.Request, error) {
	p, m := a.settings.AI.Provider, a.settings.AI.Model
	if p == "" || m == "" {
		return ai.Request{}, errors.New("the assistant has no model: set ai.provider and ai.model in settings.json")
	}
	model, err := ai.Resolve(p, m)
	if err != nil {
		return ai.Request{}, err
	}
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		return ai.Request{}, err
	}
	key := keys[p]
	if key == "" {
		return ai.Request{}, fmt.Errorf("no API key for %s: add one to %s", p, a.settings.AIPath())
	}
	return ai.Request{Model: model, APIKey: key, Messages: ai.Transcript(a.ai.msgs)}, nil
}

// aiCancel stops the answer that arrives.
func (a *app) aiCancel() {
	if a.ai.stop != nil {
		a.ai.stop()
	}
}
```

- [ ] **Step 4: 输入框、错误行与远程提示**

`aiBody` 中，`body` 之后、`ui.Column` 收尾之前在底部加：

```go
		ui.Column(c).Shrink(0).Padding(8, 10).Gap(6).Children(func() {
			if a.ai.err != "" {
				ui.Text(c, a.ai.err).FontSize(11.5).TextColor(p.dangerText).Padding(0, 2)
			}
			if vfs.IsRemote(a.root) {
				ui.Text(c, locales.AINoKey()).FontSize(11.5).TextColor(p.muted).Padding(0, 2)
				return
			}
			in := a.textKeys(ui.TextInputBase(c.Key("ai-input"), &a.ai.in)).
				FontSize(12.5).Placeholder(locales.AIPlaceholder()).Label(locales.AIPlaceholder())
			if in.Submitted() {
				a.aiSend(a.ai.in)
			}
			if in.Shortcut(0, ui.KeyEscape) && a.ai.run {
				a.aiCancel()
			}
		})
```

远程文件夹的提示文案先用测试用不到的占位（`AINoKey` 带 `{provider}` 形参，M2 再加专用键）；
`aiSend` 开头也要挡住远程：

```go
	if vfs.IsRemote(a.root) {
		return
	}
```

（`mayoi/internal/vfs` 需要加进 `cmd/mayoi/ai.go` 的 import。）

- [ ] **Step 5: 跑测试**

Run: `go test ./cmd/mayoi -run 'TestTheAssistant|TestSending|TestAFailed' -v`
Expected: PASS

- [ ] **Step 6: 跑全部测试**

Run: `go test ./...`
Expected: PASS（若 `cmd/mayoi` 慢，先跑 `go test ./internal/...`，再 `go test ./cmd/mayoi -short`）

- [ ] **Step 7: 提交**

```bash
git add cmd/mayoi/ai.go cmd/mayoi/ai_test.go
git commit -m "Send a question to the assistant and stream its answer"
```

---

### Task 7: 面向用户的记录

**Files:**
- Modify: `CHANGELOG.md`、`docs/FEATURES.md`

- [ ] **Step 1: CHANGELOG**

`CHANGELOG.md` 顶部（`# Changelog` 说明之后、`## 0.12.0` 之前）加：

```markdown
## Unreleased

- An **AI Assistant** panel at the right of the window chats with the model set in `ai.model`.
- Its API keys live in `ai-keys.json` beside the settings, not in the synced settings.
- The answer streams in as the model writes it; `Esc` stops it.
```

- [ ] **Step 2: FEATURES.md**

在 `docs/FEATURES.md` 里合适的小节后加一段：

```markdown
### AI assistant

The panel at the right of the window (Alt+CmdOrCtrl+A, or the command palette's
**AI Assistant**) chats with a model of any provider pi-ai-go knows, chosen by
`ai.provider` and `ai.model` in settings.json. The keys of the providers are in
`ai-keys.json`, beside the settings, and never in the settings themselves, which
the account syncs. The answer streams in; `Esc` stops it.
```

- [ ] **Step 3: 提交**

```bash
git add CHANGELOG.md docs/FEATURES.md
git commit -m "Write the assistant down for the users"
```

---

## 计划之外（M2–M4，各自另立计划）

- **M2**：agent 工具循环（自研 read/write/edit/grep/glob 走 `vfs`）、写审批卡片、
  `BeforeToolCall` 闸门、24 步上限（`ShouldStopAfterTurn` 计数）、压缩改用 `AgentLoop`。
- **M3**：会话持久化（枚举 `ai-sessions/*.jsonl`，`session.NewJSONLStorage`）、会话列表、自动标题。
- **M4**：五个人格、自定义人格、`MAYOI.md` 与 `<env>` 注入。
