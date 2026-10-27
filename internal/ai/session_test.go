package ai

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	piai "github.com/HycJack/pi-ai-go"
)

// words is a conversation of words as history: a chat that called no tool,
// which is most of them.
func words(msgs ...Message) []piai.Message {
	out := make([]piai.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Role == "user" {
			out = append(out, piai.UserMessage{Role: "user", Content: m.Text})
			continue
		}
		out = append(out, piai.AssistantMessage{
			Role:    "assistant",
			Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: m.Text}},
		})
	}
	return out
}

func TestASavedChatComesBack(t *testing.T) {
	s := Sessions{Dir: filepath.Join(t.TempDir(), "ai-sessions")}
	id, err := s.Save("", "Fix the parser", words(
		Message{Role: "user", Text: "the parser fails"},
		Message{Role: "assistant", Text: "it does"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("no id")
	}
	chats, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].ID != id || chats[0].Title != "Fix the parser" {
		t.Fatalf("the list is %+v", chats)
	}
	back, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != "Fix the parser" {
		t.Errorf("the title is %q", back.Title)
	}
	if len(back.Msgs) != 2 || back.Msgs[1].Role != "assistant" || back.Msgs[1].Text != "it does" {
		t.Errorf("the messages are %+v", back.Msgs)
	}
}

func TestAChatKeepsWhatTheToolsDid(t *testing.T) {
	s := Sessions{Dir: t.TempDir()}
	history := []piai.Message{
		piai.UserMessage{Role: "user", Content: "what is in main.go"},
		piai.AssistantMessage{Role: "assistant", Content: []piai.ContentBlock{
			piai.TextContent{Type: "text", Text: "Let me look."},
			piai.ToolCall{Type: "tool_use", ID: "c1", Name: "read", Arguments: json.RawMessage(`{"path":"main.go"}`)},
		}},
		piai.ToolResultMessage{Role: "toolResult", ToolCallID: "c1", ToolName: "read",
			Content: []piai.ContentBlock{piai.TextContent{Type: "text", Text: "package main"}}},
		piai.AssistantMessage{Role: "assistant", Content: []piai.ContentBlock{
			piai.TextContent{Type: "text", Text: "It is a main."},
		}},
	}
	id, err := s.Save("", "what is in main.go", history)
	if err != nil {
		t.Fatal(err)
	}
	back, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	// The model's conversation comes back whole: the same assistant can
	// go on reading without reading main.go again.
	if len(back.History) != len(history) {
		t.Fatalf("the history has %d messages: %+v", len(back.History), back.History)
	}
	result, ok := back.History[2].(piai.ToolResultMessage)
	if !ok || result.ToolName != "read" || saidText(result.Content) != "package main" {
		t.Errorf("the tool's result is %+v", back.History[2])
	}
	// The panel shows the tool that ran, not its result.
	if len(back.Msgs) != 3 || len(back.Msgs[1].Tools) != 1 || back.Msgs[1].Tools[0] != "read" {
		t.Errorf("the chat shows %+v", back.Msgs)
	}
}

func TestSavingAChatAgainReplacesIt(t *testing.T) {
	s := Sessions{Dir: t.TempDir()}
	id, err := s.Save("", "One", words(Message{Role: "user", Text: "hi"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(id, "Two", words(
		Message{Role: "user", Text: "hi"},
		Message{Role: "assistant", Text: "hello"},
	)); err != nil {
		t.Fatal(err)
	}
	chats, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 {
		t.Fatalf("the list is %+v", chats)
	}
	back, err := s.Load(id)
	if err != nil {
		t.Fatal(err)
	}
	if back.Title != "Two" || len(back.Msgs) != 2 {
		t.Errorf("the chat is %+v", back)
	}
}

func TestAFolderWithNoChatsHasNone(t *testing.T) {
	chats, err := Sessions{Dir: filepath.Join(t.TempDir(), "missing")}.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 0 {
		t.Errorf("the list is %+v", chats)
	}
}

func TestAJunkFileIsNotAChat(t *testing.T) {
	dir := t.TempDir()
	s := Sessions{Dir: dir}
	if _, err := s.Save("", "One", words(Message{Role: "user", Text: "hi"})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.jsonl"), []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chats, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 1 || chats[0].Title != "One" {
		t.Errorf("the list is %+v", chats)
	}
}

func TestTheNewestChatIsFirst(t *testing.T) {
	s := Sessions{Dir: t.TempDir()}
	first, err := s.Save("", "Older", words(Message{Role: "user", Text: "one"}))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Save("", "Newer", words(Message{Role: "user", Text: "two"}))
	if err != nil {
		t.Fatal(err)
	}
	// The files of a fast save share a second: the order still holds.
	chats, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) != 2 {
		t.Fatalf("the list is %+v", chats)
	}
	if chats[0].ID == first && chats[1].ID == second {
		t.Errorf("the older chat is first: %+v", chats)
	}
}
