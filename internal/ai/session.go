package ai

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/agent/session"
)

// Chat is one conversation kept on disk: its title, when it was last
// written, and what was said.
type Chat struct {
	ID    string
	Title string
	When  time.Time
	Msgs  []Message
}

// Sessions are the conversations of a folder, one JSONL file each, in
// Dir. A chat that cannot be read is left out of the list rather than
// breaking it.
type Sessions struct {
	Dir string
}

// Save writes msgs as the chat id, or as a new one when id is empty, and
// returns the id it used.
func (s Sessions) Save(id, title string, msgs []Message) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	if id == "" {
		id = newChatID()
	}
	path := s.path(id)
	entries := []session.SessionTreeEntry{
		{Type: session.EntrySessionInfo, SessionID: id, Description: title, Timestamp: time.Now().UTC()},
	}
	for _, m := range msgs {
		if m.Text == "" {
			continue
		}
		entries = append(entries, session.SessionTreeEntry{
			Type:      session.EntryMessage,
			Message:   asMessage(m),
			Timestamp: time.Now().UTC(),
		})
	}
	// The chat is written to a file of its own and moved over the old
	// one: pi-ai-go's storage appends to a file it finds, which would keep
	// what the chat no longer says. A crash leaves the old chat, never
	// half of one.
	tmp, err := os.CreateTemp(s.Dir, id+".*.tmp")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Close(); err != nil {
		return "", err
	}
	storage, err := session.NewJSONLStorage(tmp.Name())
	if err != nil {
		return "", err
	}
	err = storage.Append(entries)
	storage.Close()
	if err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return id, nil
}

// Load reads the chat id.
func (s Sessions) Load(id string) (Chat, error) {
	entries, err := s.read(id)
	if err != nil {
		return Chat{}, err
	}
	chat := Chat{ID: id, Msgs: msgsOf(entries)}
	for _, e := range entries {
		if e.Type == session.EntrySessionInfo && e.Description != "" {
			chat.Title = e.Description
		}
	}
	if info, err := os.Stat(s.path(id)); err == nil {
		chat.When = info.ModTime()
	}
	return chat, nil
}

// List is every chat of the folder, the newest first. A file that cannot
// be read is left out; a folder of chats that are not chats has none.
func (s Sessions) List() ([]Chat, error) {
	files, err := filepath.Glob(filepath.Join(s.Dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	chats := make([]Chat, 0, len(files))
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		chat, err := s.Load(id)
		if err != nil || chat.Title == "" {
			continue
		}
		chats = append(chats, chat)
	}
	sort.SliceStable(chats, func(i, j int) bool { return chats[i].When.After(chats[j].When) })
	return chats, nil
}

// path is where the chat id is kept.
func (s Sessions) path(id string) string {
	return filepath.Join(s.Dir, id+".jsonl")
}

// read is the entries of a chat, as pi-ai-go keeps them.
func (s Sessions) read(id string) ([]session.SessionTreeEntry, error) {
	storage, err := session.NewJSONLStorage(s.path(id))
	if err != nil {
		return nil, err
	}
	defer storage.Close()
	return storage.ReadAll()
}

// asMessage is a message of the panel as pi-ai-go takes it.
func asMessage(m Message) piai.Message {
	out := Transcript([]Message{m})
	if len(out) == 0 {
		return piai.UserMessage{Role: "user", Content: m.Text}
	}
	return out[0]
}

// msgsOf is what the entries say, in the words the panel shows.
func msgsOf(entries []session.SessionTreeEntry) []Message {
	out := make([]Message, 0, len(entries))
	for _, e := range entries {
		if e.Type != session.EntryMessage {
			continue
		}
		switch m := e.Message.(type) {
		case piai.UserMessage:
			out = append(out, Message{Role: "user", Text: saidText(m.Content)})
		case piai.AssistantMessage:
			var texts []string
			for _, b := range m.Content {
				if tc, ok := b.(piai.TextContent); ok {
					texts = append(texts, tc.Text)
				}
			}
			out = append(out, Message{Role: "assistant", Text: strings.Join(texts, "")})
		}
	}
	return out
}

// saidText is the text of a user message, which may be a plain string or
// blocks.
func saidText(content any) string {
	switch c := content.(type) {
	case string:
		return c
	case []piai.ContentBlock:
		var texts []string
		for _, b := range c {
			if tc, ok := b.(piai.TextContent); ok {
				texts = append(texts, tc.Text)
			}
		}
		return strings.Join(texts, "")
	}
	return ""
}

// newChatID is a name for a chat that no other chat of the folder has.
func newChatID() string {
	return time.Now().UTC().Format("20060102-150405.000000000")
}
