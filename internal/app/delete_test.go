package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/scan"
)

func resultKey(m Model, code rune) (Model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: code}))
	return updated.(Model), cmd
}

func deletionModel() Model {
	m := New()
	m.screen = screenResults
	m.focus = focusDetails
	m.result = resultFixture()
	m.applyFilter()
	return m
}

func TestResultsDeletionMarkingAndNavigation(t *testing.T) {
	m := deletionModel()
	m, cmd := resultKey(m, 'd')
	if cmd != nil || !m.deleteMarked["/tmp/root/alpha"] || m.deleteBusy {
		t.Fatal("d must only mark the selected file")
	}
	view := m.View().Content
	if !strings.Contains(view, "[DELETE] /tmp/root/alpha") || !strings.Contains(view, "1 files flagged") {
		t.Fatalf("missing deletion indicators: %s", view)
	}
	m, _ = resultKey(m, tea.KeyDown)
	m, _ = resultKey(m, 'd')
	if m.detailOffset != 1 || len(m.deleteMarked) != 1 || !strings.Contains(m.deleteNotice, "at least one") {
		t.Fatal("marking the final surviving copy should be refused")
	}
	m, _ = resultKey(m, tea.KeyUp)
	m, _ = resultKey(m, 'd')
	if len(m.deleteMarked) != 0 {
		t.Fatal("second d should unmark the file")
	}
	m.focus = focusGroups
	m, _ = resultKey(m, 'd')
	if len(m.deleteMarked) != 0 {
		t.Fatal("group focus must not mark an unseen file")
	}
	m.focus = focusDetails
	m.showWarnings = true
	m, _ = resultKey(m, 'd')
	if len(m.deleteMarked) != 0 {
		t.Fatal("warning view must not mark a hidden file")
	}
}

func TestResultsMarksSurviveFilterSortAndRequireConfirmation(t *testing.T) {
	m := deletionModel()
	m, _ = resultKey(m, 'd')
	m, _ = resultKey(m, 's')
	m.filterInput.SetValue("no-match")
	m.applyFilter()
	if len(m.groups) != 0 || len(m.deleteMarked) != 1 {
		t.Fatal("filter must retain marks")
	}
	m, cmd := resultKey(m, tea.KeyEnter)
	if !m.deleteConfirm || cmd != nil || m.deleteBusy {
		t.Fatal("enter must review without deleting")
	}
	view := m.View().Content
	if !strings.Contains(view, "/tmp/root/alpha") || !strings.Contains(view, "Permanently delete") {
		t.Fatalf("review must include files hidden by the filter: %s", view)
	}
	m, cmd = resultKey(m, tea.KeyEnter)
	if cmd != nil || !m.deleteConfirm || m.deleteBusy {
		t.Fatal("enter in confirmation must not delete files")
	}
	m, _ = resultKey(m, 'n')
	if m.deleteConfirm || len(m.deleteMarked) != 1 {
		t.Fatal("cancelling confirmation must preserve marks")
	}
	m, _ = resultKey(m, 'u')
	if len(m.deleteMarked) != 0 {
		t.Fatal("u should clear all marks")
	}
}

func TestResultsPendingMarksGuardLeaving(t *testing.T) {
	for _, code := range []rune{'q', 'n', 'r', tea.KeyEscape} {
		t.Run(string(code), func(t *testing.T) {
			m := deletionModel()
			m, _ = resultKey(m, 'd')
			m, cmd := resultKey(m, code)
			if !m.deleteConfirm || m.screen != screenResults || cmd != nil {
				t.Fatal("leaving results should first review pending deletion marks")
			}
			m, _ = resultKey(m, tea.KeyEscape)
			if m.deleteConfirm || m.deleteAfter != "" || len(m.deleteMarked) != 1 || m.screen != screenResults {
				t.Fatal("cancelling should leave results and marks intact")
			}
		})
	}
}

func TestResultsConfirmedDeletionUpdatesActualFilesAndResults(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("same"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result, err := scan.ScanWithOptions(context.Background(), root, nil, scan.Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	m := deletionModel()
	m.result = result
	m.applyFilter()
	m, _ = resultKey(m, 'd')
	m, _ = resultKey(m, tea.KeyEnter)
	target := filepath.Join(root, "alpha")
	if _, err := os.Stat(target); err != nil {
		t.Fatal("file was deleted before confirmation")
	}
	m, cmd := resultKey(m, 'y')
	if !m.deleteBusy || cmd == nil || m.deleteConfirm {
		t.Fatal("confirmation should start deletion")
	}
	// Bubble Tea executes the deletion command alongside the spinner tick.
	batch := cmd().(tea.BatchMsg)
	finished := batch[len(batch)-1]()
	updated, _ := m.Update(finished)
	m = updated.(Model)
	if m.deleteBusy || len(m.deleteMarked) != 0 || len(m.groups) != 1 || len(m.groups[0].Files) != 2 || m.groups[0].ReclaimableBytes != 4 {
		t.Fatalf("unexpected post-deletion state: groups=%v marks=%v busy=%v", m.groups, m.deleteMarked, m.deleteBusy)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("confirmed target remains: %v", err)
	}
	if !strings.Contains(m.deleteNotice, "Deleted 1") {
		t.Fatalf("missing completion notice: %q", m.deleteNotice)
	}
	// Delete one more copy; the remaining original is no longer a duplicate.
	m, _ = resultKey(m, 'd')
	m, _ = resultKey(m, tea.KeyEnter)
	m, cmd = resultKey(m, 'y')
	batch = cmd().(tea.BatchMsg)
	updated, _ = m.Update(batch[len(batch)-1]())
	m = updated.(Model)
	if len(m.result.Groups) != 0 || !strings.Contains(m.View().Content, "No duplicates found") {
		t.Fatal("resolved groups should disappear from results")
	}
	if _, err := os.Stat(filepath.Join(root, "gamma")); err != nil {
		t.Fatal("last copy should survive")
	}
}

func TestResultsDeletionFailuresRetainMarksAndIgnoreStaleMessages(t *testing.T) {
	m := deletionModel()
	m.deleteBusy = true
	m.scanID = 7
	m.deleteMarked = map[string]bool{"/tmp/root/alpha": true}
	m.deleteAfter = keyQuit
	updated, _ := m.Update(duplicatesDeletedMsg{id: 6, removed: []string{"/tmp/root/alpha"}})
	m = updated.(Model)
	if !m.deleteBusy || len(m.deleteMarked) != 1 {
		t.Fatal("stale completion changed current state")
	}
	updated, cmd := m.Update(duplicatesDeletedMsg{id: 7, warnings: []scan.Warning{{Path: "/tmp/root/alpha", Err: errors.New("changed")}}})
	m = updated.(Model)
	if m.deleteBusy || len(m.deleteMarked) != 1 || len(m.deleteWarnings) != 1 || len(m.result.Warnings) != 0 || m.deleteAfter != "" || cmd != nil {
		t.Fatal("failure must preserve marks and stop pending exit")
	}
	if !strings.Contains(m.deleteNotice, "could not be deleted") {
		t.Fatal("failure should explain where warnings can be read")
	}
}

func TestResultsReviewDiscardContinuesPendingAction(t *testing.T) {
	for _, code := range []rune{'q', 'n', tea.KeyEscape} {
		t.Run(string(code), func(t *testing.T) {
			m := deletionModel()
			m, _ = resultKey(m, 'd')
			m, _ = resultKey(m, code)
			m, cmd := resultKey(m, 'u')
			if m.deleteConfirm || m.deleteAfter != "" || len(m.deleteMarked) != 0 || cmd == nil {
				t.Fatal("discarding marks must continue the requested action")
			}
			if code == 'q' {
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Fatal("discarding during quit review should quit")
				}
			} else if m.screen != screenStart {
				t.Fatal("discarding during navigation review should open folder selection")
			}
		})
	}
}

func TestResultsReviewScrollsThroughEveryFlaggedFile(t *testing.T) {
	m := deletionModel()
	m.height = 15
	m.deleteMarked = map[string]bool{"/1": true, "/2": true, "/3": true, "/4": true, "/5": true}
	m, _ = resultKey(m, tea.KeyEnter)
	if view := m.View().Content; !strings.Contains(view, "/1\n/2") || strings.Contains(view, "/5") {
		t.Fatalf("unexpected first review page: %s", view)
	}
	for i := 0; i < 10; i++ {
		m, _ = resultKey(m, tea.KeyDown)
	}
	if view := m.View().Content; m.deleteReviewOffset != 3 || !strings.Contains(view, "/4\n/5") {
		t.Fatalf("review failed to reach final file: offset=%d view=%s", m.deleteReviewOffset, view)
	}
	for i := 0; i < 10; i++ {
		m, _ = resultKey(m, tea.KeyUp)
	}
	if m.deleteReviewOffset != 0 {
		t.Fatal("review scroll must stop at its first file")
	}
}

func TestResultsStopDeletionPreservesRemainingMarks(t *testing.T) {
	m := deletionModel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m.cancel = cancel
	m.deleteBusy = true
	m.deleteAfter = keyQuit
	m.scanID = 7
	m.deleteMarked = map[string]bool{"/tmp/root/alpha": true}
	m, _ = resultKey(m, tea.KeyEscape)
	if !errors.Is(ctx.Err(), context.Canceled) || !m.deleteBusy || m.deleteAfter != "" {
		t.Fatal("escape must request cancellation and await completion")
	}
	updated, cmd := m.Update(duplicatesDeletedMsg{id: 7, err: context.Canceled})
	m = updated.(Model)
	if m.deleteBusy || len(m.deleteMarked) != 1 || cmd != nil || !strings.Contains(m.deleteNotice, "Deletion stopped") {
		t.Fatal("stopped deletion should retain remaining marks without exiting")
	}
}

func TestResultsSuccessfulDeletionContinuesPendingQuit(t *testing.T) {
	m := deletionModel()
	m.deleteBusy = true
	m.deleteAfter = keyQuit
	m.scanID = 7
	m.deleteMarked = map[string]bool{"/tmp/root/alpha": true}
	updated, cmd := m.Update(duplicatesDeletedMsg{id: 7, removed: []string{"/tmp/root/alpha"}})
	m = updated.(Model)
	if m.deleteBusy || len(m.deleteMarked) != 0 || len(m.groups) != 0 || cmd == nil {
		t.Fatal("successful deletion should complete the pending quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("successful deletion did not quit")
	}
}

func TestResultsDeletionKeepsSelectionAndScanErrors(t *testing.T) {
	m := New()
	m.screen = screenResults
	m.groupSort = sortPath
	m.result = scan.Result{Root: "/tmp/root", Groups: []scan.DuplicateGroup{
		sortGroup("/tmp/root/a", 4, 3, time.Unix(0, 0)),
		sortGroup("/tmp/root/b", 4, 3, time.Unix(0, 0)),
		sortGroup("/tmp/root/c", 4, 3, time.Unix(0, 0)),
	}}
	m.applyFilter()
	m.selectedGroup, m.detailOffset = 2, 2
	m.deleteMarked = map[string]bool{"/tmp/root/a-0": true, "/tmp/root/c-0": true}
	m.deleteBusy = true
	m.scanID = 3
	updated, _ := m.Update(duplicatesDeletedMsg{
		id:       3,
		removed:  []string{"/tmp/root/a-0"},
		warnings: []scan.Warning{{Path: "/tmp/root/c-0", Err: errors.New("changed")}},
	})
	m = updated.(Model)
	group := m.groups[m.selectedGroup]
	if group.Files[m.detailOffset].Path != "/tmp/root/c-2" {
		t.Fatalf("selection moved to group %d file %d", m.selectedGroup, m.detailOffset)
	}
	if m.result.Stats.Errors != 0 || len(m.result.Warnings) != 0 || len(m.deleteWarnings) != 1 {
		t.Fatal("deletion problems must be kept apart from scan errors")
	}
	m.showWarnings = true
	if view := m.warningView(); !strings.Contains(view, "Deletion problems (1)") {
		t.Fatalf("deletion problems missing from warnings view:\n%s", view)
	}
}
