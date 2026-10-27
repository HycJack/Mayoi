package ai

import (
	"strings"

	piai "github.com/HycJack/pi-ai-go"
)

// Message is a turn of a chat, in the words the panel shows.
type Message struct {
	Role string // "user" or "assistant"
	Text string
	// Tools are the tools the assistant called while saying it.
	Tools []string
}

// Displays is a conversation as the panel shows it: what was said, and the
// tools that ran on the way. A tool's result is not a turn of the chat: it
// is what the model reads, and the screen says the tool ran instead.
func Displays(history []piai.Message) []Message {
	out := make([]Message, 0, len(history))
	for _, m := range history {
		switch msg := m.(type) {
		case piai.UserMessage:
			if text := saidText(msg.Content); text != "" {
				out = append(out, Message{Role: "user", Text: text})
			}
		case piai.AssistantMessage:
			var text strings.Builder
			var tools []string
			for _, b := range msg.Content {
				switch block := b.(type) {
				case piai.TextContent:
					text.WriteString(block.Text)
				case piai.ToolCall:
					tools = append(tools, block.Name)
				}
			}
			// An answer that said nothing and called nothing said
			// nothing: nothing shows for it.
			if text.Len() == 0 && len(tools) == 0 {
				continue
			}
			out = append(out, Message{Role: "assistant", Text: text.String(), Tools: tools})
		}
	}
	return out
}

// Said is a conversation as the model takes it, built from what was said
// on the screen: the way back when a run returned no messages of its own.
// An answer that said nothing is left out.
func Said(msgs []Message) []piai.Message {
	out := make([]piai.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Text == "" {
			continue
		}
		if m.Role == "user" {
			out = append(out, piai.UserMessage{Role: "user", Content: m.Text})
			continue
		}
		out = append(out, piai.AssistantMessage{
			Role:       "assistant",
			Content:    []piai.ContentBlock{piai.TextContent{Type: "text", Text: m.Text}},
			StopReason: piai.StopStop,
		})
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
