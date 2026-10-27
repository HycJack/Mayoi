package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"mayoi/internal/ai"
	"mayoi/internal/bg"
	"mayoi/locales"

	piai "github.com/HycJack/pi-ai-go"
	"github.com/HycJack/pi-ai-go/agent"
	"github.com/HycJack/pi-ai-go/core"
	"github.com/egoist/mygo/ui"
)

// aiMinW is the least the assistant's panel is wide.
const aiMinW = 240

// aiLoop runs a message through a model and its tools: ai.Loop, or a
// test's own.
type aiLoop func(ctx context.Context, r ai.Run, onStep func(ai.Step)) ([]piai.Message, error)

// aiChatSystem is what the assistant is told of itself, before the folder.
const aiChatSystem = "You are Mayoi's assistant, helping with the code of the folder open in the editor."

// aiPanel is the assistant's panel at the right of the window: the
// conversation so far and what is being said.
type aiPanel struct {
	// msgs are the messages of the conversation, the user's and the
	// assistant's, in the order they were said.
	msgs []aiMessage
	// history is the same conversation as the model takes it: the tools
	// that ran and what they said are in it, so a chat opened again is
	// the assistant that read those files.
	history []piai.Message
	// prompt is what was typed and not sent yet.
	prompt string
	// keyed says what was typed is what this window typed, not what a
	// newer conversation put there.
	keyed ui.Handle
	// writing is the answer under way, nil when the assistant is idle.
	writing *aiReply
	// runCancel stops the run, writing or unwinding, and runDone says the
	// goroutine of the run has returned: both nil when nothing runs.
	runCancel context.CancelFunc
	runDone   chan struct{}
	// approval is a write waiting for Approve or Reject, nil when there
	// is none.
	approval *aiApproval
	// outOfSteps says the last run stopped at its cap, not because it
	// was done.
	outOfSteps bool
	// err is what the panel says of the last failure, ready to show.
	err string
	// shown says the panel has drawn itself once, so a window restored
	// with it open gives it the keyboard.
	shown bool
	// chat is the conversation on disk this panel writes to, "" until
	// the first answer of a new chat.
	chat string
	// title is what the chat is called, on disk and in the list.
	title string
	// chats says the list of conversations is open.
	chats bool
	// listed is what the folder's conversations were when the list
	// opened, nil once they are read.
	listed []ai.Chat
	// menu says the list of voices is open.
	menu bool
}

// aiMessage is a message of the conversation: whose it is, "user" or
// "assistant", and what it says.
type aiMessage struct {
	role string
	text string
	// stopped says the user stopped this answer before it said anything.
	stopped bool
	// tools are the tools the assistant called while saying it.
	tools []string
}

// aiApproval is a write waiting for its answer: the change, and where the
// answer goes.
type aiApproval struct {
	change ai.Change
	reply  chan bool
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
// lands there, or nowhere. A run that a stop already killed still has its
// context, which this cancels as well, so that nothing of it stays alive.
func (p *aiPanel) cancel() {
	if p.runCancel != nil {
		p.runCancel()
		p.runCancel = nil
	}
	w := p.writing
	p.writing = nil
	if w == nil {
		return
	}
	if w.at < len(p.msgs) && p.msgs[w.at].text == "" {
		p.msgs[w.at].stopped = true
	}
}

// running says a message is being answered, so Stop has something to stop.
func (p *aiPanel) running() bool {
	return p.runCancel != nil && p.runDone == nil
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
	// The run outlives the answer's text: a stop has to reach it while it
	// unwinds too, and New Chat must not leave it running.
	done := make(chan struct{})
	p.runCancel, p.runDone = cancel, done
	tools := a.aiTools()
	// The question is not in the messages: the run adds it, and the run
	// that follows this one takes it from the history.
	before := len(p.history)
	run := ai.Run{
		Model:    model,
		APIKey:   key,
		System:   ai.SystemFor(a.persona(), aiChatSystem, a.settings.AI.SystemExtra, ai.Memory(a.root)),
		Prompt:   text + "\n\n" + ai.Env(a.root, a.aiOpenPath()),
		Messages: p.history,
		Tools:    append(tools.Sets(), a.aiOpenTool()),
	}
	loop := a.loop
	bg.Go(func() {
		defer recoverCrash()
		defer close(done)
		// Only post from here: the panel's state belongs to the main
		// thread, and the tools read and write the folder from it.
		msgs, err := loop(ctx, run, func(s ai.Step) {
			a.post(func() {
				if p.writing != w {
					return
				}
				p.msgs[last].text += s.Message
				p.msgs[last].tools = append(p.msgs[last].tools, s.ToolCalls...)
				if s.OutOfSteps {
					p.outOfSteps = true
				}
			})
		})
		// A stop is not a failure: the user asked for it. It was already
		// marked where it stopped, and this answer's place stays.
		stopped := ctx.Err() != nil
		cancel()
		a.post(func() {
			// A stop or a New Chat took the run away from this answer: it
			// says nothing more, and keeps no chat.
			if p.runDone != done {
				return
			}
			p.runCancel, p.runDone = nil, nil
			// The question is part of the conversation whether or not
			// its answer came: what the model read on the way is what
			// makes the chat the same one when it is opened again.
			if len(msgs) > before {
				p.history = msgs
			} else {
				// A run that returned nothing of its own said what the
				// screen says it said.
				p.history = ai.Said(spoken(p.msgs))
			}
			if p.writing != w {
				return
			}
			p.writing = nil
			if err != nil && !stopped {
				p.err = a.aiSaid(err)
			}
			// A model that answers in one piece sends no deltas: the
			// answer is what it said last.
			if p.msgs[last].text == "" && !stopped {
				p.msgs[last].text = lastSaid(msgs)
			}
			// What the assistant wrote shows in the tabs that have it.
			if wrote := tools.Wrote(); len(wrote) > 0 {
				a.diskChanged(wrote, nil)
			}
			// A stopped answer keeps nothing: the chat it belongs to may
			// already be another one.
			if !stopped {
				a.keepChat(model, key)
			}
		})
	})
	a.invalidate()
}

// keepChat writes the conversation to the folder's chats. A chat is named
// after its first question, once, when it is new: a later answer does not
// rename it.
func (a *app) keepChat(model piai.Model, key string) {
	p := &a.ai
	if len(p.msgs) == 0 {
		return
	}
	if p.chat == "" {
		p.title = aiTitleOf(p.msgs)
	}
	sessions := ai.Sessions{Dir: a.settings.SessionsPath()}
	id, err := sessions.Save(p.chat, p.title, p.history)
	if err != nil {
		// The conversation stays in the panel: a chat that cannot be
		// written is still a chat.
		p.err = a.aiSaid(err)
		a.invalidate()
		return
	}
	p.chat = id
	a.invalidate()
}

// aiTitleOf names a conversation: its first question, cut short.
func aiTitleOf(msgs []aiMessage) string {
	for _, m := range msgs {
		if m.role == "user" && m.text != "" {
			return cutTitle(m.text)
		}
	}
	return cutTitle("")
}

// cutTitle is the first 40 characters of s, on one line.
func cutTitle(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > 40 {
		return strings.TrimSpace(string(rs[:40])) + "…"
	}
	return s
}

// openChat shows the conversation id in the panel.
func (a *app) openChat(id string) {
	p := &a.ai
	chat, err := ai.Sessions{Dir: a.settings.SessionsPath()}.Load(id)
	if err != nil {
		p.err = a.aiSaid(err)
		a.invalidate()
		return
	}
	p.cancel()
	a.refuseApproval()
	p.msgs = nil
	for _, m := range chat.Msgs {
		p.msgs = append(p.msgs, aiMessage{role: m.Role, text: m.Text, tools: m.Tools})
	}
	// The tools the chat called are part of it: asking again here is
	// asking the assistant that already read those files.
	p.history = chat.History
	p.chat, p.title, p.chats, p.outOfSteps, p.err = id, chat.Title, false, false, ""
	a.invalidate()
}

// newChat starts a conversation that is kept once it has an answer.
func (a *app) newChat() {
	p := &a.ai
	p.cancel()
	a.refuseApproval()
	p.msgs, p.chat, p.title, p.chats, p.outOfSteps, p.err = nil, "", "", false, false, ""
	p.history = nil
	p.keyed.Focus()
	a.invalidate()
}

// readChats fills the list of conversations from the folder.
func (a *app) readChats() {
	p := &a.ai
	chats, err := ai.Sessions{Dir: a.settings.SessionsPath()}.List()
	a.post(func() {
		if p.listed != nil || !p.chats {
			return
		}
		if err != nil {
			p.err = a.aiSaid(err)
		}
		p.listed = chats
		a.invalidate()
	})
}

// lastSaid is what the last answer of a run said.
func lastSaid(msgs []piai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		m, ok := msgs[i].(piai.AssistantMessage)
		if !ok {
			continue
		}
		var sb strings.Builder
		for _, b := range m.Content {
			if tc, ok := b.(piai.TextContent); ok {
				sb.WriteString(tc.Text)
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	return ""
}

// persona is whose voice the assistant answers in: the one this window
// chose, or none, the assistant's own.
func (a *app) persona() ai.Persona {
	id := a.window.AIPersona
	if id == "" {
		return ai.Persona{}
	}
	if p := ai.PersonaByID(a.personas(), id); p != nil {
		return *p
	}
	return ai.Persona{}
}

// personas is the voices the panel offers: the five that come with Mayoi,
// then the ones settings keep.
func (a *app) personas() []ai.Persona {
	custom := make([]ai.Persona, 0, len(a.settings.AI.Personas))
	for _, p := range a.settings.AI.Personas {
		custom = append(custom, ai.Persona{
			ID: p.ID, Name: p.Name, Description: p.Description, Instructions: p.Instructions,
		})
	}
	return ai.Personas(custom)
}

// aiOpenPath is the file on screen, "" when no file has it.
func (a *app) aiOpenPath() string {
	if e := a.activeEditor(); e != nil {
		return e.path
	}
	return ""
}

// togglePersonas opens or closes the list of voices. One list shows at a
// time: the other closes, and the voice chosen does not decide this one.
func (a *app) togglePersonas() {
	a.ai.menu = !a.ai.menu
	a.ai.chats = false
	a.invalidate()
}

// choosePersona answers in the voice id, "" for the assistant's own.
func (a *app) choosePersona(id string) {
	a.window.AIPersona = id
	a.ai.menu = false
	a.settings.Changed()
	a.invalidate()
}

// personaMenu is the list of voices, over the panel.
func (a *app) personaMenu(c *ui.Context) {
	p := &a.ai
	if !p.menu {
		return
	}
	pal := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "ai-personas")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(420).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.AiPersonaTitle())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
					ui.Icon(c, iconSparkles).FontSize(15).TextColor(pal.muted)
					ui.Text(c, locales.AiPersonaTitle()).FontSize(14).FontWeight(600).TextColor(pal.text).Grow(1).SingleLine()
				})
				ui.Scroll(c.Key("ai-persona-list")).MaxHeight(380).Padding(6).Children(func() {
					a.personaRow(c, ai.Persona{Name: locales.AiPersonaOwn(), Description: locales.AiPersonaOwnWhy()})
					for _, p := range a.personas() {
						a.personaRow(c, p)
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.post(func() {
					p.menu = false
					a.invalidate()
				})
			}
		})
	})
}

// personaRow is one voice of the list, the one in use marked.
func (a *app) personaRow(c *ui.Context, p ai.Persona) {
	pal := a.pal
	chosen := a.window.AIPersona == p.ID
	row := ui.ButtonBase(c.Key("ai-persona:"+p.ID)).Padding(5, 10).Gap(10).Radius(6).
		Justify(ui.Start).AlignItems(ui.Center).Label(p.Name)
	if row.Hovered() {
		row.Background(pal.hover)
	}
	if row.Clicked() {
		a.post(func() { a.choosePersona(p.ID) })
	}
	row.Children(func() {
		a.pickMark(c, chosen, "ai-personas")
		ui.Column(c).Gap(1).Grow(1).Children(func() {
			ui.Text(c, p.Name).FontSize(13).TextColor(pal.text).SingleLine()
			if p.Description != "" {
				ui.Text(c, p.Description).FontSize(11.5).TextColor(pal.muted).SingleLine()
			}
		})
	})
}

// aiTools are the assistant's tools, rooted at the window's folder, with
// the panel asked before each write and, when the user allowed commands,
// each one of those too.
func (a *app) aiTools() *ai.Tools {
	t := &ai.Tools{Root: a.root}
	if !a.settings.AI.ApproveEdits {
		t.Approve = a.askApproval
	}
	if a.settings.AI.AllowBash {
		t.Exec = ai.Sh
	}
	return t
}

// aiOpenParams is what open_file is asked: the file, and its line.
type aiOpenParams struct {
	Path string `json:"path"`
	Line int    `json:"line,omitempty"`
}

// aiOpenTool is the tool that shows a file of the folder in the editors,
// at its line, so that the user sees what the assistant explains.
func (a *app) aiOpenTool() agent.AgentTool {
	return agent.AgentTool{
		Name:        "open_file",
		Description: "Open a file of the folder in the editor, at a line, so the user sees it.",
		Label:       "Open",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"line":{"type":"integer"}},"required":["path"]}`),
		Execute: func(ctx context.Context, id string, params json.RawMessage, onUpdate func(json.RawMessage)) (agent.AgentToolResult, error) {
			var p aiOpenParams
			if err := json.Unmarshal(params, &p); err != nil {
				return told(err), nil
			}
			full, err := ai.Under(a.root, p.Path)
			if err != nil {
				return told(err), nil
			}
			a.post(func() { a.openFileAt(full, p.Line, true) })
			return agent.AgentToolResult{
				Content: []core.ContentBlock{core.TextContent{Type: "text", Text: "opened " + p.Path}},
			}, nil
		},
	}
}

// told is a failure the model reads.
func told(err error) agent.AgentToolResult {
	return agent.AgentToolResult{
		Content: []core.ContentBlock{core.TextContent{Type: "text", Text: err.Error()}},
		IsError: true,
	}
}

// askApproval shows the change and waits for Approve or Reject. It runs on
// a tool's goroutine, so it only posts to the panel. The card keeps the
// channel of this call, so a second card replacing it on screen can never
// answer this one, nor be answered by its rejection.
func (a *app) askApproval(c ai.Change) bool {
	card := &aiApproval{change: c, reply: make(chan bool, 1)}
	a.post(func() {
		a.ai.approval = card
	})
	return <-card.reply
}

// answered takes the answer to the card shown, and shows no card after.
func (a *app) answered(ok bool) {
	p := &a.ai
	if p.approval == nil {
		return
	}
	// Buffered, and exactly once per card: a card answered twice would
	// block the frame it is closed in.
	p.approval.reply <- ok
	p.approval = nil
	a.invalidate()
}

// refuseApproval says no to a card waiting, so that a turn whose window is
// closing does not wait for an answer nobody will give.
func (a *app) refuseApproval() {
	p := &a.ai
	if p.approval == nil {
		return
	}
	p.approval.reply <- false
	p.approval = nil
}

// spoken is what the panel shows, as the assistant's own type.
func spoken(msgs []aiMessage) []ai.Message {
	out := make([]ai.Message, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, ai.Message{Role: m.role, Text: m.text, Tools: m.tools})
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
		a.aiApprovalCard(c)
		if a.ai.err != "" {
			// A failure is often the settings: the way to them is here,
			// rather than in a file the user has to find.
			ui.Row(c).Gap(8).AlignItems(ui.Center).Padding(0, 10, 6, 10).Children(func() {
				ui.Text(c, a.ai.err).FontSize(12).TextColor(p.dangerText).Grow(1).Shrink(1)
				if ui.Button(c, locales.AiConfTitle()+"…").FontSize(11).Padding(3, 6).Clicked() {
					a.post(a.openAISettings)
				}
			})
		}
		a.aiCompose(c)
		if !a.panelMax {
			a.splitter(c, edgeLeft, func(d float32) {
				w.AIWidth = clamp(size-d, aiMinW, room)
			})
		}
	})
	a.aiChatListView(c)
	a.personaMenu(c)
}

// aiTitleRow is the head of the assistant's panel: its name, the new
// chat and the list of chats, and Stop while an answer is under way.
func (a *app) aiTitleRow(c *ui.Context) {
	p := a.pal
	ui.Row(c.Key("ai-head")).Height(panelHeadH).Padding(0, 10, 0, 12).Gap(6).AlignItems(ui.Center).
		Background(p.chrome).BorderWidth(0, 0, 1, 0).BorderColor(p.border).Children(func() {
		ui.Text(c, locales.CmdViewAI()).FontSize(11).FontWeight(600).
			LetterSpacing(titleSpacing).TextColor(p.text).Grow(1).SingleLine()
		if a.ai.running() && a.iconButton(c, iconStopSquare, locales.AiStop(), false).Clicked() {
			a.stopAI()
		}
		if a.iconButton(c, iconPlus, locales.AiNewChat(), false).Clicked() {
			a.post(a.newChat)
		}
		if a.iconButton(c, iconListTree, locales.AiChats(), false).Clicked() {
			a.post(a.toggleChats)
		}
		if a.iconButton(c, iconSparkles, locales.AiPersonaButton(), false).Clicked() {
			a.post(a.togglePersonas)
		}
		if a.iconButton(c, iconSettings, locales.AiConfTitle(), false).Clicked() {
			a.post(a.openAISettings)
		}
	})
}

// toggleChats opens or closes the list of conversations, reading the
// folder's own when it opens. One list shows at a time: the other closes.
func (a *app) toggleChats() {
	p := &a.ai
	p.chats = !p.chats
	p.menu = false
	p.listed = nil
	a.invalidate()
	if !p.chats {
		return
	}
	bg.Go(func() {
		defer recoverCrash()
		a.readChats()
	})
}

// aiChatListView is the list of conversations, over the panel.
func (a *app) aiChatListView(c *ui.Context) {
	p := &a.ai
	if !p.chats {
		return
	}
	pal := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "ai-chats")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(420).MaxWidthPercent(92).Margin(titleBarH+8, 0, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.AiChats())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(10).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
					ui.Icon(c, iconListTree).FontSize(15).TextColor(pal.muted)
					ui.Text(c, locales.AiChats()).FontSize(14).FontWeight(600).TextColor(pal.text).Grow(1).SingleLine()
				})
				switch {
				case p.listed == nil:
					ui.Text(c, locales.AiReading()).FontSize(13).TextColor(pal.muted).Padding(12, 16)
				case len(p.listed) == 0:
					ui.Text(c, locales.AiNoChats()).FontSize(13).TextColor(pal.muted).Padding(12, 16)
				default:
					ui.Scroll(c.Key("ai-chat-list")).MaxHeight(380).Padding(6).Children(func() {
						for _, chat := range p.listed {
							a.aiChatRow(c, chat)
						}
					})
				}
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.post(func() {
					p.chats = false
					a.invalidate()
				})
			}
		})
	})
}

// aiChatRow is one conversation of the list.
func (a *app) aiChatRow(c *ui.Context, chat ai.Chat) {
	pal := a.pal
	row := ui.ButtonBase(c.Key("ai-chat:"+chat.ID)).Padding(5, 10).Gap(10).Radius(6).
		Justify(ui.Start).AlignItems(ui.Center).Label(chat.Title)
	if row.Hovered() {
		row.Background(pal.hover)
	}
	if row.Clicked() {
		a.post(func() { a.openChat(chat.ID) })
	}
	row.Transition(hoverFade)
	row.Children(func() {
		ui.Icon(c, iconClock).FontSize(14).TextColor(pal.muted)
		ui.Text(c, chat.Title).FontSize(13).TextColor(pal.text).SingleLine().Grow(1)
		ui.Text(c, ago(chat.When, time.Now())).FontSize(11).TextColor(pal.faint).SingleLine()
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
				if len(msg.tools) > 0 {
					ui.Text(c, locales.AiToolRan(strings.Join(msg.tools, ", "))).FontSize(11).
						TextColor(p.faint).SingleLine()
				}
			})
		}
		if ai.outOfSteps {
			ui.Text(c, locales.AiStepsOut()).FontSize(11).TextColor(p.muted).Padding(6, 8)
		}
	})
}

// aiApprovalCard is a write waiting for Approve or Reject, with the lines
// it changes.
func (a *app) aiApprovalCard(c *ui.Context) {
	p := a.pal
	ap := a.ai.approval
	if ap == nil {
		return
	}
	ui.Column(c.Key("ai-approval")).Margin(6).Padding(8).Gap(6).Radius(6).
		Background(p.surface).Border(1, p.border).Children(func() {
		// A command has the line it runs; a write has the lines it changes.
		if ap.change.Command != "" {
			ui.Text(c, locales.AiApproveRunTitle()).FontSize(11).FontWeight(600).TextColor(p.muted).SingleLine()
			ui.Text(c, ap.change.Command).FontSize(12).TextColor(p.text)
		} else {
			ui.Text(c, locales.AiApproveTitle()).FontSize(11).FontWeight(600).TextColor(p.muted).SingleLine()
			ui.Text(c, filepath.Base(ap.change.Path)).FontSize(12.5).TextColor(p.text).SingleLine()
		}
		if d := changedLines(ap.change.Before, ap.change.After); d != "" {
			ui.Scroll(c.Key("ai-approval-diff")).MaxHeight(160).Children(func() {
				for _, line := range strings.Split(d, "\n") {
					col := p.text
					switch {
					case strings.HasPrefix(line, "+ "):
						col = p.addedFg
					case strings.HasPrefix(line, "- "):
						col = p.removedFg
					}
					ui.Text(c, line).FontSize(11.5).TextColor(col).SingleLine()
				}
			})
		}
		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			reject := ui.Button(c, locales.AiReject())
			reject.FontSize(12).Padding(6, 8)
			if reject.Clicked() {
				a.post(func() { a.answered(false) })
			}
			approve := ui.Button(c, locales.AiApprove())
			approve.FontSize(12).Padding(6, 8)
			if approve.Clicked() {
				a.post(func() { a.answered(true) })
			}
		})
	})
}

// changedLines are the lines a change adds or removes, signed, at most 80
// of them. The lines are compared in place, so that a line that stayed
// where it was is neither added nor removed, and an empty file has no
// lines at all.
func changedLines(before, after []byte) string {
	old, new := linesOf(before), linesOf(after)
	// The shared start and end are not the change.
	for len(old) > 0 && len(new) > 0 && old[0] == new[0] {
		old, new = old[1:], new[1:]
	}
	for len(old) > 0 && len(new) > 0 && old[len(old)-1] == new[len(new)-1] {
		old, new = old[:len(old)-1], new[:len(new)-1]
	}
	// What is left is one block: the old lines go, the new ones come.
	var out []string
	for _, line := range old {
		out = append(out, "- "+line)
	}
	for _, line := range new {
		out = append(out, "+ "+line)
	}
	if len(out) > 80 {
		out = append(out[:80], "…")
	}
	return strings.Join(out, "\n")
}

// splitLines is the lines of a file, none for an empty one.
func linesOf(b []byte) []string {
	s := strings.TrimSuffix(string(b), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// aiCompose is the field what is asked is typed in.
func (a *app) aiCompose(c *ui.Context) {
	p := a.pal
	ai := &a.ai
	field := ui.Row(c.Key("ai-compose")).Height(30).Padding(0, 4, 0, 10).Gap(2).AlignItems(ui.Center).
		Background(p.editor).BorderWidth(1, 0, 0, 0).BorderColor(p.border).Transition(fieldFocus)
	field.Children(func() {
		placeholder := locales.AiPlaceholder()
		// A list over the panel has the keyboard: what is typed is not a
		// question, and Escape closes the list.
		veiled := ai.chats || ai.menu
		in := a.textKeys(ui.TextInputBase(c.Key("ai-input"), &ai.prompt)).Grow(1).MinWidth(0).
			FontSize(12.5).Placeholder(placeholder).Label(placeholder).Disabled(veiled)
		in.Bind(&ai.keyed)
		// Escape stops the answer under way, as Enter starts one.
		if ai.running() && in.Shortcut(0, ui.KeyEscape) {
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
