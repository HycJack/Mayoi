package main

import (
	"context"
	"mayoi/internal/ai"
	"mayoi/internal/bg"
	"mayoi/internal/settings"
	"mayoi/locales"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
)

// aiSettings is the dialog that configures the assistant: the endpoint it
// asks, the key it asks with, the models it may ask for, what it may do in
// the folder, and the voices it answers in. It edits a copy: nothing
// changes until Save.
type aiSettings struct {
	open bool
	// protocol is how the endpoint speaks, base where it is, "" for the
	// protocol's own service, and models the ones kept from the list it
	// gave. model is the one of them that answers.
	protocol string
	base     string
	model    string
	models   []string
	// kept is whether a key is kept for the protocol, which the dialog
	// says without showing it; key is what was typed now, "" to keep it.
	kept bool
	key  string
	// listed is what the endpoint said it offers, and filter narrows it:
	// an endpoint may offer more models than the list of rows shows, and
	// one the user knows of need not be listed at all.
	listed []string
	filter string
	// typed is the model the user typed, which Enter or the button beside
	// it keeps.
	typed string
	// fetch says what fetching did, folded into asked when it failed, and
	// fetching is whether it is asking now.
	fetch    string
	failed   bool
	fetching bool
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

// aiConfRows is how many models the dialog shows at once. The list used to
// be cut here, which left an endpoint's longer list half seen; it scrolls
// instead, in a box below the fields, so that the buttons stay in sight.
const aiConfRows = 8

// aiConfListHeight is how tall the box of models is in a window: about
// aiConfListRows of them, and never so tall that the buttons below it leave
// a window of the smallest size.
func (a *app) aiConfListHeight() float32 {
	if a.winH == 0 {
		return 130
	}
	return min(max(a.winH*0.18, 60), 200)
}

// aiConfListRows is the models the box of models shows at once.
const aiConfListRows = 6

// aiConfW is how wide the dialog is, and aiConfPad how far its content is
// from its sides: a box that scrolls is given its width, as a box that
// grows to its parent would be as wide as the longest row.
const (
	aiConfW   = 560
	aiConfPad = 18
)

// modelRowH is how tall a row of a model is, and voiceRowH a row of a
// voice: a scroll box is as tall as the rows it shows.
const (
	modelRowH = 26
	voiceRowH = 30
)

// aiConfProtocol is what a protocol is called where it is chosen.
func aiConfProtocol(protocol string) string {
	if protocol == ai.Anthropic {
		return locales.AiConfProtocolAnthropic()
	}
	return locales.AiConfProtocolOpenAI()
}

// openAISettings shows the assistant's settings, as they are now.
func (a *app) openAISettings() {
	c := &a.aiConf
	c.open = true
	c.err, c.fetch, c.failed = "", "", false
	c.protocol, c.base = a.settings.AI.Protocol, a.settings.AI.BaseURL
	if c.protocol == "" {
		keys, err := ai.LoadKeys(a.settings.AIPath())
		if err != nil {
			c.err = a.aiSaid(err)
		}
		c.protocol = ai.ProtocolOf(keys)
	}
	c.model = a.settings.AI.Model
	c.models = append([]string(nil), a.settings.AI.Models...)
	c.listed, c.filter = append([]string(nil), c.models...), ""
	c.typed = ""
	// The field says where answers go now, which is the service of the
	// protocol until another endpoint is named: a port typed after it
	// makes the other one.
	if c.base == "" {
		c.base = ai.DefaultBaseURL(c.protocol)
	}
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		c.err = a.aiSaid(err)
	}
	c.kept = keys[c.protocol] != ""
	c.key = ""
	c.approve, c.bash = a.settings.AI.ApproveEdits, a.settings.AI.AllowBash
	c.extra = a.settings.AI.SystemExtra
	c.voices = append([]settings.Persona(nil), a.settings.AI.Personas...)
	a.invalidate()
}

// aiConfPick chooses a protocol, and reads whether a key is kept for it.
func (a *app) aiConfPick(protocol string) {
	c := &a.aiConf
	if c.protocol == protocol {
		return
	}
	c.protocol = protocol
	// The list is the protocol's: what another endpoint offers says
	// nothing of this one.
	c.listed, c.fetch, c.failed = append([]string(nil), c.models...), "", false
	keys, err := ai.LoadKeys(a.settings.AIPath())
	if err != nil {
		c.err = a.aiSaid(err)
	}
	c.kept = keys[protocol] != ""
	a.invalidate()
}

// aiConfKeep keeps a model, or drops it. Dropping the one that answers
// leaves the first of the rest answering, which is none of them.
func (a *app) aiConfKeep(model string, keep bool) {
	c := &a.aiConf
	i := slicesIndex(c.models, model)
	switch {
	case keep && i < 0:
		c.models = append(c.models, model)
		if c.model == "" {
			c.model = model
		}
	case !keep && i >= 0:
		c.models = append(c.models[:i:i], c.models[i+1:]...)
		if c.model == model {
			c.model = ""
			if len(c.models) > 0 {
				c.model = c.models[0]
			}
		}
	}
	a.invalidate()
}

// aiConfUse makes a model the one that answers, which keeps it: a model
// that answers is one of the kept.
func (a *app) aiConfUse(model string) {
	c := &a.aiConf
	if slicesIndex(c.models, model) < 0 {
		c.models = append(c.models, model)
	}
	c.model = model
	a.invalidate()
}

// aiConfAdd keeps the model the user typed: an endpoint may offer one it
// does not list, and a proxy's names are the user's to know.
func (a *app) aiConfAdd() {
	c := &a.aiConf
	model := strings.TrimSpace(c.typed)
	if model == "" {
		return
	}
	if slicesIndex(c.listed, model) < 0 {
		c.listed = append(c.listed, model)
	}
	a.aiConfUse(model)
	c.typed = ""
}

// slicesIndex is where a model sits in what is kept, or -1.
func slicesIndex(models []string, model string) int {
	for i, m := range models {
		if m == model {
			return i
		}
	}
	return -1
}

// fetchAIModels asks the endpoint which models it offers. It asks with the
// key typed, or the one kept: a key given here is enough to look.
func (a *app) fetchAIModels() {
	c := &a.aiConf
	if c.fetching {
		return
	}
	protocol, base := c.protocol, strings.TrimSpace(c.base)
	key := strings.TrimSpace(c.key)
	if key == "" {
		keys, err := ai.LoadKeys(a.settings.AIPath())
		if err != nil {
			c.err = a.aiSaid(err)
			return
		}
		key = keys[protocol]
	}
	c.fetching, c.fetch, c.failed = true, locales.AiConfFetching(), false
	a.invalidate()
	// The progress a frame is what draws the spinner; the asking is the
	// endpoint's, which waits, so it runs on a thread of its own.
	go func() {
		models, err := ai.FetchModels(context.Background(), protocol, base, key)
		bg.Go(func() {
			defer recoverCrash()
			defer a.invalidate()
			a.post(func() {
				c.fetching = false
				if err != nil {
					c.fetch, c.failed = a.aiSaid(err), true
					return
				}
				c.listed, c.failed = models, false
				c.fetch = locales.AiConfFetched(len(models))
				// A model kept from another endpoint is not in this list,
				// and stays kept: the endpoint answers for what it is asked.
				for _, m := range c.models {
					if slicesIndex(models, m) < 0 {
						c.listed = append(c.listed, m)
					}
				}
			})
		})
	}()
}

// aiConfShown is the models the dialog lists: those matching what was
// typed, at most aiConfRows of them, and how many more there are.
func aiConfShown(c *aiSettings) (shown []string, more int) {
	filter := strings.ToLower(strings.TrimSpace(c.filter))
	for _, m := range c.listed {
		if filter != "" && !strings.Contains(strings.ToLower(m), filter) {
			continue
		}
		if len(shown) == aiConfRows {
			more++
			continue
		}
		shown = append(shown, m)
	}
	return shown, more
}

// saveAISettings writes what the dialog shows, and closes it.
func (a *app) saveAISettings() {
	c := &a.aiConf
	if c.protocol != ai.OpenAI && c.protocol != ai.Anthropic {
		c.err = locales.AiConfNoProtocol()
		return
	}
	model := c.model
	if model == "" && len(c.models) > 0 {
		model = c.models[0]
	}
	if model == "" {
		c.err = locales.AiConfNoModel()
		return
	}
	if c.key != "" {
		keys, err := ai.LoadKeys(a.settings.AIPath())
		if err != nil {
			c.err = a.aiSaid(err)
			return
		}
		keys[c.protocol] = strings.TrimSpace(c.key)
		if err := ai.SaveKeys(a.settings.AIPath(), keys); err != nil {
			// The key stays where it is: a dialog that cannot save says
			// so rather than closing as if it had.
			c.err = a.aiSaid(err)
			return
		}
	}
	s := a.settings
	s.AI.Protocol, s.AI.BaseURL = c.protocol, strings.TrimSpace(c.base)
	s.AI.Model, s.AI.Models = model, c.models
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
			box := a.floating(c, ui.Column(c).Width(aiConfW).MaxWidthPercent(94).MaxHeightPercent(90).
				Margin(titleBarH+8, 0, 0, 0).Padding(16, aiConfPad).Gap(10).Radius(10), true).
				Role(ui.RoleDialog).Label(locales.AiConfTitle())
			box.Children(func() {
				// The fields scroll, as the dialog is taller than some
				// windows: the rows of buttons stay under them, in sight.
				ui.Scroll(c.Key("ai-conf-body")).Grow(1).MinHeight(0).Children(func() {
					ui.Column(c).Gap(10).Children(func() {
						ui.Text(c, locales.AiConfTitle()).FontSize(14).FontWeight(600).TextColor(p.text).SingleLine()
						a.aiConfFields(c, conf)
						if conf.err != "" {
							ui.Text(c, conf.err).FontSize(11.5).TextColor(p.dangerText)
						}
					})
				})
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

// aiConfFields is what the dialog shows of the assistant's settings: the
// endpoint, the models, what the assistant may do, and the voices.
func (a *app) aiConfFields(c *ui.Context, conf *aiSettings) {
	p := a.pal
	// The endpoint: how it speaks, where it is, and the key.
	a.aiConfLabel(c, locales.AiConfProtocol())
	ui.Row(c).Gap(6).Wrap().Children(func() {
		for _, name := range ai.Protocols() {
			chosen := name == conf.protocol
			b := ui.Button(c, aiConfProtocol(name))
			b.FontSize(11.5).Padding(4, 8)
			if chosen {
				b.Background(p.accent).TextColor(p.fillText)
			}
			if b.Clicked() && !chosen {
				protocol := name
				a.post(func() { a.aiConfPick(protocol) })
			}
		}
	})
	a.aiConfLabel(c, locales.AiConfBaseURL())
	base := a.textKeys(ui.TextInputBase(c.Key("ai-conf-base"), &conf.base)).FontSize(12.5).
		Placeholder(ai.DefaultBaseURL(conf.protocol)).Label(locales.AiConfBaseURL())
	if base.Submitted() {
		a.post(a.saveAISettings)
	}
	ui.Text(c, locales.AiConfBaseURLWhy()).FontSize(11).TextColor(p.faint).SingleLine()

	// The key: never shown, and never in the settings file.
	a.aiConfLabel(c, locales.AiConfKey())
	hint := locales.AiConfKeyWhy()
	if conf.kept {
		hint = locales.AiConfKeyKept(ai.DisplayName(conf.protocol))
	}
	in := a.textKeys(ui.TextInputBase(c.Key("ai-conf-key"), &conf.key)).FontSize(12.5).
		Placeholder(locales.AiConfKeyPlaceholder()).Label(locales.AiConfKey())
	if in.Submitted() {
		a.post(a.saveAISettings)
	}
	ui.Text(c, hint).FontSize(11).TextColor(p.faint).SingleLine()

	// The models: what the endpoint says it offers, and which
	// of them the assistant may ask for.
	a.aiConfLabel(c, locales.AiConfModels())
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		fetch := locales.AiConfFetch()
		if conf.fetching {
			fetch = locales.AiConfFetching()
		}
		b := ui.Button(c, fetch).FontSize(11.5).Padding(4, 8).Disabled(conf.fetching)
		if b.Clicked() {
			a.post(a.fetchAIModels)
		}
		if conf.fetch != "" {
			why := p.muted
			if conf.failed {
				why = p.dangerText
			}
			ui.Text(c, conf.fetch).FontSize(11).TextColor(why).SingleLine().Shrink(1)
		}
	})
	ui.Text(c, locales.AiConfModelsWhy()).FontSize(11).TextColor(p.faint).SingleLine()
	if len(conf.listed) > aiConfRows {
		a.textKeys(ui.TextInputBase(c.Key("ai-conf-filter"), &conf.filter)).FontSize(12).
			Placeholder(locales.AiConfFilter()).Label(locales.AiConfFilter())
	}
	// The rows scroll in a box of their own, so that a list
	// of hundreds does not push the buttons out of the window.
	shown, more := aiConfShown(conf)
	if len(shown) > 0 {
		rows := func() {
			for _, model := range shown {
				a.aiConfModelRow(c, model)
			}
		}
		if len(shown) == 1 {
			// A lone row is laid out at the width of the column it is
			// in: in a column of one row, that is the dialog's.
			ui.Row(c).Children(rows)
		} else {
			ui.Row(c).Children(func() {
				// The box is a share of the window, not of the dialog: the
				// dialog is as tall as what it holds, and a list of
				// hundreds would take the buttons out of the window.
				ui.Scroll(c.Key("ai-conf-list")).Height(a.aiConfListHeight()).
					Background(p.editor).Radius(6).Padding(2).Children(func() {
					ui.Column(c).Key(c.Key("ai-conf-rows")).Gap(1).Children(rows)
				})
			})
		}
	}
	if more > 0 {
		ui.Text(c, locales.AiConfMore(more)).FontSize(11).TextColor(p.faint).SingleLine()
	}
	if len(shown) == 0 {
		ui.Text(c, locales.AiConfNoModels()).FontSize(11.5).TextColor(p.faint).SingleLine()
	}
	// And a name of the user's own, for a model the endpoint did not think
	// to list: Enter keeps it, and so does the button, which also takes
	// the keyboard to the field when there is nothing in it to keep.
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		field := a.textKeys(ui.TextInputBase(c.Key("ai-conf-model"), &conf.typed)).FontSize(12).
			Placeholder(locales.AiConfModelPlaceholder()).Label(locales.AiConfModelType()).Grow(1).MinWidth(0)
		if field.Submitted() {
			a.post(a.aiConfAdd)
		}
		if strings.TrimSpace(conf.typed) == "" {
			// The field came back with a space where Enter cleared it:
			// nothing typed is nothing to keep.
			conf.typed = ""
		}
		b := ui.Button(c, locales.AiConfModelType()).FontSize(11.5).Padding(4, 8).Shrink(0)
		if b.Clicked() {
			// A press with something in the field keeps it; with nothing
			// there, it takes the keyboard to the field to type one. The
			// field comes after the button in the frame, so it says the
			// text of the frame before.
			if strings.TrimSpace(conf.typed) == "" {
				field.Focus()
			} else {
				a.post(a.aiConfAdd)
			}
		}
	})

	a.aiConfCheck(c, &conf.approve, locales.AiConfApprove(), locales.AiConfApproveWhy())
	a.aiConfCheck(c, &conf.bash, locales.AiConfBash(), locales.AiConfBashWhy())

	a.aiConfLabel(c, locales.AiConfExtra())
	ui.TextArea(c.Key("ai-conf-extra"), &conf.extra).FontSize(12).Height(36).
		Background(p.editor).Padding(6, 8).Radius(6)
	ui.Text(c, locales.AiConfExtraWhy()).FontSize(11).TextColor(p.faint).SingleLine()

	// The voices the user wrote, each a name and what it is
	// told, and the built-in five are not edited: they come
	// with Mayoi.
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, locales.AiConfVoices()).FontSize(12.5).TextColor(p.text).SingleLine()
		if ui.Button(c, locales.AiConfAddVoice()).FontSize(11.5).Padding(4, 8).Clicked() {
			a.post(func() {
				conf.voices = append(conf.voices, settings.Persona{})
			})
		}
	})
	if len(conf.voices) > 0 {
		// Each voice is a row of what the next one's build
		// cannot be measured from: a column of them is as
		// tall as what it had measured, and settles on the
		// frame after, which is a dialog twice as tall.
		ui.Row(c).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				for i := range conf.voices {
					a.aiConfVoiceRow(c, i)
				}
			})
		})
	}
}

// aiConfModelRow is one model the endpoint offers: the way to keep it or
// drop it, and the model itself, which answers when it is clicked.
func (a *app) aiConfModelRow(c *ui.Context, model string) {
	conf := &a.aiConf
	p := a.pal
	kept := slicesIndex(conf.models, model) >= 0
	mark, label := "+", locales.AiConfKeep()
	if kept {
		mark, label = "✓", locales.AiConfDrop()
	}
	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		keep := ui.Button(c, mark).FontSize(11.5).Padding(2, 6).Label(label)
		keep.Background(p.editor).TextColor(p.muted)
		if keep.Clicked() {
			was := kept
			a.post(func() { a.aiConfKeep(model, !was) })
		}
		use := ui.Button(c, model).FontSize(11.5).Padding(4, 8).Justify(ui.Start).Grow(1)
		if model == conf.model {
			use.Background(p.selection).TextColor(p.text)
		} else {
			use.Background(p.editor)
		}
		if use.Clicked() {
			a.post(func() { a.aiConfUse(model) })
		}
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
