// Package history persists a small most-recently-used list of scan roots.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxEntries = 10
	fileName   = "history.json"
)

type storeFile struct {
	Paths []string `json:"paths"`
}

// Store manages recent roots in the platform user configuration directory.
type Store struct {
	path string
}

// New returns the default history store.
func New() (*Store, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("find user config directory: %w", err)
	}
	return &Store{path: filepath.Join(configDir, "deduper", fileName)}, nil
}

// NewAt creates a store at an explicit path, primarily for tests.
func NewAt(path string) *Store {
	return &Store{path: path}
}

// Load returns most-recent-first paths. A missing file is an empty history.
func (s *Store) Load() ([]string, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}
	var stored storeFile
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}
	return normalize(stored.Paths), nil
}

// Add moves path to the front of history and persists it atomically.
func (s *Store) Add(path string) ([]string, error) {
	paths, err := s.Load()
	if err != nil {
		// Corrupt history should not prevent replacing it with valid state.
		paths = nil
	}
	paths = normalize(append([]string{filepath.Clean(path)}, paths...))
	if err := s.save(paths); err != nil {
		return paths, err
	}
	return paths, nil
}

func (s *Store) save(paths []string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create history directory: %w", err)
	}
	data, err := json.MarshalIndent(storeFile{Paths: paths}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".history-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary history: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temporary history: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary history: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary history: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary history: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace history: %w", err)
	}
	return nil
}

func normalize(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, min(len(paths), maxEntries))
	for _, path := range paths {
		if path == "" {
			continue
		}
		clean := filepath.Clean(path)
		key := clean
		if filepath.Separator == '\\' {
			// Windows paths are generally case-insensitive, but preserve display case.
			key = strings.ToLower(filepath.Clean(filepath.ToSlash(clean)))
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, clean)
		if len(out) == maxEntries {
			break
		}
	}
	return out
}
