package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mayoi/internal/ai"
	"mayoi/internal/settings"
	"mayoi/locales"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
	"github.com/egoist/mygo/ui"
)

// waitFor draws frames until cond holds, or says it never did: frames are
// what runs the work a background job posts, and the word the endpoint
// gave lands there.
func waitFor(t *testing.T, hs *harness, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(10 * time.Second); !cond(); {
		if time.Now().After(deadline) {
			t.Fatal("it never happened")
		}
		time.Sleep(5 * time.Millisecond)
		hs.tt.Frame()
	}
}

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
		return piai.Model{}, "", aiNoKey{wanted: true, protocol: ai.Anthropic}
	}
	a.send()
	hs.settle()
	if !hs.tt.HasText("Add an API key for Anthropic") {
		t.Errorf("the provider is not named: %q", hs.tt.Texts())
	}
	a.target = func() (piai.Model, string, error) { return piai.Model{}, "", aiNoModel{} }
	a.send()
	hs.settle()
	if !hs.tt.HasText("Choose a model in AI Settings") {
		t.Errorf("no word about the model: %q", hs.tt.Texts())
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
	if err := hs.tt.Click(locales.AiPersonaButton()); err != nil {
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
	if err := hs.tt.Click(locales.AiPersonaButton()); err != nil {
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

func TestTheCardShowsTheLinesThatChanged(t *testing.T) {
	got := changedLines([]byte("one\ntwo\nthree\n"), []byte("one\nTWO\nthree\n"))
	if got != "- two\n+ TWO" {
		t.Errorf("the change is %q", got)
	}
	// A new file has no old lines, and an empty one has no line at all.
	if got := changedLines(nil, []byte("hi\n")); got != "+ hi" {
		t.Errorf("a new file is %q", got)
	}
	if got := changedLines(nil, nil); got != "" {
		t.Errorf("an empty change is %q", got)
	}
}

func TestOneListShowsAtATime(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.toggleAI()
	hs.settle()
	if err := hs.tt.Click(locales.AiChats()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if !a.ai.chats {
		t.Fatal("the chats did not open")
	}
	// A list over the panel covers its buttons: the other one is opened
	// once this one is closed, or by its command.
	a.post(func() { a.toggleChats() })
	hs.settle()
	if err := hs.tt.Click(locales.AiPersonaButton()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.ai.chats || !a.ai.menu {
		t.Errorf("chats open %v, voices open %v", a.ai.chats, a.ai.menu)
	}
	// The other list closes the first when it opens, from its command.
	a.post(func() { a.togglePersonas() })
	a.post(func() { a.toggleChats() })
	hs.settle()
	if !a.ai.chats || a.ai.menu {
		t.Errorf("chats open %v, voices open %v", a.ai.chats, a.ai.menu)
	}
}

func TestAnOpenedChatRemembersWhatTheToolsDid(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var sent []ai.Run
	old, oldTarget := a.loop, a.target
	a.target = func() (piai.Model, string, error) {
		return piai.Model{ID: "faux-1", API: "faux"}, "test-key", nil
	}
	// The first run reads a file, and says so with a tool result in the
	// conversation it returns.
	a.loop = func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error) {
		sent = append(sent, r)
		if len(sent) > 1 {
			return nil, nil
		}
		onStep(ai.Step{ToolCalls: []string{"read"}})
		onStep(ai.Step{Message: "It is a main."})
		return []piai.Message{
			piai.UserMessage{Role: "user", Content: "what is in main.go"},
			piai.AssistantMessage{Role: "assistant", Content: []piai.ContentBlock{
				piai.ToolCall{Type: "tool_use", ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
			}},
			piai.ToolResultMessage{Role: "toolResult", ToolCallID: "c1", ToolName: "read",
				Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: "package main"}}},
			piai.AssistantMessage{Role: "assistant", Content: []piai.ContentBlock{
				piai.TextContent{Type: "text", Text: "It is a main."},
			}},
		}, nil
	}
	t.Cleanup(func() { a.loop, a.target = old, oldTarget })

	toggle := func() {
		a.toggleAI()
		hs.settle()
	}
	toggle()
	hs.tt.Type("what is in main.go")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()

	chats, err := ai.Sessions{Dir: hs.chats}.List()
	if err != nil || len(chats) != 1 {
		t.Fatalf("the chats are %+v, %v", chats, err)
	}
	// Another conversation, then this one opened again.
	a.newChat()
	hs.settle()
	a.openChat(chats[0].ID)
	hs.settle()
	// The question, the answer that read, and what it said after.
	if len(a.ai.msgs) != 3 || len(a.ai.msgs[1].tools) != 1 || a.ai.msgs[1].tools[0] != "read" {
		t.Fatalf("the chat shows %+v", a.ai.msgs)
	}
	hs.tt.Type("and what does it do")
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()

	if len(sent) != 2 {
		t.Fatalf("the assistant was asked %d times", len(sent))
	}
	// The assistant that reads again is the one that already read it.
	carried := false
	for _, m := range sent[1].Messages {
		if r, ok := m.(piai.ToolResultMessage); ok && r.ToolName == "read" {
			carried = true
		}
	}
	if !carried {
		t.Errorf("the question carries %+v", sent[1].Messages)
	}
}

// openAIConf opens the assistant's settings as the command does, and
// settles.
func (hs *harness) openAIConf() {
	hs.t.Helper()
	hs.ownChats()
	hs.a.openAISettings()
	hs.settle()
}

func TestTheSettingsChooseTheModel(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	if !a.aiConf.open {
		t.Fatal("the settings did not open")
	}
	if err := hs.tt.Click(locales.AiConfProtocolAnthropic()); err != nil {
		t.Fatalf("the protocol is not listed: %v", err)
	}
	hs.settle()
	if a.aiConf.protocol != ai.Anthropic {
		t.Fatalf("the protocol is %q", a.aiConf.protocol)
	}
	// The models listed are the ones kept, and a row clicked is the model
	// that answers, which keeps it.
	a.aiConf.listed = []string{"claude-a"}
	hs.settle()
	if err := hs.tt.Click("claude-a"); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.aiConf.model != "claude-a" || len(a.aiConf.models) != 1 {
		t.Fatalf("the models are %q with %v answering", a.aiConf.models, a.aiConf.model)
	}
	if err := hs.tt.Click(locales.ButtonSave()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.AI.Protocol != ai.Anthropic || a.settings.AI.Model != "claude-a" {
		t.Errorf("the settings say %q/%q", a.settings.AI.Protocol, a.settings.AI.Model)
	}
	if a.aiConf.open {
		t.Error("the settings stayed open")
	}
}

func TestAModelRowStaysPutUnderThePointer(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	hs.a.aiConf.protocol = ai.OpenAI
	a.aiConf.listed = []string{"a-1", "a-2"}
	hs.settle()
	// Where a row is, and where it is with the pointer over it: a row that
	// moves is a list that cannot be clicked.
	before, ok := hs.tt.Find("a-1")
	if !ok || before.W == 0 {
		t.Fatalf("no row shows a-1: %v", before)
	}
	hs.tt.Move(before.X+before.W/2, before.Y+before.H/2)
	hs.settle()
	after, ok := hs.tt.Find("a-1")
	if !ok || after.W == 0 {
		t.Fatalf("the row went: %v", after)
	}
	if after != before {
		t.Errorf("the row moved from %v to %v under the pointer", before, after)
	}
	// And the rows stay put as the list is scrolled.
	hs.tt.Move(0, 0)
	hs.settle()
}

func TestAModelIsTypedIn(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	hs.a.aiConf.protocol = ai.OpenAI
	hs.settle()
	// An endpoint may offer a model it does not list: the button takes the
	// keyboard to the field while there is nothing to keep, and what is
	// typed there is the one that answers.
	if err := hs.tt.Click(locales.AiConfModelType()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	hs.tt.Type("local-qwen-32b")
	hs.settle()
	if a.aiConf.typed != "local-qwen-32b" {
		t.Fatalf("the field says %q", a.aiConf.typed)
	}
	// Enter keeps what is typed, as the button does.
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if a.aiConf.model != "local-qwen-32b" || len(a.aiConf.models) != 1 {
		t.Fatalf("the models are %q with %q answering", a.aiConf.models, a.aiConf.model)
	}
	if a.aiConf.typed != "" {
		t.Errorf("the field still says %q", a.aiConf.typed)
	}
	if err := hs.tt.Click(locales.ButtonSave()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.settings.AI.Model != "local-qwen-32b" {
		t.Errorf("the settings say %q", a.settings.AI.Model)
	}
}

// The dialog is as tall as the window allows. In a window of the usual size
// its fields fit, and none of them is past the bottom; in one too short for
// them, they scroll, and the bar beside them says so.
func TestTheSettingsFitTheWindow(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	a.aiConf.protocol = ai.OpenAI
	a.aiConf.listed = []string{"a-1", "a-2"}
	a.aiConf.voices = []settings.Persona{{Name: "Plain Talk"}}
	hs.settle()
	// The whole of it is in the window, and nothing needs scrolling.
	box, ok := hs.tt.Find(locales.AiConfTitle())
	if !ok || box.H == 0 {
		t.Fatalf("no dialog: %v", box)
	}
	if box.Y < 0 || box.Y+box.H > 760 {
		t.Errorf("the dialog is at %v, outside a window of 760", box)
	}
	for _, label := range []string{locales.ButtonSave(), locales.AiConfVoices(), locales.AiConfExtra()} {
		r, ok := hs.tt.Find(label)
		if !ok || r.H == 0 {
			t.Errorf("%q is not in the window: %v", label, r)
			continue
		}
		if r.Y < box.Y || r.Y+r.H > box.Y+box.H {
			t.Errorf("%q is at %v, outside the dialog %v", label, r, box)
		}
	}
	if a.aiConf.scroll.MaxY > 0 {
		t.Errorf("the fields scroll in a window of 760: %v", a.aiConf.scroll.MaxY)
	}
}

// A window too short for the fields scrolls them, and keeps the buttons in
// sight under them.
func TestTheSettingsScrollInAShortWindow(t *testing.T) {
	hs := newHarness(t, true)
	hs.tt.SetSize(1200, 320)
	a := hs.a
	hs.openAIConf()
	a.aiConf.protocol = ai.OpenAI
	a.aiConf.listed = []string{"a-1", "a-2"}
	a.aiConf.voices = []settings.Persona{{Name: "Plain Talk"}}
	hs.settle()
	if a.aiConf.scroll.MaxY <= 0 {
		t.Fatalf("the fields do not scroll in a window of 320: %v", a.aiConf.scroll.MaxY)
	}
	save, ok := hs.tt.Find(locales.ButtonSave())
	if !ok || save.H == 0 {
		t.Fatalf("no Save button: %v", save)
	}
	if save.Y < 0 || save.Y+save.H > 320 {
		t.Errorf("the Save button is at %v, outside the window", save)
	}
	// The fields below the window are reached by the scroll the bar beside
	// them reports: at the end of it, the last of them is in sight, above
	// the buttons.
	last := locales.AiConfExtraWhy()
	below, ok := hs.tt.Find(last)
	if ok && below.Y+below.H < save.Y {
		t.Fatalf("%q is in sight without scrolling: %v", last, below)
	}
	a.aiConf.scroll.Y = a.aiConf.scroll.MaxY
	hs.settle()
	if a.aiConf.scroll.Y < a.aiConf.scroll.MaxY/2 {
		t.Errorf("the fields are not at the end they were set to: %v of %v", a.aiConf.scroll.Y, a.aiConf.scroll.MaxY)
	}
	end, ok := hs.tt.Find(last)
	if !ok || end.H == 0 {
		t.Fatalf("%q is not reached: %v", last, end)
	}
	if end.Y < 46 || end.Y+end.H > save.Y {
		t.Errorf("%q is at %v, not between the top of the dialog and the buttons at %v", last, end, save)
	}
}

func TestTheSaveButtonIsInTheWindowWithALongList(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	hs.a.aiConf.protocol = ai.OpenAI
	// An endpoint that names hundreds: the rows scroll, and the buttons
	// stay where they are.
	const height = 760
	listed := make([]string, 0, 120)
	for i := range 120 {
		listed = append(listed, "model-"+strconv.Itoa(i)+"-with-a-name-an-endpoint-gave-it")
	}
	a.aiConf.listed = listed
	a.aiConf.filter = "model-1"
	hs.settle()
	save, ok := hs.tt.Find(locales.ButtonSave())
	if !ok || save.W == 0 {
		t.Fatalf("no Save button: %v", save)
	}
	if save.Y < 0 || save.Y+save.H > height {
		t.Errorf("the Save button is at %v, outside the window", save)
	}
	if err := hs.tt.Click(locales.ButtonSave()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
}

func TestAModelIsKeptAndDropped(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	a.aiConf.listed = []string{"a-1", "a-2"}
	hs.settle()
	if err := hs.tt.Click(locales.AiConfKeep()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.aiConf.models) != 1 || a.aiConf.models[0] != "a-1" {
		t.Fatalf("the kept models are %v", a.aiConf.models)
	}
	// The first one kept answers while nothing else was chosen.
	if a.aiConf.model != "a-1" {
		t.Errorf("%q answers", a.aiConf.model)
	}
	if err := hs.tt.Click(locales.AiConfDrop()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.aiConf.models) != 0 || a.aiConf.model != "" {
		t.Errorf("dropping left %v with %q answering", a.aiConf.models, a.aiConf.model)
	}
}

func TestTheModelsAreFetchedAndNarrowed(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	var mu sync.Mutex
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/models" {
			t.Errorf("asked for %s", got)
		}
		// The endpoint says little of itself: what a body claims of the
		// test that reads it is not its to send.
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		var out struct {
			Data []struct{ ID string } `json:"data"`
		}
		for i := 0; i < aiConfRows+2; i++ {
			out.Data = append(out.Data, struct{ ID string }{ID: "m-" + strconv.Itoa(i)})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	a.aiConf.base = srv.URL
	a.aiConf.key = "sk-typed"
	hs.settle()
	if err := hs.tt.Click(locales.AiConfFetch()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, hs, func() bool { return len(a.aiConf.listed) == aiConfRows+2 })
	// The key typed is the one looked with, before anything is saved.
	mu.Lock()
	got := auth
	mu.Unlock()
	if got != "Bearer sk-typed" {
		t.Errorf("the key sent is %q", got)
	}
	if !hs.tt.HasText("m-0") {
		t.Errorf("the models are not listed: %q", hs.tt.Texts())
	}
	// The list is cut to what a dialog shows, and says how many follow.
	shown, more := aiConfShown(&a.aiConf)
	if len(shown) != aiConfRows || more != 2 {
		t.Errorf("%d shown and %d more", len(shown), more)
	}
	narrowed := a.aiConf.filteredBy("m-1")
	shown, more = aiConfShown(&narrowed)
	if len(shown) != 1 || shown[0] != "m-1" || more != 0 {
		t.Errorf("filtered to %v with %d more", shown, more)
	}
}

// filteredBy is a copy of the dialog narrowed to what was typed.
func (c aiSettings) filteredBy(f string) aiSettings {
	c.filter = f
	return c
}

func TestAFetchThatFailsSaysWhy(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad key"}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	a.aiConf.base, a.aiConf.key = srv.URL, "sk-wrong"
	hs.settle()
	if err := hs.tt.Click(locales.AiConfFetch()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	waitFor(t, hs, func() bool { return a.aiConf.fetch != "" && !a.aiConf.fetching })
	if !a.aiConf.failed || !strings.Contains(a.aiConf.fetch, "401") {
		t.Fatalf("the failure says %q", a.aiConf.fetch)
	}
}

func TestTheKeyGoesInItsOwnFile(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	// A model to answer with, or saving has nothing to save.
	a.aiConf.model = "gpt-x"
	a.aiConf.key = "sk-test-123"
	a.saveAISettings()
	hs.settle()
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		t.Fatal(err)
	}
	if keys[a.aiConf.protocol] != "sk-test-123" {
		t.Errorf("the keys are %v", keys)
	}
	// The settings the account syncs hold no key.
	if data, err := os.ReadFile(a.settings.Path()); err == nil && strings.Contains(string(data), "sk-test-123") {
		t.Error("the key is in the settings, which the account syncs")
	}
}

func TestAVoiceIsWrittenInTheSettings(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	if err := hs.tt.Click(locales.AiConfAddVoice()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if len(a.aiConf.voices) != 1 {
		t.Fatalf("the voices are %+v", a.aiConf.voices)
	}
	a.aiConf.voices[0].Name = "Plain Talk"
	a.aiConf.voices[0].Instructions = "Say it plainly."
	a.aiConf.model = "gpt-x"
	a.saveAISettings()
	hs.settle()
	if a.aiConf.err != "" {
		t.Fatalf("the dialog says %q", a.aiConf.err)
	}
	if len(a.settings.AI.Personas) != 1 {
		t.Fatalf("the voices saved are %+v", a.settings.AI.Personas)
	}
	got := a.settings.AI.Personas[0]
	if got.ID != "plain-talk" || got.Name != "Plain Talk" || got.Instructions != "Say it plainly." {
		t.Errorf("the voice is %+v", got)
	}
	// And the panel answers in it.
	a.toggleAI()
	hs.settle()
	if err := hs.tt.Click(locales.AiPersonaButton()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := hs.tt.Click("Plain Talk"); err != nil {
		t.Fatalf("the voice is not in the list: %v", err)
	}
	hs.settle()
	if a.persona().Instructions != "Say it plainly." {
		t.Errorf("the voice answering is %+v", a.persona())
	}
}

func TestAVoiceMayNotTakeABuiltInName(t *testing.T) {
	got := personaID("Coder")
	if got == "coder" {
		t.Errorf("the id is %q", got)
	}
}

// A key that cannot be written is a dialog that says so, and which file it
// could not write: the settings are not saved without it, and the dialog
// does not close as if all had gone well.
func TestAKeyThatCannotBeSavedSaysWhy(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	a.aiConf.protocol = ai.OpenAI
	a.aiConf.model = "gpt-x"
	a.aiConf.key = "sk-test-123"
	// A folder in the place of the file: no key can be written into a name
	// a folder has taken.
	if err := os.Mkdir(a.settings.AIPath(), 0o755); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if err := hs.tt.Click(locales.ButtonSave()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if a.aiConf.err == "" {
		t.Fatal("the dialog closed as if it had saved")
	}
	if !strings.Contains(a.aiConf.err, "ai-keys.json") {
		t.Errorf("the dialog says %q", a.aiConf.err)
	}
	if !a.aiConf.open {
		t.Error("the dialog closed without saving")
	}
	if a.settings.AI.Model == "gpt-x" {
		t.Error("the settings were saved without the key")
	}
}

func TestTheExtraInstructionsReachTheModel(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.openAIConf()
	// The endpoint speaks OpenAI's protocol like the service itself.
	hs.a.aiConf.protocol = ai.OpenAI
	a.aiConf.extra = "Answer in French."
	a.aiConf.model = "gpt-x"
	a.saveAISettings()
	hs.settle()
	sent := hs.fakeStream([]string{"ok"}, nil)
	hs.ask("hello")
	got := *sent
	if len(got) != 1 || !strings.Contains(got[0].System, "Answer in French.") {
		t.Errorf("the prompt is %q", got[0].System)
	}
}

func TestAFailureOffersItsSettings(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	hs.ownChats()
	// No key at all: the panel says so and offers the way to give one.
	a.target = func() (piai.Model, string, error) {
		return piai.Model{}, "", errors.New("no key")
	}
	a.toggleAI()
	hs.settle()
	hs.tt.Type("hello")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if a.ai.err == "" {
		t.Fatal("the failure said nothing")
	}
	if err := hs.tt.Click(locales.AiConfTitle() + "…"); err != nil {
		t.Fatalf("the settings are not offered: %v", err)
	}
	hs.settle()
	if !a.aiConf.open {
		t.Error("the settings did not open")
	}
}
