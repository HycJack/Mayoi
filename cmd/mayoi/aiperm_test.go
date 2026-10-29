package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mayoi/internal/ai"
	"mayoi/internal/settings"
	"mayoi/locales"

	"github.com/egoist/mygo/ui"
)

// TestTheQuestionTakesAFile: a file added to the field goes with the
// question, named in it, and leaves the field once it is asked.
func TestTheQuestionTakesAFile(t *testing.T) {
	hs := newHarness(t, true)
	text := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(text, []byte("# What it says\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sent := hs.fakeStream([]string{"read it"}, nil)
	hs.ownChats()
	hs.a.toggleAI()
	hs.settle()
	hs.a.host.(*fakeHost).chosen = []string{text}
	hs.a.attachFiles()
	hs.settle()
	if len(hs.a.ai.attached) != 1 {
		t.Fatalf("the field holds %d files", len(hs.a.ai.attached))
	}
	hs.tt.Type("explain this")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if len(hs.a.ai.attached) != 0 {
		t.Error("the files stayed in the field after the question")
	}
	runs := *sent
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	if !strings.Contains(runs[0].Prompt, "notes.md") || !strings.Contains(runs[0].Prompt, "What it says") {
		t.Errorf("the prompt does not carry the file: %q", runs[0].Prompt)
	}
	if len(runs[0].Pictures) != 0 {
		t.Errorf("%d pictures came of a file of text", len(runs[0].Pictures))
	}
}

// TestAPictureIsShownToTheModel: a picture is not read as text: it is the
// picture the model is given, and the question says what it is called.
func TestAPictureIsShownToTheModel(t *testing.T) {
	hs := newHarness(t, true)
	png := filepath.Join(t.TempDir(), "shot.png")
	// The bytes of a one-pixel PNG, which is what the service is given.
	one := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 0x0D, 'I', 'H', 'D', 'R',
		0, 0, 0, 1, 0, 0, 0, 1, 8, 2, 0, 0, 0, 0x90, 0x77, 0x53, 0xDE}
	if err := os.WriteFile(png, one, 0o644); err != nil {
		t.Fatal(err)
	}
	sent := hs.fakeStream([]string{"a dot"}, nil)
	hs.ownChats()
	hs.a.toggleAI()
	hs.settle()
	hs.a.host.(*fakeHost).chosen = []string{png}
	hs.a.attachFiles()
	hs.settle()
	hs.tt.Type("what is this?")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	runs := *sent
	if len(runs) != 1 {
		t.Fatalf("%d runs", len(runs))
	}
	if len(runs[0].Pictures) != 1 || runs[0].Pictures[0].MIMEType != "image/png" {
		t.Fatalf("the run carries %+v", runs[0].Pictures)
	}
	if string(runs[0].Pictures[0].Data) != string(one) {
		t.Error("the picture's bytes changed on the way")
	}
	if !strings.Contains(runs[0].Prompt, "shot.png") {
		t.Errorf("the prompt does not name the picture: %q", runs[0].Prompt)
	}
}

// TestTheQuestionShowsWhatItCameWith: the question is kept with the names
// of its files under it, as the conversation is read back.
func TestTheQuestionShowsWhatItCameWith(t *testing.T) {
	hs := newHarness(t, true)
	text := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(text, []byte("# What it says\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hs.fakeStream([]string{"read it"}, nil)
	hs.ownChats()
	hs.a.toggleAI()
	hs.settle()
	hs.a.host.(*fakeHost).chosen = []string{text}
	hs.a.attachFiles()
	hs.settle()
	hs.tt.Type("explain this")
	hs.settle()
	hs.tt.TypeKey(0, ui.KeyEnter, "\r")
	hs.settle()
	if _, ok := hs.tt.Find("notes.md"); !ok {
		t.Error("the question does not show the file it came with")
	}
}

// TestAFileTooBigIsNotAttached: a file beyond what a question carries is
// said so, and left out.
func TestAFileTooBigIsNotAttached(t *testing.T) {
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	if err := os.WriteFile(big, make([]byte, maxText+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readAttachment(big); err == nil {
		t.Fatal("a file of 256 KB and a byte was read")
	}
}

// TestAFolderIsNotAFile: a folder cannot come with a question.
func TestAFolderIsNotAFile(t *testing.T) {
	if _, err := readAttachment(t.TempDir()); err == nil {
		t.Fatal("a folder was read as a file")
	}
}

// TestThePermissionIsChosenFromThePanel: the panel says what the assistant
// may do, and choosing another keeps it for the next window.
func TestThePermissionIsChosenFromThePanel(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AI.Permissions = settings.PermAsk
	hs.a.toggleAI()
	hs.settle()
	// The button of the field names what the assistant may do.
	hs.tt.Click(locales.AiPermTitle())
	hs.settle()
	hs.tt.Click(locales.AiPermWorkspaceWrite())
	hs.settle()
	if got := hs.a.settings.AI.Permission(); got != settings.PermWorkspaceWrite {
		t.Fatalf("the permission is %q", got)
	}
	if hs.a.ai.perms {
		t.Error("the list of permissions stayed open")
	}
}

// TestFullAccessAsksFirst: the one permission that stops the cards for good
// asks whether that is meant, and no until the user says yes.
func TestFullAccessAsksFirst(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AI.Permissions = settings.PermAsk
	hs.a.toggleAI()
	hs.settle()
	hs.tt.Click(locales.AiPermTitle())
	hs.settle()
	hs.tt.Click(locales.AiPermFullAccess())
	hs.settle()
	if hs.a.settings.AI.Permission() != settings.PermAsk {
		t.Fatal("full access was given before it was allowed")
	}
	if !hs.a.ai.shown || hs.a.ai.permAsk == "" {
		t.Fatal("the question of full access is not shown")
	}
	hs.tt.Click(locales.AiPermConfirmAllow())
	hs.settle()
	if got := hs.a.settings.AI.Permission(); got != settings.PermFullAccess {
		t.Fatalf("the permission is %q after allowing it", got)
	}
	if hs.a.ai.permAsk != "" {
		t.Error("the question of full access stayed up")
	}
}

// TestCancellingFullAccessKeepsThePermission: no keeps what it was.
func TestCancellingFullAccessKeepsThePermission(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AI.Permissions = settings.PermAsk
	hs.a.toggleAI()
	hs.settle()
	hs.tt.Click(locales.AiPermTitle())
	hs.settle()
	hs.tt.Click(locales.AiPermFullAccess())
	hs.settle()
	hs.tt.Click(locales.ButtonCancel())
	hs.settle()
	if got := hs.a.settings.AI.Permission(); got != settings.PermAsk {
		t.Fatalf("the permission is %q after cancelling", got)
	}
	if hs.a.ai.permAsk != "" {
		t.Error("the question of full access stayed up")
	}
}

// TestAReadOnlyFolderGivesNoWriteTools: the panel's tools follow the
// permission, and a folder only looked at has none that change it.
func TestAReadOnlyFolderGivesNoWriteTools(t *testing.T) {
	hs := newHarness(t, true)
	hs.a.settings.AI.Permissions = settings.PermReadOnly
	for _, tool := range hs.a.aiTools().Sets() {
		switch tool.Name {
		case "write", "edit", "bash":
			t.Errorf("a read-only folder gives the tool %q", tool.Name)
		}
	}
}

// TestAnAllowedProgramRunsWithoutACard: once the user allows a program from
// a card, its commands run by themselves, and the ones that run another
// program still ask.
func TestAnAllowedProgramRunsWithoutACard(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Permissions = settings.PermWorkspaceWrite
	a.settings.AI.Allowed = []string{"go"}
	if !a.askCommand(ai.Change{Command: "go build ./..."}) {
		t.Error("a command of an allowed program asked")
	}
	if !a.askCommand(ai.Change{Command: "cd internal && go test ./..."}) {
		t.Error("a command of an allowed program, after cd, asked")
	}
	// Another program is another decision, and the card is shown: nothing
	// answers it here, so the answer is what the panel says of a card.
	a.settings.AI.Permissions = settings.PermAsk
	if a.trusted("go test && rm -rf out") {
		t.Error("a command that also runs rm is trusted")
	}
	if a.trusted("rm -rf out") {
		t.Error("a command of a program never allowed is trusted")
	}
}

// TestTheCardAllowsTheProgramItRuns: the card before a command trusts the
// program for good, and answers the command it was shown for.
func TestTheCardAllowsTheProgramItRuns(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Permissions = settings.PermAsk
	a.toggleAI()
	card := &aiApproval{change: ai.Change{Command: "go build ./..."}, reply: make(chan bool, 1)}
	a.ai.approval = card
	hs.settle()
	hs.tt.Click(locales.AiAlwaysAllow("go"))
	hs.settle()
	if !a.settings.AI.Allows("go") {
		t.Error("the program was not allowed")
	}
	if a.ai.approval != nil {
		t.Error("the card stayed up")
	}
	select {
	case ok := <-card.reply:
		if !ok {
			t.Error("the command was refused")
		}
	default:
		t.Error("the command was not answered")
	}
}

// TestTheCardOfAWriteAllowsEdits: the card before a write can free the
// writes of this folder, which is the permission that has no card for them.
func TestTheCardOfAWriteAllowsEdits(t *testing.T) {
	hs := newHarness(t, true)
	a := hs.a
	a.settings.AI.Permissions = settings.PermAsk
	a.toggleAI()
	card := &aiApproval{change: ai.Change{Path: "main.go", After: []byte("package main\n")}, reply: make(chan bool, 1)}
	a.ai.approval = card
	hs.settle()
	hs.tt.Click(locales.AiAlwaysAllowEdits())
	hs.settle()
	if got := a.settings.AI.Permission(); got != settings.PermWorkspaceWrite {
		t.Errorf("the permission is %q", got)
	}
	// The next write of the same run does not ask: the permission is read
	// when the card would be shown.
	if !a.askApproval(ai.Change{Path: "other.go", After: []byte("package main\n")}) {
		t.Error("a write asked after edits were allowed")
	}
}
