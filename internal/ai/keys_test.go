package ai

import (
	"os"
	"path/filepath"
	"runtime"
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
