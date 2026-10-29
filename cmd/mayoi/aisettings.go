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
	// perm is what the assistant may do without asking, as the presets of
	// internal/settings name it.
	perm string
	// steps is how many turns one question may take, and seconds how long
	// a command may run, as the dialog's number fields show them.
	steps   float64
	seconds float64
	// extra is what the user adds to every prompt.
	extra string
	// voices are the personas the user wrote, after the built-in five.
	voices []settings.Persona
	// err is what is wrong with what was typed, shown under the fields.
	err string
	// scroll is how far the fields are scrolled, which the bar beside them
	// is drawn from: a dialog taller than the window says so.
	scroll ui.ScrollState
}

// aiConfRowHeight is how tall a row of the models is, and aiConfRowGap the
// room the box leaves around them: the two say how many rows the box shows
// in a window, which is what the count of the rest must match.
const (
	aiConfRowHeight = 22
	aiConfRowGap    = 4
)

// aiConfRowCount is how many models the box shows at once in a window.
func (a *app) aiConfRowCount() int {
	return max(int((a.aiConfListHeight()-aiConfRowGap)/aiConfRowHeight), 2)
}

// aiConfListHeight is how tall the box of models is in a window: a share of
// it, so that the fields beside the list stay in the window.
func (a *app) aiConfListHeight() float32 {
	if a.winH == 0 {
		return 110
	}
	return min(max(a.winH*0.14, 56), 160)
}

// aiConfW is how wide the dialog is, and aiConfPad how far its content is
// from its sides: a box that scrolls is given its width, as a box that
// grows to its parent would be as wide as the longest row. Two columns of
// fields fit side by side in it.
const (
	aiConfW   = 820
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
	c.perm = a.settings.AI.Permission()
	// The fields show what one question does now, defaults included, so
	// that a number typed here is the one the next question uses.
	c.steps = float64(a.steps())
	c.seconds = a.commandTimeout().Seconds()
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
// does not list, and a proxy's names are the user's to know. It goes to the
// top of the list, so that a list of many models shows the one just added.
func (a *app) aiConfAdd() {
	c := &a.aiConf
	model := strings.TrimSpace(c.typed)
	if model == "" {
		return
	}
	if slicesIndex(c.listed, model) < 0 {
		c.listed = append([]string{model}, c.listed...)
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
// typed, as many as the box shows, and how many more there are.
func (a *app) aiConfShown(c *aiSettings) (shown []string, more int) {
	filter := strings.ToLower(strings.TrimSpace(c.filter))
	rows := a.aiConfRowCount()
	for _, m := range c.listed {
		if filter != "" && !strings.Contains(strings.ToLower(m), filter) {
			continue
		}
		if len(shown) == rows {
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
	// A name typed but not kept with Enter is kept here: typing a model and
	// pressing Save is how one gets in, and dropping it there would lose it
	// without a word.
	if typed := strings.TrimSpace(c.typed); typed != "" {
		if slicesIndex(c.models, typed) < 0 {
			c.models = append(c.models, typed)
		}
		c.model = typed
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
			c.err = locales.AiConfSaveFailed(err.Error())
			return
		}
		keys[c.protocol] = strings.TrimSpace(c.key)
		if err := ai.SaveKeys(a.settings.AIPath(), keys); err != nil {
			// The key stays where it is: a dialog that cannot save says
			// so, and which file it could not write, rather than closing
			// as if it had.
			c.err = locales.AiConfSaveFailed(err.Error())
			return
		}
	}
	s := a.settings
	s.AI.Protocol, s.AI.BaseURL = c.protocol, strings.TrimSpace(c.base)
	s.AI.Model, s.AI.Models = model, c.models
	s.AI.Permissions = c.perm
	s.AI.MaxSteps = int(c.steps)
	s.AI.CommandSeconds = int(c.seconds)
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

// aiConfBar draws the bar of the dialog's fields: what share of them the
// window shows, and where in them it looks. It is there as long as there is
// more to see, which is what tells the user the dialog goes on below.
func (a *app) aiConfBar(pt *ui.Painter, r ui.Rect, s *ui.ScrollState) {
	if s.MaxY <= 0 || r.H <= 0 {
		return
	}
	p := a.pal
	const w, gap = 4, 2
	track := ui.Rect{X: r.X + r.W - w - gap, Y: r.Y, W: w, H: r.H}
	content := r.H + s.MaxY
	thumb := max(r.H*r.H/content, 28)
	if thumb > r.H {
		thumb = r.H
	}
	y := r.Y + (r.H-thumb)*(s.Y/s.MaxY)
	pt.Fill(track, p.border, w/2)
	pt.Fill(ui.Rect{X: track.X, Y: y, W: w, H: thumb}, p.faint, w/2)
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
				Margin(titleBarH+8, 0, 0, 0).Padding(14, aiConfPad).Gap(8).Radius(10), true).
				Role(ui.RoleDialog).Label(locales.AiConfTitle())
			box.Children(func() {
				// The fields scroll, as the dialog holds more than a short
				// window shows: the rows of buttons stay under them, in
				// sight, and a bar along their side says how much more
				// there is. The window's own bar shows under the pointer
				// alone, which a dialog that is cut short cannot wait for.
				body := ui.Scroll(c.Key("ai-conf-body")).Grow(1).MinHeight(0).
					TrackScroll(&conf.scroll).
					DrawOver(func(pt *ui.Painter, r ui.Rect) {
						a.aiConfBar(pt, r, &conf.scroll)
					})
				body.Children(func() {
					ui.Column(c).Gap(8).Children(func() {
						ui.Text(c, locales.AiConfTitle()).FontSize(14).FontWeight(600).TextColor(p.text).SingleLine()
						a.aiConfFields(c, conf)
						if conf.err != "" {
							ui.Text(c, conf.err).FontSize(11.5).TextColor(p.dangerText)
						}
					})
				})
				// Where the conversations go, which a machine that keeps
				// Mayoi's files beside the executable makes worth saying.
				ui.Text(c, locales.AiConfKeptIn(a.settings.SessionsPath())).FontSize(11).
					TextColor(p.faint).SingleLine()
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
// endpoint and what the assistant may do down the left, the models and the
// voices down the right. Two columns, because one holds more than a window
// of the usual height shows.
func (a *app) aiConfFields(c *ui.Context, conf *aiSettings) {
	ui.Row(c).Gap(22).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Key("ai-conf-left").Grow(1).MinWidth(0).Gap(8).Children(func() {
			a.aiConfEndpoint(c, conf)
			a.aiConfPermissions(c, conf)
			a.aiConfExtra(c, conf)
		})
		ui.Column(c).Key("ai-conf-right").Grow(1).MinWidth(0).Gap(8).Children(func() {
			a.aiConfModels(c, conf)
			a.aiConfVoices(c, conf)
			a.aiConfLimits(c, conf)
		})
	})
}

// aiConfEndpoint is how the assistant speaks to its service: the protocol,
// where it is, and the key it is asked with. The key is never shown, and
// never in the settings file.
func (a *app) aiConfEndpoint(c *ui.Context, conf *aiSettings) {
	p := a.pal
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
	a.aiConfLabelWhy(c, locales.AiConfBaseURL(), locales.AiConfBaseURLWhy())
	base := a.textKeys(ui.TextInputBase(c.Key("ai-conf-base"), &conf.base)).FontSize(12.5).
		Placeholder(ai.DefaultBaseURL(conf.protocol)).Label(locales.AiConfBaseURL())
	if base.Submitted() {
		a.post(a.saveAISettings)
	}

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
}

// aiConfExtra is what the user adds to every prompt, in every voice.
func (a *app) aiConfExtra(c *ui.Context, conf *aiSettings) {
	p := a.pal
	a.aiConfLabel(c, locales.AiConfExtra())
	// A row of its own, so that the column does not stretch the field to
	// the height of the fields beside it.
	ui.Row(c).AlignItems(ui.Start).Children(func() {
		ui.TextArea(c.Key("ai-conf-extra"), &conf.extra).FontSize(12).Height(52).
			Background(p.editor).Padding(6, 8).Radius(6)
	})
	ui.Text(c, locales.AiConfExtraWhy()).FontSize(11).TextColor(p.faint).SingleLine()
}

// aiConfModels is what the endpoint says it offers, and which of those the
// assistant may ask for.
func (a *app) aiConfModels(c *ui.Context, conf *aiSettings) {
	p := a.pal
	a.aiConfLabelWhy(c, locales.AiConfModels(), locales.AiConfModelsWhy())
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
	if len(conf.listed) > a.aiConfRowCount() {
		a.textKeys(ui.TextInputBase(c.Key("ai-conf-filter"), &conf.filter)).FontSize(12).
			Placeholder(locales.AiConfFilter()).Label(locales.AiConfFilter())
	}
	// The rows scroll in a box of their own, so that a list
	// of hundreds does not push the buttons out of the window.
	shown, more := a.aiConfShown(conf)
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
	// A name of the user's own, for a model the endpoint did not think
	// to list: Enter keeps it, and so does the button, which also takes
	// the keyboard to the field when there is nothing in it to keep.
	ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
		field := a.textKeys(ui.TextInputBase(c.Key("ai-conf-model"), &conf.typed)).FontSize(12).
			Placeholder(locales.AiConfModelPlaceholder()).Label(locales.AiConfModelPlaceholder()).
			Grow(1).MinWidth(0)
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
	// Which model answers, said under the field: a list of many models
	// scrolls, and the one just typed in need not be of the rows in view.
	if conf.model != "" {
		ui.Text(c, locales.AiConfAnswersWith(conf.model)).FontSize(11).TextColor(p.faint).SingleLine()
	}
}

// aiConfVoices is the voices the user wrote, each a name and what it is
// told: the built-in five are not edited, as they come with Mayoi.
func (a *app) aiConfVoices(c *ui.Context, conf *aiSettings) {
	p := a.pal
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

// aiConfLabelWhy is what a group of fields is called, with what it means
// beside it: a dialog of many fields says such things on the line the name
// is on, rather than under it, so that fewer of them go past the window.
func (a *app) aiConfLabelWhy(c *ui.Context, label, why string) {
	ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(11.5).FontWeight(600).TextColor(a.pal.muted).SingleLine()
		ui.Text(c, why).FontSize(11).TextColor(a.pal.faint).SingleLine().Shrink(1)
	})
}

// aiConfPermissions is what the assistant may do in the folder without
// asking, as the presets it is chosen from: the one in use is marked, and
// the one that asks for nothing says what it costs.
func (a *app) aiConfPermissions(c *ui.Context, conf *aiSettings) {
	p := a.pal
	a.aiConfLabelWhy(c, locales.AiConfPermissions(), locales.AiConfPermissionsWhy())
	ui.Column(c).Key("ai-conf-perms").Gap(1).Children(func() {
		for _, perm := range settings.Permissions() {
			name := perm
			a.aiPermissionRow(c, conf.perm, name, func() {
				a.post(func() {
					conf.perm = name
					a.invalidate()
				})
			})
		}
	})
	if conf.perm == settings.PermFullAccess {
		ui.Text(c, locales.AiConfPermissionRisk()).FontSize(11).TextColor(p.dangerText)
	}
	// The programs the user allowed from a card, which run without one:
	// the way back to a card for each is here, since a card is where they
	// were allowed.
	if allowed := a.settings.AI.Allowed; len(allowed) > 0 && conf.perm != settings.PermReadOnly {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, locales.AiConfAllowed(strings.Join(allowed, ", "))).FontSize(11).
				TextColor(p.faint).Grow(1).MinWidth(0).SingleLine()
			again := ui.Button(c, locales.AiConfForgetAllowed()).FontSize(11).Padding(2, 6).Shrink(0)
			if again.Clicked() {
				a.post(a.forgetAllowed)
			}
		})
	}
}

// forgetAllowed stops trusting every program, so that the next command
// shows a card again.
func (a *app) forgetAllowed() {
	a.settings.AI.ForgetAllowed()
	a.settings.Changed()
	a.invalidate()
}

// aiConfLimits is how far one question goes: how many turns the assistant
// may take before it stops, and how long a command it runs may take.
// Neither is a way to make it work better, only to let it work longer.
func (a *app) aiConfLimits(c *ui.Context, conf *aiSettings) {
	ui.Column(c).Gap(6).Children(func() {
		a.aiConfLimit(c, "ai-conf-steps", locales.AiConfSteps(), locales.AiConfStepsWhy(),
			&conf.steps, 1, 500, 1)
		a.aiConfLimit(c, "ai-conf-seconds", locales.AiConfCommandTimeout(), locales.AiConfCommandTimeoutWhy(),
			&conf.seconds, 5, 1800, 30)
	})
}

// aiConfLimit is a number the settings keep, the input that says it at the
// right of its name, and the line under them that says what it means.
func (a *app) aiConfLimit(c *ui.Context, key, label, why string, value *float64, lo, hi, step float64) {
	p := a.pal
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(12.5).TextColor(p.text).Grow(1).SingleLine()
		ui.NumberInput(c.Key(key), value, lo, hi, step).Width(180)
	})
	ui.Text(c, why).FontSize(11).TextColor(p.faint).SingleLine()
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
