package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"mayoi/internal/ai"
	"mayoi/internal/bg"
	"mayoi/locales"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/egoist/mygo/ui"
)

// aiMinW is the least the assistant's panel is wide.
const aiMinW = 240

// aiStream is how the assistant asks a model: ai.Live, or a test's own.
type aiStream = ai.Stream

// aiChatSystem is what the assistant is told of itself, before the folder.
const aiChatSystem = "You are Mayoi's assistant, helping with the code of the folder open in the editor."

// aiPanel is the assistant's panel at the right of the window: the
// conversation so far and what is being said.
type aiPanel struct {
	// msgs are the messages of the conversation, the user's and the
	// assistant's, in the order they were said.
	msgs []aiMessage
	// prompt is what was typed and not sent yet.
	prompt string
	// keyed says what was typed is what this window typed, not what a
	// newer conversation put there.
	keyed ui.Handle
	// writing is the answer under way, nil when the assistant is idle.
	writing *aiReply
	// err is the failure of the last answer.
	err string
}

// aiMessage is a message of the conversation: whose it is, "user" or
// "assistant", and what it says.
type aiMessage struct {
	role string
	text string
}

// aiReply is an answer under way: cancel stops it, and its identity
// tells a newer answer from this one.
type aiReply struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// cancel stops the answer under way.
func (p *aiPanel) cancel() {
	if w := p.writing; w != nil {
		w.cancel()
		p.writing = nil
	}
}

// stop is the command that stops the answer under way.
func (a *app) stopAI() {
	if a.ai.writing == nil {
		return
	}
	a.ai.cancel()
	a.invalidate()
}

// toggleAI shows the assistant's panel, or hides it.
func (a *app) toggleAI() {
	a.window.AIOpen = !a.window.AIOpen
	a.settings.Changed()
	a.invalidate()
	if a.window.AIOpen {
		a.ai.keyed.Focus()
	}
}

// send asks the assistant what was typed, and shows the answer as it
// comes.
func (a *app) send() {
	p := &a.ai
	text := strings.TrimSpace(p.prompt)
	if text == "" || p.writing != nil {
		return
	}
	model, key, err := a.target()
	if err != nil {
		p.err = err.Error()
		a.invalidate()
		return
	}
	p.prompt, p.err = "", ""
	p.msgs = append(p.msgs, aiMessage{role: "user", text: text})
	answer := aiMessage{role: "assistant"}
	p.msgs = append(p.msgs, answer)
	last := len(p.msgs) - 1

	ctx, cancel := context.WithCancel(context.Background())
	w := &aiReply{ctx: ctx, cancel: cancel}
	p.writing = w
	stream := a.stream
	req := ai.Request{
		Model:    model,
		APIKey:   key,
		System:   aiChatSystem,
		Messages: ai.Transcript(spoken(p.msgs[:last])),
	}
	bg.Go(func() {
		defer recoverCrash()
		// A stop is not a failure: the user asked for it.
		_, err := stream(ctx, req, func(s string) {
			a.post(func() {
				if p.writing != w {
					return
				}
				// The answer may have been dropped as empty already.
				if last < len(p.msgs) {
					p.msgs[last].text += s
				}
			})
		})
		stopped := ctx.Err() != nil
		cancel()
		a.post(func() {
			if p.writing != w {
				return
			}
			p.writing = nil
			if err != nil && !stopped {
				p.err = err.Error()
			}
			if last < len(p.msgs) && p.msgs[last].text == "" {
				p.msgs = p.msgs[:last]
			}
		})
	})
	a.invalidate()
}

// spoken is the messages of the conversation as the models take them.
func spoken(msgs []aiMessage) []ai.Message {
	out := make([]ai.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, ai.Message{Role: m.role, Text: m.text})
	}
	return out
}

// aiTarget is the model the assistant asks, and its key: the one the
// settings chose, or the first one keys were given for.
func (a *app) aiTarget() (piai.Model, string, error) {
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		return piai.Model{}, "", err
	}
	want := a.settings.AI
	var providers []string
	if want.Provider != "" {
		providers = []string{want.Provider}
	} else {
		for _, name := range ai.Providers() {
			if keys[name] != "" {
				providers = append(providers, name)
			}
		}
		if len(providers) == 0 {
			providers = ai.Providers()
		}
	}
	for _, name := range providers {
		model := want.Model
		if model == "" {
			if models := ai.Models(name); len(models) > 0 {
				model = models[0].ID
			}
		}
		if model == "" {
			continue
		}
		m, err := ai.Resolve(name, model)
		if err != nil {
			continue
		}
		if key := keys[name]; key != "" {
			return m, key, nil
		}
	}
	return piai.Model{}, "", errAINoModel
}

// errAINoModel is the failure of a send with no model, or no key for one.
var errAINoModel = errors.New("the assistant has no model: add a key for a provider to ai-keys.json")

// aiSidePanel is the assistant's panel, at the right edge of the window:
// what it leaves for the editors and the sidebar is theirs, as the
// terminal's panel at a side leaves it.
func (a *app) aiSidePanel(c *ui.Context) {
	p, w := a.pal, a.window
	width, _ := c.Size()
	room := width - 320
	if !w.SidebarClosed {
		room -= w.SidebarWidth
	}
	if a.rail != nil {
		room -= railW
	}
	room = max(room, aiMinW)
	size := clamp(w.AIWidth, aiMinW, room)
	box := ui.Column(c.Key("ai")).Shrink(0).Width(size).Background(p.panel).
		BorderWidth(0, 0, 0, 1).BorderColor(p.border)
	box.Children(func() {
		a.aiTitleRow(c)
		a.aiTranscript(c)
		if a.ai.err != "" {
			ui.Text(c, locales.AiChatFailed(a.ai.err)).FontSize(12).TextColor(p.dangerText).
				Padding(0, 10, 6, 10)
		}
		a.aiCompose(c)
		a.splitter(c, edgeLeft, func(d float32) {
			w.AIWidth = clamp(size-d, aiMinW, room)
		})
	})
}

// aiTitleRow is the head of the assistant's panel: its name.
func (a *app) aiTitleRow(c *ui.Context) {
	p := a.pal
	ui.Row(c.Key("ai-head")).Height(panelHeadH).Padding(0, 10).Gap(6).AlignItems(ui.Center).
		Background(p.chrome).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Text(c, locales.AiTitle()).FontSize(11).FontWeight(600).
			LetterSpacing(titleSpacing).TextColor(p.text).Grow(1).SingleLine()
		if a.ai.writing != nil && a.iconButton(c, iconStopSquare, locales.AiStop(), false).Clicked() {
			a.stopAI()
		}
	})
}

// aiTranscript is what was said in the conversation so far, or what to
// start with.
func (a *app) aiTranscript(c *ui.Context) {
	p := a.pal
	ai := &a.ai
	if len(ai.msgs) == 0 {
		ui.Text(c, locales.AiEmpty()).FontSize(12.5).TextColor(p.muted).Padding(12, 12).Grow(1)
		return
	}
	ui.Scroll(c.Key("ai-msgs")).Grow(1).MinHeight(0).Padding(6).Children(func() {
		for i := range ai.msgs {
			msg := &ai.msgs[i]
			ui.Column(c.Key("ai-msg-"+strconv.Itoa(i))).Padding(6, 8).Gap(2).Children(func() {
				ui.Text(c, aiRoleName(msg.role)).FontSize(10).FontWeight(600).LetterSpacing(titleSpacing).
					TextColor(p.muted).SingleLine()
				ui.Text(c, msg.text).FontSize(12.5).TextColor(p.text)
			})
		}
	})
}

// aiCompose is the field what is asked is typed in.
func (a *app) aiCompose(c *ui.Context) {
	p := a.pal
	ai := &a.ai
	field := ui.Row(c.Key("ai-compose")).Height(30).Padding(0, 4, 0, 10).Gap(2).AlignItems(ui.Center).
		Background(p.editor).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Transition(fieldFocus)
	field.Children(func() {
		placeholder := locales.AiPlaceholder()
		in := a.textKeys(ui.TextInputBase(c.Key("ai-input"), &ai.prompt)).Grow(1).MinWidth(0).
			FontSize(12.5).Placeholder(placeholder).Label(placeholder)
		in.Bind(&ai.keyed)
		if in.Submitted() {
			a.post(a.send)
		}
		if in.Changed() {
			ai.keyed.Focus()
		}
	})
}

// aiRoleName is whose message a role is, in words.
func aiRoleName(role string) string {
	if role == "user" {
		return locales.AiYou()
	}
	return locales.AiTitle()
}
