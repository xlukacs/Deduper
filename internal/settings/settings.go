// Package settings persists interactive scan preferences.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const fileName = "settings.json"

// Config contains preferences that apply to interactive scans.
type Config struct {
	IgnoreHiddenFolders bool     `json:"ignore_hidden_folders"`
	ExcludedFolders     []string `json:"excluded_folders"`
	CleanupFolders      []string `json:"cleanup_folders"`
	Workers             int      `json:"workers"`
}

var defaultCleanupFolders = []string{
	"venv",
	".venv",
	"node_modules",
	"__pycache__",
	".pytest_cache",
	".mypy_cache",
}

// DefaultCleanupFolders returns the built-in exact directory names targeted by
// cleanup mode.
func DefaultCleanupFolders() []string {
	return append([]string(nil), defaultCleanupFolders...)
}

// ValidCleanupName reports whether name is a single directory name that cleanup
// mode can match. Callers should trim surrounding whitespace first.
func ValidCleanupName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, `/\\`)
}

// Store manages settings in the platform user configuration directory.
type Store struct {
	path string
}

// New returns the default settings store.
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

// Load returns saved settings, or defaults if no file exists.
func (s *Store) Load() (Config, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{CleanupFolders: DefaultCleanupFolders()}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read settings: %w", err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("decode settings: %w", err)
	}
	// Add the defaults for pre-cleanup settings files. An explicit empty list
	// remains empty so users can turn off every cleanup rule.
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(data, &saved); err != nil {
		return Config{}, fmt.Errorf("decode settings: %w", err)
	}
	if _, exists := saved["cleanup_folders"]; !exists {
		config.CleanupFolders = DefaultCleanupFolders()
	}
	return normalize(config), nil
}

// Save persists settings atomically.
func (s *Store) Save(config Config) error {
	config = normalize(config)
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temporary settings: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary settings: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temporary settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary settings: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace settings: %w", err)
	}
	return nil
}

func normalize(config Config) Config {
	if config.Workers < 0 {
		config.Workers = 0
	}
	seen := make(map[string]struct{}, len(config.ExcludedFolders))
	folders := make([]string, 0, len(config.ExcludedFolders))
	for _, folder := range config.ExcludedFolders {
		folder = strings.TrimSpace(folder)
		if folder == "" {
			continue
		}
		folder = filepath.Clean(filepath.FromSlash(strings.ReplaceAll(folder, "\\", "/")))
		if folder == "." || folder == ".." || filepath.IsAbs(folder) || strings.HasPrefix(folder, ".."+string(filepath.Separator)) {
			continue
		}
		if _, exists := seen[folder]; exists {
			continue
		}
		seen[folder] = struct{}{}
		folders = append(folders, folder)
	}
	config.ExcludedFolders = folders
	if config.CleanupFolders != nil {
		seen = make(map[string]struct{}, len(config.CleanupFolders))
		folders = make([]string, 0, len(config.CleanupFolders))
		for _, folder := range config.CleanupFolders {
			folder = strings.TrimSpace(folder)
			if !ValidCleanupName(folder) {
				continue
			}
			if _, exists := seen[folder]; exists {
				continue
			}
			seen[folder] = struct{}{}
			folders = append(folders, folder)
		}
		config.CleanupFolders = folders
	}
	return config
}
