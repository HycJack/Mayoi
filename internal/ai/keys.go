// Package ai is Mayoi's AI assistant: the models it talks to, the keys
// it talks to them with, and the chat it keeps with them.
package ai

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// Keys holds the API key of each provider, by the provider's name.
type Keys map[string]string

// LoadKeys reads the keys at path. A file that is not there has none.
func LoadKeys(path string) (Keys, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Keys{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	k := Keys{}
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return k, nil
}

// SaveKeys writes the keys to path, and the folders before it, for the
// user alone to read.
func SaveKeys(path string, k Keys) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return WritePrivate(path, append(data, '\n'))
}

// WritePrivate writes data to path for the user alone to read. It writes a
// file of its own beside path and moves it over, so that a reader never sees
// half of it, and a file another window has open is replaced rather than
// written through.
//
// A file that cannot be replaced where it stands is written where it is:
// Windows refuses a move over a file that another program holds open, and a
// key that does not save is worse than one written the plain way.
func WritePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp, 0o600)
	}
	if err == nil {
		err = replace(tmp, path)
	}
	if err != nil {
		os.Remove(tmp)
	}
	if err == nil {
		return nil
	}
	// The move failed: write the file where it is, which needs no name of
	// its own. A file marked read-only is made writable first, which is
	// what a copy from another machine, or an archive, leaves behind.
	if ferr := os.WriteFile(path, data, 0o600); ferr != nil {
		if errors.Is(ferr, fs.ErrPermission) && os.Chmod(path, 0o600) == nil {
			ferr = os.WriteFile(path, data, 0o600)
		}
		if ferr != nil {
			return fmt.Errorf("%s: %w", path, ferr)
		}
	}
	return os.Chmod(path, 0o600)
}

// replace moves tmp over path. Windows refuses the move while another
// program holds the file open, as a check of what was just written, a
// backup or a search index does, and refuses it while the file is marked
// read-only: the mark is cleared, and the moment waited out.
func replace(tmp, path string) error {
	tries := 1
	if runtime.GOOS == "windows" {
		tries = 8
	}
	var err error
	for i := range tries {
		if i > 0 {
			os.Chmod(path, 0o600)
			time.Sleep(time.Duration(5+i*10) * time.Millisecond)
		}
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		if runtime.GOOS != "windows" || !errors.Is(err, fs.ErrPermission) {
			break
		}
	}
	return err
}
