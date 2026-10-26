package main

import (
	"cmp"
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
	// err is what the panel says of the last failure, ready to show.
	err string
	// shown says the panel has drawn itself once, so a window restored
	// with it open gives it the keyboard.
	shown bool
}

// aiMessage is a message of the conversation: whose it is, "user" or
// "assistant", and what it says.
type aiMessage struct {
	role string
	text string
	// stopped says the user stopped this answer before it said anything.
	stopped bool
}

// aiReply is an answer under way: cancel stops it, its identity tells a
// newer answer from this one, and at is the place of its message in the
// conversation.
type aiReply struct {
	ctx    context.Context
	cancel context.CancelFunc
	at     int
}

// cancel stops the answer under way. The answer keeps its place in the
// conversation, marked when it said nothing: a delta still on its way
// lands there, or nowhere.
func (p *aiPanel) cancel() {
	w := p.writing
	if w == nil {
		return
	}
	w.cancel()
	p.writing = nil
	if w.at < len(p.msgs) && p.msgs[w.at].text == "" {
		p.msgs[w.at].stopped = true
	}
}

// aiSaid is what the panel says of a failure: the one it knows, in the
// words of the locale, or the failure wrapped.
func (a *app) aiSaid(err error) string {
	var noKey aiNoKey
	var unknown aiUnknownModel
	switch {
	case errors.As(err, &noKey) && noKey.wanted:
		return locales.AiNoKeyFor(noKey.provider)
	case errors.As(err, &noKey):
		return locales.AiNoKey()
	case errors.As(err, &unknown):
		return locales.AiUnknownModel(unknown.provider + "/" + unknown.model)
	}
	return locales.AiChatFailed(err.Error())
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
		p.err = a.aiSaid(err)
		a.invalidate()
		return
	}
	p.prompt, p.err = "", ""
	// The keyboard comes back to the field once the answer is under way.
	p.keyed.Focus()
	p.msgs = append(p.msgs, aiMessage{role: "user", text: text})
	answer := aiMessage{role: "assistant"}
	p.msgs = append(p.msgs, answer)
	// last stays the answer's place: the conversation only grows, so a
	// delta of a stopped answer lands where it belongs, or nowhere.
	last := len(p.msgs) - 1

	ctx, cancel := context.WithCancel(context.Background())
	w := &aiReply{ctx: ctx, cancel: cancel, at: last}
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
		// Only post from here: the panel's state belongs to the main
		// thread, and the tools of a later milestone too.
		_, err := stream(ctx, req, func(s string) {
			a.post(func() {
				if p.writing != w {
					return
				}
				p.msgs[last].text += s
			})
		})
		// A stop is not a failure: the user asked for it. It was already
		// marked where it stopped, and this answer's place stays.
		stopped := ctx.Err() != nil
		cancel()
		a.post(func() {
			if p.writing != w {
				return
			}
			p.writing = nil
			if err != nil && !stopped {
				p.err = a.aiSaid(err)
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

// aiTarget is the model the assistant asks, and its key. The settings
// name one; left empty, the first provider a key was given for answers,
// with its first model. Every way of finding none says which it was.
func (a *app) aiTarget() (piai.Model, string, error) {
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		return piai.Model{}, "", err
	}
	want := a.settings.AI
	if want.Provider != "" {
		m, err := ai.Resolve(want.Provider, a.modelOf(want.Provider, want.Model))
		if err != nil {
			return piai.Model{}, "", aiUnknownModel{provider: want.Provider, model: cmp.Or(want.Model, "its first model")}
		}
		key := keys[want.Provider]
		if key == "" {
			return piai.Model{}, "", aiNoKey{wanted: true, provider: want.Provider}
		}
		return m, key, nil
	}
	for _, name := range ai.Providers() {
		if key := keys[name]; key != "" {
			m, err := ai.Resolve(name, a.modelOf(name, want.Model))
			if err != nil {
				return piai.Model{}, "", aiUnknownModel{provider: name, model: want.Model}
			}
			return m, key, nil
		}
	}
	return piai.Model{}, "", aiNoKey{}
}

// modelOf is the model to ask a provider for: the one the settings name,
// or the provider's first.
func (a *app) modelOf(provider, model string) string {
	if model != "" {
		return model
	}
	if models := ai.Models(provider); len(models) > 0 {
		return models[0].ID
	}
	return ""
}

// aiNoKey is the failure of a send with no key for its provider, wanted
// when the settings name that provider.
type aiNoKey struct {
	wanted   bool
	provider string
}

func (e aiNoKey) Error() string { return "no API key for " + e.provider }

// aiUnknownModel is the failure of a send for a model the provider does
// not offer.
type aiUnknownModel struct{ provider, model string }

func (e aiUnknownModel) Error() string { return "unknown model " + e.provider + "/" + e.model }

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
	box := ui.Column(c.Key("ai")).Shrink(0).Background(p.panel).
		BorderWidth(0, 0, 0, 1).BorderColor(p.border)
	// A panel maximized elsewhere in the window leaves the editors none,
	// so the assistant takes what is left rather than a fixed width.
	if a.panelMax {
		box.Grow(1)
	} else {
		box.Width(size)
	}
	box.Children(func() {
		if !a.ai.shown {
			// A window restored with the panel open starts in it.
			a.ai.shown = true
			a.ai.keyed.Focus()
		}
		a.aiTitleRow(c)
		a.aiTranscript(c)
		if a.ai.err != "" {
			ui.Text(c, a.ai.err).FontSize(12).TextColor(p.dangerText).Padding(0, 10, 6, 10)
		}
		a.aiCompose(c)
		if !a.panelMax {
			a.splitter(c, edgeLeft, func(d float32) {
				w.AIWidth = clamp(size-d, aiMinW, room)
			})
		}
	})
}

// aiTitleRow is the head of the assistant's panel: its name.
func (a *app) aiTitleRow(c *ui.Context) {
	p := a.pal
	ui.Row(c.Key("ai-head")).Height(panelHeadH).Padding(0, 10, 0, 12).Gap(6).AlignItems(ui.Center).
		Background(p.chrome).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Text(c, locales.CmdViewAI()).FontSize(11).FontWeight(600).
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
				switch {
				case msg.stopped:
					ui.Text(c, locales.AiStopped()).FontSize(12.5).TextColor(p.muted)
				case msg.text != "":
					ui.Text(c, msg.text).FontSize(12.5).TextColor(p.text)
				}
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
		// Escape stops the answer under way, as Enter starts one.
		if ai.writing != nil && in.Shortcut(0, ui.KeyEscape) {
			a.post(a.stopAI)
		}
		if in.Submitted() {
			a.post(a.send)
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
