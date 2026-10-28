package ai

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestKeysRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-keys.json")
	if err := SaveKeys(path, Keys{"anthropic": "sk-ant", "openai": "sk-oa"}); err != nil {
		t.Fatal(err)
	}
	k, err := LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if k["anthropic"] != "sk-ant" || k["openai"] != "sk-oa" {
		t.Fatalf("keys are %v", k)
	}
}

func TestKeysOfNoFileAreEmpty(t *testing.T) {
	k, err := LoadKeys(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(k) != 0 {
		t.Fatalf("keys are %v", k)
	}
}

func TestSaveKeysIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows has no permission bits to check: a file's mode there
		// is -rw-rw-rw- whatever it was created with.
		t.Skip("the permission bits say nothing on Windows")
	}
	path := filepath.Join(t.TempDir(), "sub", "ai-keys.json")
	if err := SaveKeys(path, Keys{"x": "y"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("the file is %v", perm)
	}
}

// A key file that another program left read-only, as a copy from another
// machine or an archive does, is saved over all the same: a key that cannot
// be written is a dialog that cannot close.
func TestSaveKeysWritesOverAReadOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ai-keys.json")
	if err := SaveKeys(path, Keys{"openai": "sk-old"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o400); err != nil {
		t.Fatal(err)
	}
	if err := SaveKeys(path, Keys{"openai": "sk-new"}); err != nil {
		t.Fatalf("a read-only file was not saved over: %v", err)
	}
	k, err := LoadKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if k["openai"] != "sk-new" {
		t.Fatalf("the keys are %v", k)
	}
}

// A failure says which file it could not write, which is what the dialog
// shows the user.
func TestAFailedSaveNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	// A folder where the file goes: no key can be written into a name that
	// is taken by a folder.
	path := filepath.Join(dir, "ai-keys.json")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := SaveKeys(path, Keys{"openai": "sk-x"})
	if err == nil {
		t.Fatal("a key was written into a folder")
	}
	if !strings.Contains(err.Error(), "ai-keys.json") {
		t.Errorf("the failure says %q", err)
	}
}
