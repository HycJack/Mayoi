//go:build linux && (amd64 || arm64)

package text

import (
	"fmt"
	"testing"
)

// A flex item may give text exactly the width of its unconstrained
// layout. Kerning in strings such as "11" must not make Pango wrap it.
func TestPangoLayoutAtMeasuredWidth(t *testing.T) {
	s := newSystem()
	for _, family := range []string{"sans-serif", "serif", "monospace"} {
		for _, size := range []float32{10, 12, 14, 16} {
			for _, value := range []string{"11", "10", "12", "1911", "2011", "2111", "2211", "2001", "2002", "2012", "2021", "2011-04-17", "2011-06-17", "11:30", "2.11.1", "AV", "office"} {
				t.Run(fmt.Sprintf("%s/%g/%s", family, size, value), func(t *testing.T) {
					p := Params{Text: value, Style: Style{Family: family, Size: size}}
					p.Width = s.Layout(p).Width
					for _, maxLines := range []int{0, 1} {
						p.MaxLines = maxLines
						l := s.Layout(p)
						if len(l.Lines) != 1 || l.Truncated || l.Lines[0].End != len(l.Runes) {
							t.Errorf("at measured width %g, MaxLines %d: %d lines, truncated %v, end %d of %d", p.Width, maxLines, len(l.Lines), l.Truncated, l.Lines[0].End, len(l.Runes))
						}
					}
				})
			}
		}
	}
}

// Letter spacing makes a text's measured width fractional, so a flex
// item may lay it out at a width a thousandth of a dip short of its own
// measurement. Truncating the scaled width must not wrap it.
func TestPangoLayoutAtMeasuredWidthWithLetterSpacing(t *testing.T) {
	s := newSystem()
	for _, spacing := range []float32{0.1, 0.25, 0.5, 1, 1.5} {
		for _, value := range []string{"System", "Settings", "Favorites", "Home", "Now Playing"} {
			t.Run(fmt.Sprintf("%g/%s", spacing, value), func(t *testing.T) {
				p := Params{Text: value, Style: Style{Family: "sans-serif", Size: 14, LetterSpacing: spacing}}
				p.Width = s.Layout(p).Width
				for _, maxLines := range []int{0, 1} {
					p.MaxLines = maxLines
					l := s.Layout(p)
					if len(l.Lines) != 1 || l.Truncated || l.Lines[0].End != len(l.Runes) {
						t.Errorf("at measured width %g, MaxLines %d: %d lines, truncated %v, end %d of %d", p.Width, maxLines, len(l.Lines), l.Truncated, l.Lines[0].End, len(l.Runes))
					}
				}
			})
		}
	}
}

// Tabs reach their stops, as Pango lays them out, rather than taking the
// advance of the font's own tab glyph, which has none.
func TestPangoTabsReachTheirStops(t *testing.T) {
	s := newSystem()
	style := Style{Family: "monospace", Size: 13}
	// after is where the last glyph of text starts.
	after := func(text string) float32 {
		l := s.Layout(Params{Text: text, Style: style})
		g := l.Lines[0].Glyphs
		return g[len(g)-1].X
	}
	stop := after("\tx")
	if stop <= after("x") {
		t.Fatalf("x after a tab is at %g", stop)
	}
	if x := after("ab\tx"); x != stop {
		t.Errorf("x after ab and a tab is at %g, after a tab at %g", x, stop)
	}
	if x := after("\t\tx"); x != 2*stop {
		t.Errorf("x after two tabs is at %g, after one at %g", x, stop)
	}
}

// Where the desktop asks for subpixel antialiasing, the grayscale glyphs
// of a font without color glyphs stay gray, though cairo before 1.17.2
// antialiases them by their subpixels: white, they would show as colored.
func TestPangoGrayGlyphsWithSubpixels(t *testing.T) {
	s := newSystem()
	s.SetFontRendering("subpixel", "slight", "rgb")
	l := s.Layout(Params{Text: "Mayoi", Style: Style{Family: "sans-serif", Size: 13}})
	if len(l.Lines) == 0 || len(l.Lines[0].Glyphs) == 0 {
		t.Fatal("no glyphs")
	}
	e := s.engine().(*pangoEngine)
	for _, g := range l.Lines[0].Glyphs {
		b := e.glyph(g.Font, g.ID, 1, 0, 0, false)
		if b.pix == nil || b.color || b.subpixel {
			t.Errorf("glyph %d: color %v, subpixel %v, %d bytes", g.ID, b.color, b.subpixel, len(b.pix))
		}
		if b := e.glyph(g.Font, g.ID, 1, 0, 0, true); !b.subpixel {
			t.Errorf("glyph %d has no subpixels where they are asked for", g.ID)
		}
	}
}
