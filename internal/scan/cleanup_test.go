package scan

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func makeDirs(t *testing.T, root string, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFindCleanupFoldersHonorsExclusionsAndHiddenFolders(t *testing.T) {
	root := t.TempDir()
	makeDirs(t, root,
		"app/node_modules/dep/node_modules",
		"backups/old/node_modules",
		"proj/keep/node_modules",
		"proj/.venv/lib",
		".config/tool/node_modules",
	)
	options := Options{IgnoreHiddenFolders: true, ExcludedFolders: []string{"backups", "proj/keep"}}
	result, err := FindCleanupFolders(context.Background(), root, []string{"node_modules", ".venv", "", "a/b"}, options, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(root, "app/node_modules"), filepath.Join(root, "proj/.venv")}
	if len(result.Folders) != len(want) {
		t.Fatalf("found %v, want %v", result.Folders, want)
	}
	for i, folder := range result.Folders {
		if folder.Path != want[i] {
			t.Fatalf("found %v, want %v", result.Folders, want)
		}
	}
	if len(result.Names) != 2 {
		t.Fatalf("invalid names were not dropped: %v", result.Names)
	}
}

func TestDeleteCleanupFoldersReportsOnlyFullyRemovedFolders(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission checks do not apply to root")
	}
	root := t.TempDir()
	makeDirs(t, root, "a/node_modules/x", "b/node_modules/locked")
	for i := range 3 {
		if err := os.WriteFile(filepath.Join(root, "b/node_modules/locked", string(rune('a'+i))), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	locked := filepath.Join(root, "b/node_modules/locked")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })

	folders := []CleanupFolder{
		{Name: "node_modules", Path: filepath.Join(root, "a/node_modules")},
		{Name: "node_modules", Path: filepath.Join(root, "b/node_modules")},
	}
	removed, warnings, err := DeleteCleanupFolders(context.Background(), root, folders, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != folders[0].Path {
		t.Fatalf("removed %v, want only %s", removed, folders[0].Path)
	}
	if len(warnings) <= 1 {
		t.Fatalf("expected per-entry warnings for the locked folder, got %v", warnings)
	}
}
