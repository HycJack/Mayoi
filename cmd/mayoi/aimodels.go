package main

import (
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// aiModelButton is the model that answers, in the head of the panel: it
// says which one it is and opens the list that changes it, so that a
// question in another voice need not go through the settings.
func (a *app) aiModelButton(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("ai-model-button")).Padding(2, 6).Gap(4).Radius(4).Height(22).
		Justify(ui.Start).AlignItems(ui.Center).Label(locales.AiModelTitle())
	if b.Hovered() {
		b.Background(p.hover)
	}
	if b.Clicked() {
		a.post(a.toggleModels)
	}
	b.Children(func() {
		ui.Icon(c, iconBox).FontSize(12).TextColor(p.muted)
		ui.Icon(c, iconChevronDown).FontSize(11).TextColor(p.faint)
	})
}

// toggleModels opens or closes the list of models, closing the other lists
// the panel shows over it.
func (a *app) toggleModels() {
	p := &a.ai
	p.models = !p.models
	p.menu, p.chats, p.perms = false, false, false
	a.invalidate()
}

// aiModelNames are the models the panel offers: the ones the settings keep,
// and the one that answers, which a settings file of an older version may
// name without keeping.
func (a *app) aiModelNames() []string {
	names := append([]string(nil), a.settings.AI.Models...)
	if model := a.settings.AI.Model; model != "" && slicesIndex(names, model) < 0 {
		names = append([]string{model}, names...)
	}
	return names
}

// aiModelMenu is the list of models over the panel, the one that answers
// marked, with the way to the settings that add and drop them.
func (a *app) aiModelMenu(c *ui.Context) {
	p := &a.ai
	if !p.models {
		return
	}
	pal := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "ai-models")
		// Over the panel its button is in, not over the editor.
		back.AlignItems(ui.End)
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(340).MaxWidthPercent(92).
				Margin(titleBarH+panelHeadH+4, 10, 0, 0).Radius(10), true).Clip().
				Role(ui.RoleDialog).Label(locales.AiModelTitle())
			box.Children(func() {
				ui.Row(c).Padding(10, 14).Gap(8).AlignItems(ui.Center).
					BorderWidth(0, 0, 1, 0).BorderColor(pal.border).Children(func() {
					ui.Icon(c, iconBox).FontSize(15).TextColor(pal.muted)
					ui.Text(c, locales.AiModelTitle()).FontSize(13).FontWeight(600).
						TextColor(pal.text).Grow(1).SingleLine()
				})
				names := a.aiModelNames()
				if len(names) == 0 {
					ui.Text(c, locales.AiModelNone()).FontSize(12.5).TextColor(pal.muted).Padding(10, 14)
				} else {
					ui.Scroll(c.Key("ai-model-list")).MaxHeight(320).Padding(4).Children(func() {
						ui.Column(c).Gap(1).Children(func() {
							for _, name := range names {
								a.aiModelRow(c, name)
							}
						})
					})
				}
				ui.Row(c).Padding(6, 8).BorderWidth(1, 0, 0, 0).BorderColor(pal.border).Children(func() {
					more := ui.Button(c, locales.AiModelMore()).FontSize(11.5).Padding(4, 8).Justify(ui.Start)
					more.Background(pal.hover).TextColor(pal.muted)
					if more.Clicked() {
						a.post(func() {
							p.models = false
							a.openAISettings()
						})
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				a.post(func() {
					p.models = false
					a.invalidate()
				})
			}
		})
	})
}

// aiModelRow is one model of the list: the one that answers marked, and a
// click on another makes it the one.
func (a *app) aiModelRow(c *ui.Context, model string) {
	pal := a.pal
	on := model == a.settings.AI.Model
	row := ui.ButtonBase(c.Key("ai-model:"+model)).Padding(4, 8).Gap(8).Radius(6).
		Justify(ui.Start).AlignItems(ui.Center).Label(model)
	if row.Hovered() {
		row.Background(pal.hover)
	}
	if row.Clicked() && !on {
		a.post(func() { a.chooseModel(model) })
	}
	row.Children(func() {
		a.pickMark(c, on, "ai-models")
		ui.Text(c, model).FontSize(12.5).TextColor(pal.text).Grow(1).MinWidth(0).SingleLine()
	})
}

// chooseModel makes a model the one that answers, and keeps the list of
// them as it is: a model offered here is one of the kept.
func (a *app) chooseModel(model string) {
	a.ai.models = false
	if a.settings.AI.Model == model {
		a.invalidate()
		return
	}
	a.settings.AI.Model = model
	a.settings.Changed()
	a.invalidate()
}
