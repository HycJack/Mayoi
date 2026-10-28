package main

import (
	"strings"
	"testing"

	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// TestAnAnswerIsShownAsMarkdown: the assistant answers in Markdown, and the
// panel draws it as Markdown: no hashes, asterisks or dashes of its syntax
// in sight, its words in their places.
func TestAnAnswerIsShownAsMarkdown(t *testing.T) {
	hs := newHarness(t, true)
	hs.fakeStream([]string{
		"## Steps\n",
		"\nRun **go build** and then\n",
		"\n- first\n- second\n",
		"\n```go\nfmt.Println(1)\n```\n",
	}, nil)
	hs.ask("how do I build it")
	hs.settle()
	for _, mark := range []string{"## ", "**", "- first", "```"} {
		if hs.tt.HasText(mark) {
			t.Errorf("the Markdown's %q is shown as it was written: %q", mark, hs.tt.Texts())
		}
	}
	for _, want := range []string{"Steps", "go build", "first", "second", "fmt.Println(1)"} {
		if !hs.tt.HasText(want) {
			t.Errorf("the answer's %q is not shown: %q", want, hs.tt.Texts())
		}
	}
	// The answer itself keeps its Markdown, which is what is copied and
	// what the model is told it said.
	if got := hs.a.ai.msgs[len(hs.a.ai.msgs)-1].text; !strings.HasPrefix(got, "## Steps") {
		t.Errorf("the answer is kept as %q", got)
	}
}

// TestAnAnswerIsCopiedWithOneClick: the answer's copy button puts its
// Markdown on the clipboard, without dragging over it.
func TestAnAnswerIsCopiedWithOneClick(t *testing.T) {
	hs := newHarness(t, true)
	const answer = "# Title\n\nBody with `code`."
	hs.fakeStream([]string{answer}, nil)
	hs.ask("say something")
	hs.settle()
	if err := hs.tt.Click(locales.AiCopyAnswer()); err != nil {
		t.Fatal(err)
	}
	hs.settle()
	if got := hs.tt.Clipboard(); got != answer {
		t.Errorf("the clipboard holds %q", got)
	}
}

// TestTheAnswerIsDraggedAndCopied: the conversation is text to the engine, as
// the editor's is: a drag chooses part of an answer and the copy key takes it.
func TestTheAnswerIsDraggedAndCopied(t *testing.T) {
	hs := newHarness(t, true)
	hs.fakeStream([]string{"Hello there."}, nil)
	hs.ask("hi")
	hs.settle()
	r, ok := hs.tt.Find("Hello there.")
	if !ok {
		t.Fatalf("the answer is not one paragraph of text: %q", hs.tt.Texts())
	}
	y := r.Y + r.H/2
	hs.tt.Press(r.X+1, y)
	hs.tt.Move(r.X+r.W-1, y)
	hs.tt.Release(r.X+r.W-1, y)
	hs.settle()
	hs.key(ui.Cmd, ui.KeyC)
	hs.settle()
	if got := hs.tt.Clipboard(); !strings.Contains(got, "Hello") {
		t.Errorf("the drag copied %q", got)
	}
}

// TestAPictureInAnAnswerShowsItsDescription: a picture of the web is not
// fetched into a column of prose; its description stands in for it.
func TestAPictureInAnAnswerShowsItsDescription(t *testing.T) {
	got := answerSource("Look: ![a chart](https://example.com/chart.png) at once.")
	if want := "Look: a chart at once."; got != want {
		t.Errorf("the text parsed is %q, want %q", got, want)
	}
}
