package app

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/scan"
)

func TestStartRejectsInvalidPath(t *testing.T) {
	m := New()
	m.history = nil
	m.pathInput.SetValue(t.TempDir() + "/missing")
	updated, _ := m.updateStart(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	got := updated.(Model)
	if got.screen != screenStart || !strings.Contains(got.status, "cannot access") {
		t.Fatalf("unexpected state: screen=%v status=%q", got.screen, got.status)
	}
}

func TestScanningProgressAndCompletion(t *testing.T) {
	m := New()
	m.screen = screenScanning
	m.scanID = 7
	m.events = make(chan tea.Msg)
	progress := scan.Progress{Phase: scan.PhaseHashing, FilesCompleted: 2, FilesTotal: 3}
	updated, _ := m.updateScanning(scanProgressMsg{id: 7, progress: progress})
	m = updated.(Model)
	if m.latest.FilesCompleted != 2 {
		t.Fatalf("progress not applied: %+v", m.latest)
	}

	result := resultFixture()
	updated, _ = m.updateScanning(scanFinishedMsg{id: 7, result: result})
	m = updated.(Model)
	if m.screen != screenResults || len(m.groups) != 1 || m.focus != focusGroups {
		t.Fatalf("completion state invalid: %+v", m)
	}
}

func TestScanningFailureReturnsToStart(t *testing.T) {
	m := New()
	m.screen = screenScanning
	m.scanID = 2
	updated, _ := m.updateScanning(scanFinishedMsg{id: 2, err: errors.New("boom")})
	got := updated.(Model)
	if got.screen != screenStart || !strings.Contains(got.status, "boom") {
		t.Fatalf("unexpected failure state: screen=%v status=%q", got.screen, got.status)
	}
}

func TestResultsFilterNavigationAndLayout(t *testing.T) {
	m := New()
	m.screen = screenResults
	m.focus = focusGroups
	m.width, m.height = 80, 30
	m.result = resultFixture()
	m.groups = append([]scan.DuplicateGroup(nil), m.result.Groups...)
	m.filterInput.SetValue("no-match")
	m.applyFilter()
	if len(m.groups) != 0 {
		t.Fatalf("filter retained groups: %+v", m.groups)
	}
	m.filterInput.SetValue("alpha")
	m.applyFilter()
	if len(m.groups) != 1 {
		t.Fatalf("filter omitted group: %+v", m.groups)
	}
	view := m.View()
	if !strings.Contains(view.Content, "Matching files") || !strings.Contains(view.Content, "alpha") {
		t.Fatalf("narrow result view missing content:\n%s", view.Content)
	}
	updated, _ := m.updateResults(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if updated.(Model).focus != focusDetails {
		t.Fatal("tab did not switch result pane")
	}
}

func TestResultsSortModesAndFiltering(t *testing.T) {
	groups := []scan.DuplicateGroup{
		sortGroup("/b/alpha", 100, 4, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
		sortGroup("/c/bravo", 200, 2, time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)),
		sortGroup("/a/charlie", 50, 6, time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)),
	}
	m := New()
	m.screen = screenResults
	m.focus = focusGroups
	m.result.Groups = groups
	m.applyFilter()
	assertGroupOrder(t, m.groups, "/b/alpha-0", "/a/charlie-0", "/c/bravo-0")

	expected := []struct {
		mode  groupSort
		paths []string
	}{
		{sortSize, []string{"/c/bravo-0", "/b/alpha-0", "/a/charlie-0"}},
		{sortCopies, []string{"/a/charlie-0", "/b/alpha-0", "/c/bravo-0"}},
		{sortUpdated, []string{"/a/charlie-0", "/c/bravo-0", "/b/alpha-0"}},
		{sortPath, []string{"/a/charlie-0", "/b/alpha-0", "/c/bravo-0"}},
	}
	for _, test := range expected {
		m.selectedGroup = 2
		updated, _ := m.updateResults(tea.KeyPressMsg(tea.Key{Code: 's'}))
		m = updated.(Model)
		if m.groupSort != test.mode || m.selectedGroup != 0 {
			t.Fatalf("sort key selected mode %v at group %d, want mode %v at group 0", m.groupSort, m.selectedGroup, test.mode)
		}
		assertGroupOrder(t, m.groups, test.paths...)
	}

	// Rebuilding the visible groups for a filter keeps the active ordering.
	m.filterInput.SetValue("")
	m.applyFilter()
	assertGroupOrder(t, m.groups, "/a/charlie-0", "/b/alpha-0", "/c/bravo-0")
	if view := m.View().Content; !strings.Contains(view, "Duplicate groups · path") || !strings.Contains(view, "s sort") {
		t.Fatalf("sort state or shortcut missing from results view:\n%s", view)
	}
}

func TestWarningViewAndWindowResize(t *testing.T) {
	m := New()
	m.screen = screenResults
	m.result = resultFixture()
	m.result.Warnings = []scan.Warning{{Path: "bad", Err: errors.New("denied")}}
	m.showWarnings = true
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(Model)
	if m.width != 120 || !strings.Contains(m.View().Content, "bad: denied") {
		t.Fatalf("resize/warning rendering failed: %s", m.View().Content)
	}
}

func resultFixture() scan.Result {
	hash := sha256.Sum256([]byte("same"))
	return scan.Result{
		Root:       "/tmp/root",
		StartedAt:  time.Unix(0, 0),
		FinishedAt: time.Unix(1, 0),
		Stats:      scan.Stats{FilesHashed: 2, BytesHashed: 8},
		Groups: []scan.DuplicateGroup{{
			Hash: hash, Size: 4, ReclaimableBytes: 4,
			Files: []scan.FileRecord{{Path: "/tmp/root/alpha", Size: 4, Hash: hash}, {Path: "/tmp/root/beta", Size: 4, Hash: hash}},
		}},
	}
}

type fixedFileInfo struct {
	modified time.Time
}

func (info fixedFileInfo) Name() string       { return "file" }
func (info fixedFileInfo) Size() int64        { return 0 }
func (info fixedFileInfo) Mode() fs.FileMode  { return 0 }
func (info fixedFileInfo) ModTime() time.Time { return info.modified }
func (info fixedFileInfo) IsDir() bool        { return false }
func (info fixedFileInfo) Sys() any           { return nil }

func sortGroup(prefix string, size int64, copies int, modified time.Time) scan.DuplicateGroup {
	hash := sha256.Sum256([]byte(prefix))
	files := make([]scan.FileRecord, copies)
	for i := range files {
		files[i] = scan.FileRecord{
			Path: fmt.Sprintf("%s-%d", prefix, i),
			Size: size,
			Hash: hash,
			Info: fixedFileInfo{modified: modified},
		}
	}
	return scan.DuplicateGroup{
		Hash:             hash,
		Size:             size,
		Files:            files,
		ReclaimableBytes: size * int64(copies-1),
	}
}

func assertGroupOrder(t *testing.T, groups []scan.DuplicateGroup, paths ...string) {
	t.Helper()
	if len(groups) != len(paths) {
		t.Fatalf("got %d groups, want %d", len(groups), len(paths))
	}
	for i, path := range paths {
		if got := groupPath(groups[i]); got != path {
			t.Fatalf("group %d path = %q, want %q", i, got, path)
		}
	}
}
