package scan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func deletionFixture(t *testing.T, names ...string) (string, []DuplicateGroup) {
	t.Helper()
	root := t.TempDir()
	for _, name := range names {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := ScanWithOptions(context.Background(), root, nil, Options{Workers: 2})
	if err != nil || len(result.Groups) != 1 {
		t.Fatalf("fixture scan: groups=%d err=%v", len(result.Groups), err)
	}
	return root, result.Groups
}

func TestDeleteDuplicatesOnlyRemovesSelectedFiles(t *testing.T) {
	root, groups := deletionFixture(t, "alpha", "beta", "gamma")
	target := filepath.Join(root, "beta")
	removed, warnings, err := DeleteDuplicates(context.Background(), root, groups, map[string]bool{target: true})
	if err != nil || len(warnings) != 0 || len(removed) != 1 || removed[0] != target {
		t.Fatalf("deletion: removed=%v warnings=%v err=%v", removed, warnings, err)
	}
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("flagged file still exists: %v", err)
	}
	for _, name := range []string{"alpha", "gamma"} {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(content) != "same" {
			t.Fatalf("remaining copy %s: %q, %v", name, content, err)
		}
	}
}

func TestDeleteDuplicatesRequiresSurvivingCopy(t *testing.T) {
	root, groups := deletionFixture(t, "alpha", "beta")
	selected := map[string]bool{}
	for _, file := range groups[0].Files {
		selected[file.Path] = true
	}
	removed, warnings, err := DeleteDuplicates(context.Background(), root, groups, selected)
	if err != nil || len(removed) != 0 || len(warnings) != 2 {
		t.Fatalf("all copies flagged: removed=%v warnings=%v err=%v", removed, warnings, err)
	}
	for path := range selected {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("copy was removed: %v", err)
		}
	}
}

func TestDeleteDuplicatesRejectsChangedAndUnsafeFiles(t *testing.T) {
	for _, scenario := range []string{"target changed", "keeper changed", "target replaced", "target symlink", "keeper missing", "outside root", "symlink parent"} {
		t.Run(scenario, func(t *testing.T) {
			root, groups := deletionFixture(t, "alpha", "nested/beta")
			keeper, target := groups[0].Files[0], groups[0].Files[1]
			selectedPath := target.Path
			switch scenario {
			case "target changed", "keeper changed":
				file := target
				if scenario == "keeper changed" {
					file = keeper
				}
				if err := os.WriteFile(file.Path, []byte("diff"), 0600); err != nil {
					t.Fatal(err)
				}
				// Matching size and timestamp must not hide changed contents.
				if err := os.Chtimes(file.Path, file.Info.ModTime(), file.Info.ModTime()); err != nil {
					t.Fatal(err)
				}
			case "target replaced":
				if err := os.Rename(target.Path, target.Path+".original"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target.Path, []byte("same"), 0600); err != nil {
					t.Fatal(err)
				}
			case "target symlink":
				if err := os.Remove(target.Path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(keeper.Path, target.Path); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "keeper missing":
				if err := os.Remove(keeper.Path); err != nil {
					t.Fatal(err)
				}
			case "outside root":
				outside := filepath.Join(t.TempDir(), "beta")
				if err := os.WriteFile(outside, []byte("same"), 0600); err != nil {
					t.Fatal(err)
				}
				file, _, err := hashFile(context.Background(), outside, func(int64) error { return nil })
				if err != nil {
					t.Fatal(err)
				}
				groups[0].Files[1] = file
				selectedPath = outside
			case "symlink parent":
				outside := filepath.Join(t.TempDir(), "moved")
				if err := os.Rename(filepath.Dir(target.Path), outside); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, filepath.Dir(target.Path)); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			}
			removed, warnings, err := DeleteDuplicates(context.Background(), root, groups, map[string]bool{selectedPath: true})
			if err != nil || len(removed) != 0 || len(warnings) != 1 {
				t.Fatalf("unsafe deletion: removed=%v warnings=%v err=%v", removed, warnings, err)
			}
			if _, err := os.Lstat(selectedPath); err != nil {
				t.Fatalf("unsafe target was removed: %v", err)
			}
		})
	}
}

func TestDeleteDuplicatesPartialFailureAndCancellation(t *testing.T) {
	root, groups := deletionFixture(t, "alpha", "beta", "gamma")
	beta, gamma := groups[0].Files[1], groups[0].Files[2]
	if err := os.WriteFile(beta.Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	selected := map[string]bool{beta.Path: true, gamma.Path: true}
	removed, warnings, err := DeleteDuplicates(context.Background(), root, groups, selected)
	if err != nil || len(removed) != 1 || removed[0] != gamma.Path || len(warnings) != 1 || warnings[0].Path != beta.Path {
		t.Fatalf("partial deletion: removed=%v warnings=%v err=%v", removed, warnings, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	removed, _, err = DeleteDuplicates(ctx, root, groups, map[string]bool{beta.Path: true})
	if !errors.Is(err, context.Canceled) || len(removed) != 0 {
		t.Fatalf("cancelled deletion: removed=%v err=%v", removed, err)
	}
}

func TestDeleteDuplicatesRejectsUnknownSelection(t *testing.T) {
	root, groups := deletionFixture(t, "alpha", "beta")
	unknown := filepath.Join(root, "unknown")
	if err := os.WriteFile(unknown, []byte("same"), 0600); err != nil {
		t.Fatal(err)
	}
	removed, warnings, err := DeleteDuplicates(context.Background(), root, groups, map[string]bool{unknown: true})
	if err != nil || len(removed) != 0 || len(warnings) != 1 || warnings[0].Path != unknown {
		t.Fatalf("unknown selection: removed=%v warnings=%v err=%v", removed, warnings, err)
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteDuplicatesUsesAnotherUnchangedKeeper(t *testing.T) {
	root, groups := deletionFixture(t, "alpha", "beta", "gamma")
	if err := os.WriteFile(groups[0].Files[0].Path, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	target := groups[0].Files[2].Path
	removed, warnings, err := DeleteDuplicates(context.Background(), root, groups, map[string]bool{target: true})
	if err != nil || len(warnings) != 0 || len(removed) != 1 || removed[0] != target {
		t.Fatalf("alternate keeper: removed=%v warnings=%v err=%v", removed, warnings, err)
	}
	content, err := os.ReadFile(groups[0].Files[1].Path)
	if err != nil || string(content) != "same" {
		t.Fatalf("unchanged copy did not survive: %q %v", content, err)
	}
}
