package ai

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
)

// scriptedStream is a model that answers with the messages given, in
// turn, and with the last one again when they run out.
func scriptedStream(msgs ...core.AssistantMessage) agent.StreamFn {
	var mu sync.Mutex
	turn := 0
	return func(ctx context.Context, model core.Model, llmCtx core.Context, opts core.SimpleStreamOptions) (*core.EventStream[core.AssistantMessageEvent, core.AssistantMessage], error) {
		mu.Lock()
		msg := msgs[min(turn, len(msgs)-1)]
		turn++
		mu.Unlock()
		s := core.NewEventStream[core.AssistantMessageEvent, core.AssistantMessage]()
		go func() {
			// A model says what it says as it says it, then calls what it
			// calls.
			s.Push(core.EventStart{Type: "start"})
			if words := msgText(msg); words != "" {
				s.Push(core.EventTextStart{Type: "text_start"})
				s.Push(core.EventTextDelta{Type: "text_delta", Delta: words})
				s.Push(core.EventTextEnd{Type: "text_end"})
			}
			s.Push(core.EventDone{Type: "done", Message: msg})
			s.End(msg)
		}()
		return s, nil
	}
}

// msgText is what a message says, its text blocks together.
func msgText(msg core.AssistantMessage) string {
	var sb strings.Builder
	for _, b := range msg.Content {
		if tc, ok := b.(core.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// calling is a model's turn that calls a tool.
func calling(name, id string) core.AssistantMessage {
	return core.AssistantMessage{
		Role:       "assistant",
		Content:    []core.ContentBlock{core.ToolCall{Type: "toolCall", ID: id, Name: name, Arguments: json.RawMessage(`{}`)}},
		StopReason: core.StopToolUse,
	}
}

// saying is a model's turn that answers in words.
func saying(s string) core.AssistantMessage {
	return core.AssistantMessage{
		Role:       "assistant",
		Content:    []core.ContentBlock{core.TextContent{Type: "text", Text: s}},
		StopReason: core.StopStop,
	}
}

// aCounter is a tool that counts how often it ran.
func aCounter(ran *int) []agent.AgentTool {
	return []agent.AgentTool{{
		Name:        "count",
		Description: "Count.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
			*ran++
			return text("1"), nil
		},
	}}
}

func TestLoopRunsTheToolTheModelCalls(t *testing.T) {
	ran := 0
	var steps []Step
	tools := aCounter(&ran)
	msgs, err := Loop(context.Background(), Run{
		Model:  fauxModel(t),
		System: "be brief",
		Prompt: "count to one",
		Tools:  tools,
		Stream: scriptedStream(calling("count", "call-1"), saying("one")),
	}, func(s Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 {
		t.Errorf("the tool ran %d times", ran)
	}
	if len(msgs) == 0 {
		t.Error("the run said nothing")
	}
	var names []string
	for _, s := range steps {
		names = append(names, s.ToolCalls...)
	}
	if len(names) != 1 || names[0] != "count" {
		t.Errorf("the steps are %+v", steps)
	}
	if !strings.Contains(joined(steps), "one") {
		t.Errorf("the answer is not in the steps: %+v", steps)
	}
}

// joined is what the steps said, in one line, for an assertion.
func joined(steps []Step) string {
	var sb strings.Builder
	for _, s := range steps {
		sb.WriteString(s.Message)
	}
	return sb.String()
}

func TestLoopStopsAtItsSteps(t *testing.T) {
	ran := 0
	var steps []Step
	tools := aCounter(&ran)
	_, err := Loop(context.Background(), Run{
		Model:  fauxModel(t),
		Prompt: "count forever",
		Tools:  tools,
		Stream: scriptedStream(calling("count", "call-1")),
	}, func(s Step) { steps = append(steps, s) })
	if err != nil {
		t.Fatal(err)
	}
	if ran > maxSteps {
		t.Errorf("the tool ran %d times, past the %d steps", ran, maxSteps)
	}
	var out bool
	for _, s := range steps {
		out = out || s.OutOfSteps
	}
	if !out {
		t.Errorf("the run did not say it ran out of steps: %+v", steps)
	}
}

func TestLoopReportsAStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Loop(ctx, Run{
		Model:  fauxModel(t),
		Prompt: "hi",
		Stream: scriptedStream(saying("hi")),
	}, nil)
	if err == nil {
		t.Error("a cancelled run said it succeeded")
	}
}

func TestLoopStopsWhenTheUserStopsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A model that starts answering and then waits, until it is stopped
	// as the providers report it.
	stream := func(ctx context.Context, model core.Model, llmCtx core.Context, opts core.SimpleStreamOptions) (*core.EventStream[core.AssistantMessageEvent, core.AssistantMessage], error) {
		s := core.NewEventStream[core.AssistantMessageEvent, core.AssistantMessage]()
		go func() {
			s.Push(core.EventTextDelta{Type: "text_delta", Delta: "half"})
			<-ctx.Done()
			s.Error(ctx.Err())
		}()
		return s, nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := Loop(ctx, Run{Model: fauxModel(t), Prompt: "hi", Stream: stream}, func(s Step) {
			if s.Message != "" {
				cancel()
			}
		})
		done <- err
	}()
	select {
	case <-done:
		// It ended, which is all a stop has to do.
	case <-time.After(10 * time.Second):
		t.Fatal("a stopped run never came back")
	}
}
