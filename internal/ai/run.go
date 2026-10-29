package ai

import (
	"context"
	"sync"

	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
)

// maxSteps is how many turns a run takes at most.
const maxSteps = 24

// Step is what a run did, as it goes: the text of a message, the tools
// it called, and that it ran out of steps.
type Step struct {
	Message    string
	ToolCalls  []string
	OutOfSteps bool
}

// Run is one question put to a model, with the tools it may use. Stream
// is the model itself, nil to ask the one pi-ai-go knows.
type Run struct {
	Model    core.Model
	APIKey   string
	System   string
	Prompt   string
	Messages []core.Message
	Tools    []agent.AgentTool
	Stream   agent.StreamFn
	// Pictures are the pictures the question comes with, which the model
	// is shown after its text.
	Pictures []Picture
}

// Loop asks the model, runs the tools it calls, and asks again with
// their results, until it answers without a tool or the steps run out.
//
// The turn-taking is pi-ai-go's agent loop; this only watches it. A run
// is capped so that a model that keeps calling tools ends.
func Loop(ctx context.Context, r Run, onStep func(Step)) ([]core.Message, error) {
	if onStep == nil {
		onStep = func(Step) {}
	}
	// A run that is already stopped does nothing: the loop below cannot
	// be started and stopped in the same breath.
	if err := ctx.Err(); err != nil {
		return r.Messages, err
	}
	var mu sync.Mutex
	turns := 0
	state := agent.AgentState{
		Model:        r.Model,
		SystemPrompt: r.System,
		Messages:     r.Messages,
		Tools:        r.Tools,
		StreamFn:     r.Stream,
		// One tool at a time: a card is answered one at a time, and two
		// writes at once would ask for two answers to one card.
		ToolExecution: core.ToolExecSequential,
		GetApiKey:     func() string { return r.APIKey },
		ShouldStopAfterTurn: func(core.AssistantMessage, []core.ToolResultMessage) bool {
			mu.Lock()
			defer mu.Unlock()
			turns++
			return turns >= maxSteps
		},
	}
	a := agent.New(agent.AgentOptions{InitialState: &state})
	a.Subscribe(func(evt agent.AgentEvent) {
		switch e := evt.(type) {
		case agent.EventMessageUpdate:
			// Only the text, which the panel appends as it comes.
			if d, ok := e.AssistantEvent.(core.EventTextDelta); ok {
				onStep(Step{Message: d.Delta})
			}
		case agent.EventToolExecStart:
			onStep(Step{ToolCalls: []string{e.ToolName}})
		}
	})
	msgs, err := a.Run(ctx, userMessage(r))
	if err != nil {
		return msgs, err
	}
	mu.Lock()
	out := turns >= maxSteps
	mu.Unlock()
	if out {
		onStep(Step{OutOfSteps: true})
	}
	return msgs, nil
}
