package ai

import (
	piai "github.com/HycJack/pi-ai-go"
)

// Message is a turn of a chat, in the words the panel shows.
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
