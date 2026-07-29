package scan

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestScanFindsAndSortsDuplicates(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "small-a", "same")
	writeTestFile(t, root, "small-b", "same")
	writeTestFile(t, root, "nested/large-a", strings.Repeat("x", 2048))
	writeTestFile(t, root, "nested/large-b", strings.Repeat("x", 2048))
	writeTestFile(t, root, ".hidden", "unique")
	writeTestFile(t, root, "same-size-different", "xxxx")

	result, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("got %d groups, want 2", len(result.Groups))
	}
	if result.Groups[0].Size != 2048 || result.Groups[0].ReclaimableBytes != 2048 {
		t.Fatalf("unexpected first group: %+v", result.Groups[0])
	}
	if result.Groups[1].Size != 4 || len(result.Groups[1].Files) != 2 {
		t.Fatalf("unexpected second group: %+v", result.Groups[1])
	}
	for _, group := range result.Groups {
		if !sort.SliceIsSorted(group.Files, func(i, j int) bool { return group.Files[i].Path < group.Files[j].Path }) {
			t.Fatalf("paths are not sorted: %+v", group.Files)
		}
	}
	if result.Stats.FilesDiscovered != 6 {
		t.Fatalf("FilesDiscovered = %d, want 6", result.Stats.FilesDiscovered)
	}
	if result.Stats.FilesHashed != 5 {
		t.Fatalf("FilesHashed = %d, want 5", result.Stats.FilesHashed)
	}
}

func TestScanFindsEmptyDuplicates(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a", "")
	writeTestFile(t, root, "b", "")
	result, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || result.Groups[0].Size != 0 || len(result.Groups[0].Files) != 2 {
		t.Fatalf("unexpected groups: %+v", result.Groups)
	}
}

func TestScanIgnoresVenvAndNodeModules(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "keep-a", "same")
	writeTestFile(t, root, "keep-b", "same")
	writeTestFile(t, root, filepath.Join("venv", "ignored-a"), "same")
	writeTestFile(t, root, filepath.Join("node_modules", "pkg", "ignored-b"), "same")

	result, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.FilesDiscovered != 2 || result.Stats.FilesHashed != 2 {
		t.Fatalf("ignored files were counted: stats=%+v", result.Stats)
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Files) != 2 {
		t.Fatalf("ignored files affected duplicate groups: %+v", result.Groups)
	}
	for _, file := range result.Groups[0].Files {
		if strings.Contains(file.Path, "venv") || strings.Contains(file.Path, "node_modules") {
			t.Fatalf("ignored file was included in duplicates: %s", file.Path)
		}
	}
}

func TestScanIgnoresConfiguredAndHiddenFolders(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "keep-a", "same")
	writeTestFile(t, root, "keep-b", "same")
	writeTestFile(t, root, filepath.Join(".git", "objects", "ignored"), "same")
	writeTestFile(t, root, filepath.Join(".turbo", "cache", "ignored"), "same")
	writeTestFile(t, root, filepath.Join("generated", "ignored"), "same")
	writeTestFile(t, root, filepath.Join("nested", "archive", "ignored"), "same")
	writeTestFile(t, root, filepath.Join("nested", "keep", "included"), "same")
	result, err := ScanWithOptions(context.Background(), root, nil, Options{
		Workers:             3,
		IgnoreHiddenFolders: true,
		ExcludedFolders:     []string{"generated", "nested/archive"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.FilesDiscovered != 3 || result.Stats.FilesHashed != 3 {
		t.Fatalf("configured exclusions were not applied: stats=%+v", result.Stats)
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Files) != 3 {
		t.Fatalf("unexpected duplicate groups: %+v", result.Groups)
	}
	for _, file := range result.Groups[0].Files {
		if strings.Contains(file.Path, ".git") || strings.Contains(file.Path, ".turbo") || strings.Contains(file.Path, "generated") || strings.Contains(file.Path, "nested/archive") {
			t.Fatalf("excluded file was included: %s", file.Path)
		}
	}
}

func TestDefaultOptionsUseConfiguredWorkers(t *testing.T) {
	t.Setenv("DEDUPER_WORKERS", "7")
	if options := DefaultOptions(); options.Workers != 7 {
		t.Fatalf("Workers = %d, want 7", options.Workers)
	}
	t.Setenv("DEDUPER_WORKERS", "not-a-number")
	if options := DefaultOptions(); options.Workers < 1 {
		t.Fatalf("Workers = %d, want a positive default", options.Workers)
	}
}

func TestScanDoesNotHashUniqueSizedFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "keep-a", "same")
	writeTestFile(t, root, "keep-b", "same")
	unique := filepath.Join(root, "unique-large")
	if err := os.WriteFile(unique, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(unique, 5*1024*1024*1024); err != nil {
		t.Fatal(err)
	}

	result, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Stats.FilesDiscovered != 3 {
		t.Fatalf("FilesDiscovered = %d, want 3", result.Stats.FilesDiscovered)
	}
	if result.Stats.FilesHashed != 2 {
		t.Fatalf("FilesHashed = %d, want 2", result.Stats.FilesHashed)
	}
	if result.Stats.BytesHashed != 8 {
		t.Fatalf("BytesHashed = %d, want 8", result.Stats.BytesHashed)
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Files) != 2 {
		t.Fatalf("unexpected groups: %+v", result.Groups)
	}
}

func TestScanSkipsSymlinksAndCollapsesHardLinks(t *testing.T) {
	root := t.TempDir()
	original := writeTestFile(t, root, "original", "duplicate")
	writeTestFile(t, root, "copy", "duplicate")
	if err := os.Link(original, filepath.Join(root, "hardlink")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("hard links unavailable: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.Symlink(original, filepath.Join(root, "symlink")); err != nil && runtime.GOOS != "windows" {
		t.Fatal(err)
	}
	result, err := Scan(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || len(result.Groups[0].Files) != 2 {
		t.Fatalf("hard link counted as a copy: %+v", result.Groups)
	}
	if result.Stats.FilesSkipped == 0 && runtime.GOOS != "windows" {
		t.Fatal("symlink was not counted as skipped")
	}
}

func TestScanCancellation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 20; i++ {
		writeTestFile(t, root, filepath.Join("files", string(rune('a'+i))), strings.Repeat("z", 1024))
	}
	ctx, cancel := context.WithCancel(context.Background())
	observer := &testObserver{progress: func(progress Progress) {
		if progress.Phase == PhaseDiscovering {
			cancel()
		}
	}}
	_, err := Scan(ctx, root, observer)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v, want context.Canceled", err)
	}
}

func TestScanWarnsWhenFileChangesDuringHash(t *testing.T) {
	root := t.TempDir()
	path := writeTestFile(t, root, "changing", strings.Repeat("a", 3*1024*1024))
	writeTestFile(t, root, "same-size-candidate", strings.Repeat("b", 3*1024*1024))
	var once sync.Once
	observer := &testObserver{progress: func(progress Progress) {
		if progress.Phase == PhaseHashing && progress.BytesCompleted > 0 {
			once.Do(func() {
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
				if err != nil {
					t.Error(err)
					return
				}
				_, err = f.WriteString("changed")
				if closeErr := f.Close(); err == nil {
					err = closeErr
				}
				if err != nil {
					t.Error(err)
				}
			})
		}
	}}
	result, err := Scan(context.Background(), root, observer)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 0 || len(result.Warnings) != 1 {
		t.Fatalf("unstable file was retained: stats=%+v warnings=%v", result.Stats, result.Warnings)
	}
}

func TestScanRejectsInvalidRoots(t *testing.T) {
	file := writeTestFile(t, t.TempDir(), "file", "data")
	if _, err := Scan(context.Background(), file, nil); err == nil {
		t.Fatal("expected non-directory error")
	}
	if _, err := Scan(context.Background(), filepath.Join(t.TempDir(), "missing"), nil); err == nil {
		t.Fatal("expected missing-root error")
	}
}

func TestFindDuplicatesHashOrderTieBreak(t *testing.T) {
	first := sha256.Sum256([]byte("first"))
	second := sha256.Sum256([]byte("second"))
	files := []FileRecord{
		{Path: "a", Size: 1, Hash: second}, {Path: "b", Size: 1, Hash: second},
		{Path: "c", Size: 1, Hash: first}, {Path: "d", Size: 1, Hash: first},
	}
	groups := FindDuplicates(files)
	if len(groups) != 2 {
		t.Fatalf("got %d groups", len(groups))
	}
	if string(groups[0].Hash[:]) > string(groups[1].Hash[:]) {
		t.Fatal("equal-sized groups not sorted by hash")
	}
}

type testObserver struct {
	progress func(Progress)
}

func (o *testObserver) OnProgress(progress Progress) {
	if o.progress != nil {
		o.progress(progress)
	}
}
func (*testObserver) OnWarning(Warning) {}

func writeTestFile(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
