package history

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMissingHistoryIsEmpty(t *testing.T) {
	store := NewAt(filepath.Join(t.TempDir(), "missing.json"))
	paths, err := store.Load()
	if err != nil || len(paths) != 0 {
		t.Fatalf("Load() = %v, %v", paths, err)
	}
}

func TestAddMaintainsMRUAndLimit(t *testing.T) {
	store := NewAt(filepath.Join(t.TempDir(), "nested", "history.json"))
	for i := 0; i < 12; i++ {
		if _, err := store.Add(filepath.Join("root", string(rune('a'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := store.Add(filepath.Join("root", "f"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 10 || paths[0] != filepath.Join("root", "f") {
		t.Fatalf("unexpected history: %v", paths)
	}
	seen := 0
	for _, path := range paths {
		if path == filepath.Join("root", "f") {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("duplicate path retained: %v", paths)
	}
	loaded, err := store.Load()
	if err != nil || !reflect.DeepEqual(paths, loaded) {
		t.Fatalf("persisted history = %v, %v", loaded, err)
	}
}

func TestCorruptHistoryCanBeReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.json")
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := NewAt(path)
	if _, err := store.Load(); err == nil {
		t.Fatal("expected decode error")
	}
	paths, err := store.Add("valid")
	if err != nil || len(paths) != 1 {
		t.Fatalf("Add() = %v, %v", paths, err)
	}
}
