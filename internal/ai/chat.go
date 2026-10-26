package ai

import (
	"context"
	"strings"

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
