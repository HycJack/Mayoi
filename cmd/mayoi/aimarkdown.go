package main

import (
	"regexp"

	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// answerMsgSize is the size of the text of an answer, in DIPs, which the
// blocks of its Markdown scale from.
const answerMsgSize float32 = 12.5

// answerTableWidth is how wide a table may be before the panel's own width
// narrows it: a table shares its room between its columns, so the width
// given only says how much it would like.
const answerTableWidth float32 = 4000

// imageInText is a picture in the text of an answer. The panel shows its
// description instead: a column of prose has no room for a picture, and one
// of the web would reach the network for it, for nothing.
var imageInText = regexp.MustCompile(`!\[([^\]\n]*)\]\(([^)\n]*)\)`)

// answerSource is the text of an answer as the panel parses it: Markdown as
// it is, but for its pictures, which their descriptions stand in for.
func answerSource(text string) string {
	return imageInText.ReplaceAllString(text, "$1")
}

// answerPreview is the Markdown of a message parsed, kept from one frame to
// the next, and parsed again only when the text changed, as it does with
// every delta of an answer.
func (a *app) answerPreview(msg *aiMessage) *markdownPreview {
	if msg.md != nil && msg.mdText == msg.text {
		return msg.md
	}
	m := msg.md
	if m == nil {
		m = &markdownPreview{}
		msg.md = m
	}
	m.blocks = parseMarkdown([]byte(answerSource(msg.text)), "", a.root, nil, &a.answerCache)
	m.tables, m.tableFont = nil, ui.Font{}
	msg.mdText = msg.text
	return m
}

// answerView draws an answer as the Markdown it is, width wide: paragraphs
// of styled text, headings, lists, quotes, tables and cards of code.
func (a *app) answerView(c *ui.Context, msg *aiMessage, width float32) {
	m := a.answerPreview(msg)
	if len(m.blocks) == 0 {
		// Nothing of the text is a block of Markdown of its own: it is
		// said as it was written.
		ui.Text(c, msg.text).FontSize(answerMsgSize).TextColor(a.pal.text)
		return
	}
	for i, b := range m.blocks {
		a.answerBlock(c, m, b, i == 0, width)
	}
}

// answerBlock draws one block of an answer, with the bars of the quotes it
// is in and the marker of the list it is in, as wide as the column it is in.
func (a *app) answerBlock(c *ui.Context, m *markdownPreview, b markdownBlock, first bool, width float32) {
	p := a.pal
	size := a.answerTextSize(b)
	top, bottom := a.answerSpacing(b, first)
	row := ui.Row(c).AlignItems(ui.Start).Gap(size*0.4).Padding(top, 0, bottom, b.offset*size)
	if width > 0 {
		row.Width(width)
	} else {
		row.WidthPercent(100)
	}
	row.Children(func() {
		for i := 0; i < b.quote; i++ {
			bar := p.guideNow
			if b.alert != alertNone && i == b.alertAt-1 {
				bar = a.markdownColor(b.alert)
			}
			ui.Box(c).Width(2).AlignSelf(ui.Stretch).Background(bar).Margin(0, size*0.2, 0, 0)
		}
		if b.marker != "" {
			a.markdownMarker(c, b, size)
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			switch b.kind {
			case markdownRule:
				ui.Box(c).Height(1).WidthPercent(100).Background(p.border).Margin(size*0.4, 0)
			case markdownCodeBlock:
				a.answerCodeCard(c, m, b, size, width)
			case markdownTable, markdownTableHeader:
				a.markdownTableRow(c, m, "", b, size, answerTableWidth,
					ui.Font{Family: c.Theme().Font, Size: size})
			case markdownAlertTitle:
				icon, title := markdownAlertHead(b.alert)
				col := a.markdownColor(b.alert)
				ui.Row(c).AlignItems(ui.Center).Gap(size * 0.4).Children(func() {
					ui.Icon(c, icon).FontSize(size * 1.1).TextColor(col)
					ui.Text(c, title).FontSize(size).FontWeight(600).TextColor(col).SingleLine()
				})
			default:
				// Quotes and the smallest headings are muted, as on
				// GitHub; the text of an alert is not.
				muted := b.quote > b.alertAt || b.kind == markdownHeading && b.level == 6
				a.markdownText(c, m, "", b.runs, size, muted, false)
				if b.kind == markdownHeading && b.level <= 2 {
					ui.Box(c).Height(1).WidthPercent(100).Background(p.border).Margin(size*0.35, 0, 0, 0)
				}
			}
		})
	})
}

// answerCodeCard is a block of code in the panel, as wide as the column:
// its language and its copy above it, its lines wrapped rather than cut at
// the edge of a panel narrower than a page.
func (a *app) answerCodeCard(c *ui.Context, m *markdownPreview, b markdownBlock, size, width float32) {
	p := a.pal
	card := ui.Column(c.Key("code")).Padding(size*0.6, size*0.85).
		Background(p.chrome).Border(1, p.border).Radius(8)
	if width > 0 {
		card.Width(width)
	}
	card.Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
			ui.Text(c, b.language).FontSize(size * 0.75).TextColor(p.faint).SingleLine().Grow(1)
			if a.smallButton(c, iconCopy, locales.CopyCode()) {
				a.services.WriteClipboard(markdownRunsText(b.runs))
				a.tell(note{message: locales.CopiedCode()})
			}
		})
		a.markdownText(c, m, "", b.runs, size*0.93, false, true)
	})
}

// answerTextSize is the size of a block's text: the panel's, scaled as
// GitHub scales its headings.
func (a *app) answerTextSize(b markdownBlock) float32 {
	base := a.settings.FontSize
	if base <= 0 {
		return answerMsgSize
	}
	return answerMsgSize * a.markdownSize(b) / base
}

// answerSpacing is the room above and below a block in the panel, nearer
// than a page's, which is as wide as a window and taller than a panel.
func (a *app) answerSpacing(b markdownBlock, first bool) (top, bottom float32) {
	gap := answerMsgSize * a.settings.LineHeight * 0.55
	bottom = gap
	if b.tight || b.kind == markdownTable || b.kind == markdownTableHeader {
		bottom = 0
	}
	switch b.kind {
	case markdownHeading:
		top, bottom = gap*1.3, gap*0.4
	case markdownAlertTitle:
		bottom = gap * 0.3
	case markdownCodeBlock:
		top, bottom = gap*0.2, gap
	}
	if first {
		top = 0
	}
	return top, bottom
}
