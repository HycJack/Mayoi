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
		return nil, err
	}
	k := Keys{}
	if err := json.Unmarshal(data, &k); err != nil {
		return nil, fmt.Errorf("ai: %s: %w", path, err)
	}
	return k, nil
}

// SaveKeys writes the keys to path, and the folders before it, for the
// user alone to read.
func SaveKeys(path string, k Keys) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
