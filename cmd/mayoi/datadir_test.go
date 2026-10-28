package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A folder Mayoi can write to is taken, and one it cannot is passed over:
// the machine's security product may refuse the system's folder for app
// data, and the folder beside the executable is the next place to try.
func TestAFolderThatRefusesWritesIsPassedOver(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "system")
	if !takesWrites(good) {
		t.Fatalf("a folder of the test's own refuses writes: %s", good)
	}
	// A file where a folder would be: no write can make it a folder.
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a folder"), 0o600); err != nil {
		t.Fatal(err)
	}
	if takesWrites(blocked) {
		t.Errorf("%s takes writes, which is no folder", blocked)
	}
	if got := chooseDataDir(blocked, []string{good}); got != good {
		t.Errorf("the folder chosen is %q, not %q", got, good)
	}
	if got := chooseDataDir(blocked, []string{blocked}); got != blocked {
		t.Errorf("with nowhere else, %q is kept rather than nothing: %q", blocked, got)
	}
	if got := chooseDataDir("", []string{good}); got != good {
		t.Errorf("with no system folder, the one beside the executable is %q", got)
	}
}

// The settings and the key of a folder Mayoi wrote before are carried to
// the one it writes now, each left alone where the new folder has one.
func TestTheSettingsAreCarriedToTheFolderBesideTheExecutable(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "system")
	to := filepath.Join(root, "beside")
	if err := os.MkdirAll(from, 0o700); err != nil {
		t.Fatal(err)
	}
	settings := `{"fontSize": 15, "ai": {"protocol": "openai", "model": "m-1"}}`
	for name, body := range map[string]string{"settings.json": settings, "ai-keys.json": `{"openai": "sk-1"}`} {
		if err := os.WriteFile(filepath.Join(from, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		t.Fatal(err)
	}
	carryOver(to, from)
	for _, name := range []string{"settings.json", "ai-keys.json"} {
		if _, err := os.Stat(filepath.Join(to, name)); err != nil {
			t.Errorf("%s was not carried over: %v", name, err)
		}
	}
	// A file of the new folder's own stays as it is.
	if err := os.WriteFile(filepath.Join(to, "settings.json"), []byte(`{"fontSize": 9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	carryOver(to, from)
	body, err := os.ReadFile(filepath.Join(to, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"fontSize": 9}` {
		t.Errorf("the settings of the folder itself were overwritten: %s", body)
	}
	// The key is private wherever it is kept. Windows keeps no read bit of
	// its own for a file, so there is nothing to read there.
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(filepath.Join(to, "ai-keys.json")); err == nil && info.Mode().Perm()&0o077 != 0 {
			t.Errorf("the key is readable by others: %v", info.Mode().Perm())
		}
	}
}

// The key of a folder that refuses writes is not lost with the folder: the
// app reads it back from the one beside the executable.
func TestTheKeyComesBackInTheOtherFolder(t *testing.T) {
	root := t.TempDir()
	from := filepath.Join(root, "system")
	to := filepath.Join(root, "beside")
	for _, dir := range []string{from, to} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(from, "ai-keys.json"), []byte(`{"openai": "sk-kept"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	carryOver(to, from)
	body, err := os.ReadFile(filepath.Join(to, "ai-keys.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"openai": "sk-kept"}` {
		t.Errorf("the key carried over is %s", body)
	}
}
