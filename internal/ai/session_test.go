package ai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestASavedChatComesBack(t *testing.T) {
	s := Sessions{Dir: filepath.Join(t.TempDir(), "ai-sessions")}
	id, err := s.Save("", "Fix the parser", []Message{
		{Role: "user", Text: "the parser fails"},
		{Role: "assistant", Text: "it does"},
	})
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

func TestSavingAChatAgainReplacesIt(t *testing.T) {
	s := Sessions{Dir: t.TempDir()}
	id, err := s.Save("", "One", []Message{{Role: "user", Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(id, "Two", []Message{
		{Role: "user", Text: "hi"},
		{Role: "assistant", Text: "hello"},
	}); err != nil {
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
	if _, err := s.Save("", "One", []Message{{Role: "user", Text: "hi"}}); err != nil {
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
	first, err := s.Save("", "Older", []Message{{Role: "user", Text: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Save("", "Newer", []Message{{Role: "user", Text: "two"}})
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
