package main

import (
	"mayoi/internal/ai"
	"mayoi/internal/settings"
	"mayoi/locales"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// aiSettings is the dialog that configures the assistant: the model it
// asks, the key it asks with, what it may do in the folder, and the voices
// it answers in. It edits a copy: nothing changes until Save.
type aiSettings struct {
	open bool
	// provider and model are the model chosen, and kept is whether a key
	// is kept for the provider, which the dialog says without showing it.
	provider string
	model    string
	kept     bool
	// key is what was typed now, "" to keep the key kept.
	key string
	// approve writes without asking, and bash lets it run commands.
	approve bool
	bash    bool
	// extra is what the user adds to every prompt.
	extra string
	// voices are the personas the user wrote, after the built-in five.
	voices []settings.Persona
	// err is what is wrong with what was typed, shown under the fields.
	err string
}

// openAISettings shows the assistant's settings, as they are now.
func (a *app) openAISettings() {
	c := &a.aiConf
	c.open = true
	c.err = ""
	c.provider, c.model = a.settings.AI.Provider, a.settings.AI.Model
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		c.err = a.aiSaid(err)
	}
	if c.provider == "" {
		c.provider = ai.DefaultProvider(keys)
	}
	c.kept = keys[c.provider] != ""
	c.key = ""
	c.approve, c.bash = a.settings.AI.ApproveEdits, a.settings.AI.AllowBash
	c.extra = a.settings.AI.SystemExtra
	c.voices = append([]settings.Persona(nil), a.settings.AI.Personas...)
	a.invalidate()
}

// saveAISettings writes what the dialog shows, and closes it.
func (a *app) saveAISettings() {
	c := &a.aiConf
	if c.provider == "" {
		c.err = locales.AiConfNoProvider()
		return
	}
	if c.key != "" {
		keys, err := ai.LoadKeys(a.settings.AIPath())
		if err != nil {
			c.err = a.aiSaid(err)
			return
		}
		keys[c.provider] = strings.TrimSpace(c.key)
		if err := ai.SaveKeys(a.settings.AIPath(), keys); err != nil {
			// The key stays where it is: a dialog that cannot save says
			// so rather than closing as if it had.
			c.err = a.aiSaid(err)
			return
		}
	}
	s := a.settings
	s.AI.Provider, s.AI.Model = c.provider, c.model
	s.AI.ApproveEdits, s.AI.AllowBash = c.approve, c.bash
	s.AI.SystemExtra = strings.TrimSpace(c.extra)
	s.AI.Personas = voicesOf(c.voices)
	s.Changed()
	c.open = false
	a.invalidate()
}

// voicesOf is the voices written, without the ones left half written: a
// voice with no name is not a voice.
func voicesOf(voices []settings.Persona) []settings.Persona {
	out := make([]settings.Persona, 0, len(voices))
	for _, p := range voices {
		p.Name = strings.TrimSpace(p.Name)
		if p.Name == "" {
			continue
		}
		if p.ID == "" {
			p.ID = personaID(p.Name)
		}
		out = append(out, p)
	}
	return out
}

// personaID is an id of a voice, taken from its name, that no built-in
// voice has: the id says which voice answers, and a name changes freely.
func personaID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "voice"
	}
	// A voice of the user's may not take the name of one that comes with
	// Mayoi: the setting says which answers, and the five are asked for
	// by theirs.
	for _, p := range ai.BuiltIn() {
		if p.ID == id {
			return id + "-2"
		}
	}
	return id
}

// aiSettingsView is the dialog of the assistant's settings.
func (a *app) aiSettingsView(c *ui.Context) {
	conf := &a.aiConf
	if !conf.open {
		return
	}
	p := a.pal
	ui.Overlay(c, func() {
		back := a.dialogBack(c, "ai-settings")
		back.Children(func() {
			box := a.floating(c, ui.Column(c).Width(560).MaxWidthPercent(94).MaxHeightPercent(90).
				Margin(titleBarH+8, 0, 0, 0).Padding(16, 18).Gap(12).Radius(10), true).
				Role(ui.RoleDialog).Label(locales.AiConfTitle())
			box.Children(func() {
				ui.Text(c, locales.AiConfTitle()).FontSize(14).FontWeight(600).TextColor(p.text).SingleLine()

				// The model: the provider first, then what it offers.
				a.aiConfLabel(c, locales.AiConfProvider())
				ui.Row(c).Gap(6).Wrap().Children(func() {
					for _, name := range ai.Providers() {
						chosen := name == conf.provider
						b := ui.Button(c, name)
						b.FontSize(11.5).Padding(4, 8)
						if chosen {
							b.Background(p.accent).TextColor(p.fillText)
						}
						if b.Clicked() && !chosen {
							a.post(func() {
								conf.provider, conf.model = name, ""
								keys, _ := ai.LoadKeys(a.settings.AIPath())
								conf.kept = keys[name] != ""
							})
						}
					}
				})
				a.aiConfLabel(c, locales.AiConfModel())
				ui.Scroll(c.Key("ai-conf-models")).MaxHeight(150).Children(func() {
					for _, m := range ai.Models(conf.provider) {
						chosen := m.ID == conf.model
						row := ui.Button(c, m.ID)
						row.FontSize(12).Padding(5, 10).Justify(ui.Start)
						if chosen {
							row.Background(p.selection).TextColor(p.text)
						} else {
							row.Background(p.editor)
						}
						if row.Clicked() && !chosen {
							a.post(func() { conf.model = m.ID })
						}
					}
				})

				// The key: never shown, and never in the settings file.
				a.aiConfLabel(c, locales.AiConfKey())
				hint := locales.AiConfKeyWhy()
				if conf.kept {
					hint = locales.AiConfKeyKept(conf.provider)
				}
				in := a.textKeys(ui.TextInputBase(c.Key("ai-conf-key"), &conf.key)).FontSize(12.5).
					Placeholder(locales.AiConfKeyPlaceholder()).Label(locales.AiConfKey())
				if in.Submitted() {
					a.post(a.saveAISettings)
				}
				ui.Text(c, hint).FontSize(11).TextColor(p.faint).SingleLine()

				a.aiConfCheck(c, &conf.approve, locales.AiConfApprove(), locales.AiConfApproveWhy())
				a.aiConfCheck(c, &conf.bash, locales.AiConfBash(), locales.AiConfBashWhy())

				a.aiConfLabel(c, locales.AiConfExtra())
				ui.TextArea(c.Key("ai-conf-extra"), &conf.extra).FontSize(12).Height(56).
					Background(p.editor).Padding(6, 8).Radius(6)
				ui.Text(c, locales.AiConfExtraWhy()).FontSize(11).TextColor(p.faint).SingleLine()

				// The voices the user wrote, each a name and what it is
				// told, and the built-in five are not edited: they come
				// with Mayoi.
				a.aiConfLabel(c, locales.AiConfVoices())
				for i := range conf.voices {
					a.aiConfVoiceRow(c, i)
				}
				if ui.Button(c, locales.AiConfAddVoice()).FontSize(11.5).Padding(4, 8).Clicked() {
					a.post(func() {
						conf.voices = append(conf.voices, settings.Persona{})
					})
				}

				if conf.err != "" {
					ui.Text(c, conf.err).FontSize(11.5).TextColor(p.dangerText)
				}
				ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
					if ui.Button(c, locales.ButtonCancel()).Clicked() {
						a.post(func() {
							conf.open = false
							a.invalidate()
						})
					}
					if ui.PrimaryButton(c, locales.ButtonSave()).Clicked() {
						a.post(a.saveAISettings)
					}
				})
			})
			if box.PressedOutside() || back.OverlayShortcut(0, ui.KeyEscape) {
				conf.open = false
				a.invalidate()
			}
		})
	})
}

// aiConfLabel is what a group of fields is called.
func (a *app) aiConfLabel(c *ui.Context, label string) {
	ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(a.pal.muted).SingleLine()
}

// aiConfCheck is a setting that is on or off, with the line that says what
// it means.
func (a *app) aiConfCheck(c *ui.Context, on *bool, label, why string) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Checkbox(c, on, label).FontSize(12.5)
		ui.Text(c, why).FontSize(11).TextColor(a.pal.faint).SingleLine().Shrink(1)
	})
}

// aiConfVoiceRow is one voice of the user's: what it is called, what it is
// told, and the way to be rid of it.
func (a *app) aiConfVoiceRow(c *ui.Context, i int) {
	voice := &a.aiConf.voices[i]
	ui.Row(c).Key(c.Key("ai-conf-voice-" + strconv.Itoa(i))).Gap(6).AlignItems(ui.Center).Children(func() {
		a.textKeys(ui.TextInputBase(c.Key("ai-conf-voice-name"), &voice.Name)).FontSize(12).
			Placeholder(locales.AiConfVoiceName()).Label(locales.AiConfVoiceName())
		a.textKeys(ui.TextInputBase(c.Key("ai-conf-voice-what"), &voice.Instructions)).FontSize(12).
			Placeholder(locales.AiConfVoiceWhat()).Label(locales.AiConfVoiceWhat()).Grow(1).MinWidth(0)
		if a.iconButton(c, iconTrash, locales.AiConfRemove(), false).Clicked() {
			a.post(func() {
				voices := a.aiConf.voices
				a.aiConf.voices = append(voices[:i:i], voices[i+1:]...)
			})
		}
	})
}
