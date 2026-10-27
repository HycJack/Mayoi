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
	// Msgs is what the panel shows: the turns of the conversation, with
	// the tools each answer called.
	Msgs []Message
	// History is the conversation as the model took it, tool calls and
	// their results included: asking again after opening a chat is
	// asking the same assistant that read those files.
	History []piai.Message
}

// Sessions are the conversations of a folder, one JSONL file each, in
// Dir. A chat that cannot be read is left out of the list rather than
// breaking it.
type Sessions struct {
	Dir string
}

// Save writes the conversation as the chat id, or as a new one when id is
// empty, and returns the id it used.
func (s Sessions) Save(id, title string, history []piai.Message) (string, error) {
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
	for _, m := range history {
		if m == nil {
			continue
		}
		entries = append(entries, session.SessionTreeEntry{
			Type:      session.EntryMessage,
			Message:   m,
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
	chat := Chat{ID: id, History: historyOf(entries)}
	chat.Msgs = Displays(chat.History)
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

// historyOf is the conversation the entries keep, as the model takes it.
func historyOf(entries []session.SessionTreeEntry) []piai.Message {
	out := make([]piai.Message, 0, len(entries))
	for _, e := range entries {
		if e.Type != session.EntryMessage {
			continue
		}
		switch m := e.Message.(type) {
		case piai.UserMessage:
			out = append(out, m)
		case piai.AssistantMessage:
			out = append(out, m)
		case piai.ToolResultMessage:
			out = append(out, m)
		}
	}
	return out
}

// newChatID is a name for a chat that no other chat of the folder has.
func newChatID() string {
	return time.Now().UTC().Format("20060102-150405.000000000")
}
