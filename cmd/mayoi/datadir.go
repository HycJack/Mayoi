package main

import (
	"mayoi/internal/ai"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// dataDirChosen is the folder this run keeps its files in, chosen by
// keepDataDir before anything reads them.
var dataDirChosen string

// chooseDataDir is where Mayoi keeps its own files: the folder the system
// keeps app data in, and behind it the others, in their order, the first
// that takes a write. A machine that refuses the first, as one whose
// security product watches it, keeps Mayoi's files in the next.
func chooseDataDir(system string, others []string) string {
	candidates := make([]string, 0, len(others)+1)
	if system != "" {
		candidates = append(candidates, system)
	}
	candidates = append(candidates, others...)
	for _, dir := range candidates {
		if takesWrites(dir) {
			return dir
		}
	}
	return system
}

// takesWrites reports whether Mayoi can write to dir: it is made if it is
// not there, and a file of Mayoi's own is written there and removed. A
// folder that is there but refuses the file is no better than one that
// cannot be made at all.
//
// The name is plain rather than hidden, as the app writes plain ones, and a
// security product may take the two differently.
func takesWrites(dir string) bool {
	if dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, "mayoi-write-*.tmp")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	return os.Remove(name) == nil
}

// dataDirs are the folders Mayoi may keep its files in instead of the
// system's, the likeliest first: the local folder for app data, which a
// security product that watches the roaming one often leaves alone, the
// data folder beside the executable, and the temp folder last.
func dataDirs() []string {
	var dirs []string
	if local, err := os.UserCacheDir(); err == nil {
		dirs = append(dirs, filepath.Join(local, "Mayoi"))
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "data"))
	}
	if tmp := os.TempDir(); tmp != "" {
		dirs = append(dirs, filepath.Join(tmp, "Mayoi"))
	}
	return dirs
}

// keepDataDir chooses the folder of this run, and applies it to MyGo's
// paths, so that the settings, the keys, the conversations, the icons and
// the crash log are all kept in one place. A folder Mayoi could not write
// to is left for one it can, with the settings and the key of the first
// carried over: a change of place, not a fresh start.
func keepDataDir() {
	system, _ := mygo.App.Path(mygo.PathUserData)
	dir := chooseDataDir(system, dataDirs())
	if dir == "" {
		return
	}
	if dir != system {
		carryOver(dir, system)
	}
	dataDirChosen = dir
	mygo.App.SetPath(mygo.PathUserData, dir)
}

// carryOver copies the settings and the assistant's key from one folder to
// another, each only when the other folder has none of its own.
func carryOver(dir, from string) {
	if from == "" {
		return
	}
	for _, name := range []string{"settings.json", "ai-keys.json"} {
		dst := filepath.Join(dir, name)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			continue
		}
		if name == "ai-keys.json" {
			// A key stays private wherever it is kept.
			_ = ai.WritePrivate(dst, data)
			continue
		}
		_ = os.WriteFile(dst, data, 0o600)
	}
}
