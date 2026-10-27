package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mayoi/internal/ai"
	"mayoi/locales"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
	"github.com/egoist/mygo/ui"
)

// ownChats puts this window's chats in a folder of their own, so that a
// test sees no conversation another test wrote.
func (hs *harness) ownChats() {
	if hs.chats != "" {
		return
	}
	hs.chats = filepath.Join(hs.t.TempDir(), "ai-sessions")
	hs.a.settings.SessionsDir = hs.chats
}

// fakeStream replaces the assistant's run for a test: it says reply piece
// by piece, and reports err after them. A nil reply hangs until the
// context is cancelled, as a model that thinks for long.
func (hs *harness) fakeStream(reply []string, err error) *[]ai.Run {
	hs.t.Helper()
	var sent []ai.Run
	asked := &sent
	a := hs.a
	old := a.loop
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		sent = append(sent, r)
		for _, s := range reply {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			onStep(ai.Step{Message: s})
		}
		if reply == nil && err == nil {
			// A model that thinks for long: nothing until it is stopped.
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, err
	}
	hs.t.Cleanup(func() {
		a.loop = old
		// A run left waiting would hold the harness's settle up.
		a.ai.cancel()
	})
	// The assistant asks a model the settings chose, so the keys of this
	// machine play no part in a test.
	oldTarget := a.target
	a.target = func() (piai.Model, string, error) { return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil }
	hs.t.Cleanup(func() { a.target = oldTarget })
	return asked
}

// ask types a question into the assistant and presses Enter, in a folder
// whose chats are new to this test.
func (hs *harness) ask(question string) {
	hs.t.Helper()
	hs.ownChats()
	hs.a.toggleAI()
	hs.settle()
	hs.tt.Type(question)
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
}

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

func TestTheAssistantSaysWhatItNeedsToAnswer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.target = func() (piai.Model, string, error) { return piai.Model{}, "", aiNoKey{} }
	hs.ask("hi")
	if !hs.tt.HasText("Add an API key") {
		t.Errorf("no word about the key: %q", hs.tt.Texts())
	}
	// What was typed stays in the field, to send once the key is there.
	a.target = func() (piai.Model, string, error) {
		return piai.Model{}, "", aiNoKey{wanted: true, provider: "anthropic"}
	}
	a.send()
	hs.settle()
	if !hs.tt.HasText("Add an API key for anthropic") {
		t.Errorf("the provider is not named: %q", hs.tt.Texts())
	}
	a.target = func() (piai.Model, string, error) {
		return piai.Model{}, "", aiUnknownModel{provider: "anthropic", model: "nope"}
	}
	a.send()
	hs.settle()
	if !hs.tt.HasText("anthropic/nope is not a model") {
		t.Errorf("the model is not named: %q", hs.tt.Texts())
	}
}

func TestTheAssistantSendsWhatWasTyped(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	sent := hs.fakeStream([]string{"Hello", " there."}, nil)
	hs.ask("what is this")

	if len(*sent) != 1 {
		t.Fatalf("sent %d requests", len(*sent))
	}
	if got := *sent; !strings.HasPrefix(got[0].Prompt, "what is this") {
		t.Errorf("the prompt is %q", got[0].Prompt)
	}
	if len(a.ai.msgs) != 2 || a.ai.msgs[0].role != "user" || a.ai.msgs[0].text != "what is this" {
		t.Fatalf("the conversation is %+v", a.ai.msgs)
	}
	if m := a.ai.msgs[1]; m.role != "assistant" || m.text != "Hello there." {
		t.Errorf("the answer is %+v", m)
	}
	if !hs.tt.HasText("Hello there.") {
		t.Errorf("the answer is not shown: %q", hs.tt.Texts())
	}
}

func TestAssistantFailuresShowUp(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream(nil, errors.New("no route"))
	a.toggleAI()
	hs.settle()
	hs.tt.Type("hi")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if !hs.tt.HasText("no route") {
		t.Errorf("the failure is not shown: %q", hs.tt.Texts())
	}
}

func TestTheAssistantStopsAnAnswer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream(nil, nil) // hangs until cancelled
	hs.ask("hi")
	if a.ai.writing == nil {
		t.Fatal("the assistant is not answering")
	}
	a.run("view.ai.stop")
	hs.settle()
	if a.ai.writing != nil {
		t.Errorf("still writing: %+v", a.ai)
	}
	// The answer keeps its place, marked, rather than being dropped.
	last := a.ai.msgs[len(a.ai.msgs)-1]
	if last.role != "assistant" || !last.stopped {
		t.Errorf("the stopped answer is %+v", last)
	}
	if !hs.tt.HasText("Stopped.") {
		t.Errorf("the stop is not shown: %q", hs.tt.Texts())
	}
}

func TestStoppingKeepsWhatWasSaidAndTakesNoMore(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream(nil, nil) // stubs the model and its key as well
	// It says a word, then waits: the stop must keep the word, and a
	// delta still on its way after the stop must land nowhere.
	release := make(chan struct{})
	var once sync.Once
	letGo := func() { once.Do(func() { close(release) }) }
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		onStep(ai.Step{Message: "Half an "})
		<-release
		onStep(ai.Step{Message: "answer."})
		<-ctx.Done()
		return nil, ctx.Err()
	}
	t.Cleanup(letGo)
	a.toggleAI()
	hs.settle()
	hs.tt.Type("hi")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	// The stream never returns, so the harness must not settle on it.
	for i := 0; i < 200 && a.ai.writing == nil; i++ {
		step(hs.tt)
	}
	if a.ai.writing == nil {
		t.Fatal("the assistant is not answering")
	}
	a.run("view.ai.stop")
	for i := 0; i < 5; i++ {
		step(hs.tt)
	}
	said := a.ai.msgs[len(a.ai.msgs)-1]
	if said.text != "Half an " || said.stopped {
		t.Errorf("the stopped answer is %+v", said)
	}
	// The dead streamer's last delta changes nothing.
	letGo()
	for i := 0; i < 20; i++ {
		step(hs.tt)
	}
	if got := a.ai.msgs[len(a.ai.msgs)-1].text; got != "Half an " {
		t.Errorf("a delta landed after the stop: %q", got)
	}
	if len(a.ai.msgs) != 2 {
		t.Errorf("the conversation is %+v", a.ai.msgs)
	}
}

func TestTheAssistantUsesTheFolderTools(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var got ai.Run
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		got = r
		return nil, nil
	}
	hs.ask("what is in main.go")
	var names []string
	for _, tool := range got.Tools {
		names = append(names, tool.Name)
	}
	if strings.Join(names, " ") != "read glob grep write edit open_file" {
		t.Errorf("the tools are %q", names)
	}
	if got.Model.ID != "faux-1" || got.APIKey != "test-key" {
		t.Errorf("the run is %+v", got)
	}
	if !strings.HasPrefix(got.Prompt, "what is in main.go") {
		t.Errorf("the prompt is %q", got.Prompt)
	}
}

func TestTheAssistantAsksBeforeItWrites(t *testing.T) {
	hs := newHarness(t, true)
	a, root := hs.a, hs.root
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		// The model calls write, which waits for the card.
		for _, tool := range r.Tools {
			if tool.Name != "write" {
				continue
			}
			res, err := tool.Execute(ctx, "call-1", json.RawMessage(`{"path": "new.txt", "content": "hi\n"}`), nil)
			if err != nil {
				return nil, err
			}
			onStep(ai.Step{Message: saidBy(t, res)})
		}
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("add new.txt")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	// The tool waits for the card, so the harness must not settle on it.
	for i := 0; i < 200 && a.ai.approval == nil; i++ {
		step(hs.tt)
	}
	if a.ai.approval == nil {
		t.Fatal("no approval card")
	}
	if !hs.tt.HasText("new.txt") {
		t.Errorf("the card does not name the file: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click(locales.AiApprove()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		step(hs.tt)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); err != nil {
		t.Errorf("the file was not written: %v", err)
	}
}

func TestRejectingTheCardWritesNothing(t *testing.T) {
	hs := newHarness(t, true)
	a, root := hs.a, hs.root
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		for _, tool := range r.Tools {
			if tool.Name != "write" {
				continue
			}
			res, _ := tool.Execute(ctx, "call-1", json.RawMessage(`{"path": "new.txt", "content": "hi\n"}`), nil)
			onStep(ai.Step{Message: saidBy(t, res)})
		}
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
	if a.ai.approval == nil {
		t.Fatal("no approval card")
	}
	if err := hs.tt.Click(locales.AiReject()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		step(hs.tt)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Error("a rejected write wrote the file")
	}
	if !hs.tt.HasText("rejected") {
		t.Errorf("the answer does not say it was rejected: %q", hs.tt.Texts())
	}
}

// saidBy is what a tool result says.
func saidBy(t *testing.T, r agent.AgentToolResult) string {
	t.Helper()
	var sb strings.Builder
	for _, b := range r.Content {
		if tc, ok := b.(core.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestThePanelShowsTheToolsThatRan(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		onStep(ai.Step{Message: "reading"})
		onStep(ai.Step{ToolCalls: []string{"read"}})
		onStep(ai.Step{ToolCalls: []string{"glob"}})
		onStep(ai.Step{Message: " done"})
		return nil, nil
	}
	hs.ask("look")
	last := a.ai.msgs[len(a.ai.msgs)-1]
	if strings.Join(last.tools, " ") != "read glob" {
		t.Errorf("the tools are %q", last.tools)
	}
	if !hs.tt.HasText("Ran read, glob") {
		t.Errorf("the tools are not shown: %q", hs.tt.Texts())
	}
	if last.text != "reading done" {
		t.Errorf("the answer is %q", last.text)
	}
}

func TestARunThatRunsOutOfStepsSaysSo(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		onStep(ai.Step{OutOfSteps: true})
		return nil, nil
	}
	hs.ask("keep going")
	if !a.ai.outOfSteps {
		t.Error("the panel does not know the run ran out of steps")
	}
	if !hs.tt.HasText("24 steps") {
		t.Errorf("it is not shown: %q", hs.tt.Texts())
	}
}

func TestCommandsComeOnlyWhenTheyAreAllowed(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	var had bool
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		for _, tool := range r.Tools {
			had = had || tool.Name == "bash"
		}
		return nil, nil
	}
	hs.ask("run the tests")
	if had {
		t.Error("bash is offered without ai.allowBash")
	}

	a.settings.AI.AllowBash = true
	had = false
	hs.tt.Type("and now run them")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if !had {
		t.Error("bash is not offered with ai.allowBash")
	}
}

func TestTheCardShowsTheCommandToRun(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.AllowBash = true
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		for _, tool := range r.Tools {
			if tool.Name != "bash" {
				continue
			}
			tool.Execute(ctx, "call-1", json.RawMessage(`{"command": "go test ./..."}`), nil)
		}
		return nil, nil
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("run the tests")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	for i := 0; i < 200 && a.ai.approval == nil; i++ {
		step(hs.tt)
	}
	if a.ai.approval == nil {
		t.Fatal("no approval card")
	}
	if !hs.tt.HasText("go test ./...") {
		t.Errorf("the card does not show the command: %q", hs.tt.Texts())
	}
	if err := hs.tt.Click(locales.AiApprove()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		step(hs.tt)
	}
}

func TestAChatIsKeptAfterItsAnswer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream([]string{"The parser is fine."}, nil)
	hs.ask("is the parser fine")
	chats, err := ai.Sessions{Dir: a.settings.SessionsPath()}.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 {
		t.Fatalf("the folder has %d chats", len(chats))
	}
	if chats[0].Title != "is the parser fine" {
		t.Errorf("the title is %q", chats[0].Title)
	}
	if len(chats[0].Msgs) != 2 || chats[0].Msgs[1].Text != "The parser is fine." {
		t.Errorf("the chat is %+v", chats[0].Msgs)
	}
}

func TestTheTitleIsTheFirstFortyCharacters(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream([]string{"ok"}, nil)
	hs.ask("why does the parser fail on the second line of a file with a very long question")
	if got := a.ai.title; len([]rune(got)) > 41 || got == "" {
		t.Errorf("the title is %q", got)
	}
	if !strings.HasPrefix(a.ai.title, "why does the parser") {
		t.Errorf("the title is %q", a.ai.title)
	}
}

func TestANewChatStartsEmptyAndKeepsTheOldOne(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream([]string{"Sure."}, nil)
	hs.ask("first question")
	hs.settle()
	if err := hs.tt.Click(locales.AiNewChat()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.ai.msgs) != 0 {
		t.Fatalf("the new chat has %+v", a.ai.msgs)
	}
	chats, err := ai.Sessions{Dir: a.settings.SessionsPath()}.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 {
		t.Fatalf("the old chat is gone: %+v", chats)
	}
	// Answering again keeps a second chat.
	hs.tt.Type("second question")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	chats, err = ai.Sessions{Dir: a.settings.SessionsPath()}.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 {
		t.Errorf("the folder has %d chats", len(chats))
	}
}

func TestTheListOpensAChat(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.fakeStream([]string{"Sure."}, nil)
	hs.ask("first question")
	hs.settle()
	if err := hs.tt.Click(locales.AiNewChat()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := hs.tt.Click(locales.AiChats()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := hs.tt.Click("first question"); err != nil {
		t.Fatalf("the chat is not in the list: %v", err)
	}
	hs.settle()
	if len(a.ai.msgs) != 2 || a.ai.msgs[0].text != "first question" {
		t.Errorf("the chat that opened is %+v", a.ai.msgs)
	}
}

func TestTheChosenPersonaIsWhatAnswers(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	sent := hs.fakeStream([]string{"Looks good."}, nil)
	a.window.AIPersona = "reviewer"
	hs.ask("review this")
	if got := *sent; len(got) != 1 {
		t.Fatalf("sent %d runs", len(got))
	}
	if !strings.Contains((*sent)[0].System, "meticulous code reviewer") {
		t.Errorf("the system prompt is %q", (*sent)[0].System)
	}
}

func TestTheQuestionCarriesWhereTheUserIs(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	sent := hs.fakeStream([]string{"ok"}, nil)
	hs.ask("what does this do")
	run := (*sent)[0]
	if !strings.Contains(run.Prompt, "<env>") || !strings.Contains(run.Prompt, a.root) {
		t.Errorf("the prompt is %q", run.Prompt)
	}
	if !strings.Contains(run.Prompt, "what does this do") {
		t.Errorf("the prompt lost the question: %q", run.Prompt)
	}
}

func TestTheProjectsMemoryReachesTheModel(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	if err := os.WriteFile(filepath.Join(a.root, "MAYOI.md"), []byte("Always write tests first.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sent := hs.fakeStream([]string{"ok"}, nil)
	hs.ask("how do we work here")
	if !strings.Contains((*sent)[0].System, "Always write tests first.") {
		t.Errorf("the system prompt is %q", (*sent)[0].System)
	}
}

func TestTheVoiceIsChosenFromTheList(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.toggleAI()
	hs.settle()
	if err := hs.tt.Click(locales.AiPersonaTitle()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.ai.menu {
		t.Fatal("the list of voices did not open")
	}
	if err := hs.tt.Click("Code Reviewer"); err != nil {
		t.Fatalf("the reviewer is not listed: %v", err)
	}
	hs.settle()
	if a.window.AIPersona != "reviewer" || a.ai.menu {
		t.Errorf("the voice is %q, list open %v", a.window.AIPersona, a.ai.menu)
	}
	// And back to the assistant's own.
	if err := hs.tt.Click(locales.AiPersonaTitle()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := hs.tt.Click(locales.AiPersonaOwn()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.window.AIPersona != "" {
		t.Errorf("the voice is %q", a.window.AIPersona)
	}
}
