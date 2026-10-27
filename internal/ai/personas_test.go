package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTheFivePersonasAreThere(t *testing.T) {
	ps := Personas(nil)
	ids := make([]string, 0, len(ps))
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	want := "coder architect reviewer security designer"
	if got := strings.Join(ids, " "); got != want {
		t.Errorf("the personas are %q", got)
	}
	for _, p := range ps {
		if p.Name == "" || p.Description == "" || p.Instructions == "" || !p.BuiltIn {
			t.Errorf("%s is %+v", p.ID, p)
		}
	}
}

func TestACustomPersonaComesAfterTheFive(t *testing.T) {
	custom := []Persona{{ID: "mine", Name: "Mine", Description: "A test", Instructions: "Be terse."}}
	ps := Personas(custom)
	if len(ps) != 6 {
		t.Fatalf("there are %d personas", len(ps))
	}
	if last := ps[5]; last.ID != "mine" || last.BuiltIn {
		t.Errorf("the last is %+v", last)
	}
}

func TestAPersonaOfAChatIsItsInstructions(t *testing.T) {
	p := PersonaByID(Personas(nil), "reviewer")
	if p == nil || p.ID != "reviewer" {
		t.Fatalf("the reviewer is %+v", p)
	}
	sys := SystemFor(*p, "", "", "")
	if !strings.Contains(sys, "meticulous code reviewer") {
		t.Errorf("the system prompt is %q", sys)
	}
	// The base prompt goes, so the persona is not one voice among three.
	if strings.Contains(sys, "helping with the code of the folder") {
		t.Errorf("the system prompt is %q", sys)
	}
}

func TestAnUnknownPersonaIsTheBasePrompt(t *testing.T) {
	if got := SystemFor(Persona{}, "the base", "", ""); got != "the base" {
		t.Errorf("the system prompt is %q", got)
	}
}

func TestTheProjectMemoryIsInThePrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "MAYOI.md"), []byte("Use tabs.\nBe brief.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Memory(dir)
	if !strings.Contains(got, "Use tabs.") {
		t.Errorf("the memory is %q", got)
	}
	if Memory(t.TempDir()) != "" {
		t.Error("a folder without MAYOI.md has a memory")
	}
}

func TestALongProjectMemoryIsCutShort(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", maxMemory+4000)
	if err := os.WriteFile(filepath.Join(dir, "MAYOI.md"), []byte(long), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := len(Memory(dir)); got > maxMemory+len("…") {
		t.Errorf("the memory is %d bytes", got)
	}
}

func TestACutProjectMemoryIsStillText(t *testing.T) {
	dir := t.TempDir()
	// Three bytes a character, and no newline near the cap: a cut by
	// bytes alone lands inside one.
	if err := os.WriteFile(filepath.Join(dir, "MAYOI.md"), []byte(strings.Repeat("項", maxMemory)), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Memory(dir)
	if !utf8.ValidString(got) {
		t.Errorf("the memory is not text: %q…", got[maxMemory-8:])
	}
	if len(got) > maxMemory {
		t.Errorf("the memory is %d bytes", len(got))
	}
}

func TestTheEnvironmentSaysWhereTheUserIs(t *testing.T) {
	got := Env("/code/mayoi", "/code/mayoi/main.go")
	if !strings.Contains(got, "/code/mayoi") || !strings.Contains(got, "main.go") {
		t.Errorf("the environment is %q", got)
	}
	if !strings.HasPrefix(got, "<env>") || !strings.HasSuffix(got, "</env>") {
		t.Errorf("the environment is %q", got)
	}
	if strings.Contains(Env("/code/mayoi", ""), "file:") {
		t.Errorf("an empty file is named: %q", Env("/code/mayoi", ""))
	}
}
