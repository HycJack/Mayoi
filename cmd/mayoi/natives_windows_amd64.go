//go:build windows && amd64

package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
)

// ghosttyVT is libghostty-vt for this platform, which the terminal draws
// with. It is built into Mayoi so that Mayoi.exe, copied on its own, still
// has a terminal: a native library that ships beside the executable is the
// one file a user forgets to bring.
//
// The file is the build of libghostty-vt that the MyGo version in go.mod
// binds, taken from its plugin manifest
// (plugins/terminal/mygo-plugin.json); its SHA-256 is checked by a test, so
// a MyGo bump says so rather than crashing the terminal.
//
//go:embed natives/ghostty-vt.dll
var ghosttyVT []byte

// ghosttyName is what the library is called on this platform.
const ghosttyName = "ghostty-vt.dll"

// unpackNative puts the terminal's library where MyGo looks for it, before
// the terminal asks for it. A library beside the executable is left to do
// its job, so that a build which ships one uses the newer of the two, and
// MYGO_GHOSTTY_VT still says where it is when a user says so.
func unpackNative() {
	if len(ghosttyVT) == 0 || os.Getenv("MYGO_GHOSTTY_VT") != "" {
		return
	}
	if exe, err := os.Executable(); err == nil {
		if _, err := os.Stat(filepath.Join(filepath.Dir(exe), ghosttyName)); err == nil {
			return
		}
	}
	path, err := nativePath(ghosttyName, ghosttyVT)
	if err != nil {
		// Without the library the rest of Mayoi works: the terminal says
		// it cannot start, as it did before.
		log.Printf("terminal library: %v", err)
		return
	}
	os.Setenv("MYGO_GHOSTTY_VT", path)
}

// nativePath writes the library to a folder of its own in the user's
// cache, named by its SHA-256, and returns it: two versions of Mayoi never
// write over each other, and a library already there is not written again.
func nativePath(name string, data []byte) (string, error) {
	sum := sha256.Sum256(data)
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	dir = filepath.Join(dir, "mayoi", "natives", hex.EncodeToString(sum[:8]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(data)) {
		return path, nil
	}
	// A half written library would load as a broken one: write it beside
	// the name and rename it into place.
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}
