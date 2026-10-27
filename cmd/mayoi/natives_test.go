//go:build windows && amd64

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"testing"

	"github.com/egoist/mygo/plugins/terminal"
)

// ghosttySHA is the SHA-256 of the libghostty-vt that the MyGo version in
// go.mod binds, as its plugin manifest gives it:
//
//	go env GOMODCACHE → plugins/terminal/mygo-plugin.json →
//	libraries[0].files["windows-amd64"].sha256
//
// A MyGo bump that changes the library changes this hash, and the test
// says so: the terminal draws with a library whose API it was built
// against, and the one beside the executable would otherwise be the
// newer of the two by accident.
const ghosttySHA = "53e8e2dd979702e8f2c48107af19be5a76efc2e88eb7cf1097218e899665da41"

func TestTheBuiltInLibraryIsTheOneMyGoBinds(t *testing.T) {
	if len(ghosttyVT) == 0 {
		t.Fatal("no library is built in: a Mayoi.exe copied on its own has no terminal")
	}
	sum := sha256.Sum256(ghosttyVT)
	if got := hex.EncodeToString(sum[:]); got != ghosttySHA {
		t.Errorf("the library is %s, not the %s MyGo binds", got, ghosttySHA)
	}
	if string(ghosttyVT[:2]) != "MZ" {
		t.Error("the library is not a Windows library")
	}
}

func TestTheLibraryIsWrittenOnceForTheVersion(t *testing.T) {
	t.Setenv("MYGO_GHOSTTY_VT", "")
	unpackNative()
	path := os.Getenv("MYGO_GHOSTTY_VT")
	if path == "" {
		t.Fatal("the library was not written, so the terminal cannot start")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the library it names is not there: %v", err)
	}
	if info.Size() != int64(len(ghosttyVT)) {
		t.Errorf("the file is %d bytes, not %d", info.Size(), len(ghosttyVT))
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != ghosttySHA {
		t.Error("the file written is not the library built in")
	}
	// Written again, it is the same file: a folder named by the hash.
	before := info.ModTime()
	t.Setenv("MYGO_GHOSTTY_VT", "")
	unpackNative()
	if again, err := os.Stat(path); err != nil || !again.ModTime().Equal(before) {
		t.Error("the library was written again, though it was already there")
	}
}

func TestALibraryTheUserNamesIsLeftAlone(t *testing.T) {
	t.Setenv("MYGO_GHOSTTY_VT", `C:\somewhere\ghostty-vt.dll`)
	unpackNative()
	if got := os.Getenv("MYGO_GHOSTTY_VT"); got != `C:\somewhere\ghostty-vt.dll` {
		t.Errorf("the library named became %q", got)
	}
}

// TestTheTerminalLoadsTheLibrary is what the whole unpacking is for: the
// terminal MyGo binds loads the library built into Mayoi, on a machine
// where Mayoi arrived alone.
func TestTheTerminalLoadsTheLibrary(t *testing.T) {
	if os.Getenv("MYGO_GHOSTTY_VT") == "" {
		t.Fatal("no library is named: a Mayoi.exe copied on its own has no terminal")
	}
	if path, err := terminal.LibraryPath(); err != nil {
		t.Fatalf("the terminal cannot find the library: %v", err)
	} else if path != os.Getenv("MYGO_GHOSTTY_VT") {
		t.Errorf("the terminal loads %q", path)
	}
	if err := terminal.Load(); err != nil {
		t.Fatalf("the terminal cannot load the library: %v", err)
	}
}
