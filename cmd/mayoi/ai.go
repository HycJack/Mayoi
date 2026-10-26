package main

import (
	"strconv"

	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// aiMinW is the least the assistant's panel is wide.
const aiMinW = 240

// aiPanel is the assistant's panel at the right of the window: what the
// conversation says so far.
type aiPanel struct {
	// msgs are the messages of the conversation, the user's and the
	// assistant's, in the order they were said.
	msgs []aiMessage
}

// aiMessage is a message of the conversation: whose it is, "user" or
// "assistant", and what it says.
type aiMessage struct {
	role string
	text string
}

// toggleAI shows the assistant's panel, or hides it.
func (a *app) toggleAI() {
	a.window.AIOpen = !a.window.AIOpen
	a.settings.Changed()
	a.invalidate()
}

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
	})
}

// aiTranscript is what was said in the conversation so far, or what to
// start with.
func (a *app) aiTranscript(c *ui.Context) {
	p := a.pal
	if len(a.ai.msgs) == 0 {
		ui.Text(c, locales.AiEmpty()).FontSize(12.5).TextColor(p.muted).Padding(12, 12).Grow(1)
		return
	}
	ui.Scroll(c.Key("ai-msgs")).Grow(1).MinHeight(0).Padding(6).Children(func() {
		for i := range a.ai.msgs {
			msg := &a.ai.msgs[i]
			ui.Column(c.Key("ai-msg-"+strconv.Itoa(i))).Padding(6, 8).Gap(2).Children(func() {
				ui.Text(c, msg.role).FontSize(10).FontWeight(600).LetterSpacing(titleSpacing).
					TextColor(p.muted).SingleLine()
				ui.Text(c, msg.text).FontSize(12.5).TextColor(p.text)
			})
		}
	})
}
