package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/scan"
	"github.com/xlukacs/Deduper/internal/settings"
)

func cleanupModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app/node_modules"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := New()
	m.history = nil
	m.settingsStore = settings.NewAt(filepath.Join(t.TempDir(), "settings.json"))
	m.settings = settings.Config{CleanupFolders: []string{"node_modules", "venv"}}
	m.screen = screenCleanupResults
	m.cleanupResult = scan.CleanupResult{
		Root:    root,
		Names:   []string{"node_modules", "venv"},
		Folders: []scan.CleanupFolder{{Name: "node_modules", Path: filepath.Join(root, "app/node_modules")}},
	}
	return m
}

func cleanupKey(m Model, key tea.Key) (Model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyPressMsg(key))
	return updated.(Model), cmd
}

func TestCleanupDeleteRequiresY(t *testing.T) {
	m := cleanupModel(t)
	m, _ = cleanupKey(m, tea.Key{Code: 'd'})
	m, _ = cleanupKey(m, tea.Key{Code: tea.KeyEnter})
	if !m.cleanupConfirm || m.cleanupBusy {
		t.Fatal("enter must not confirm cleanup deletion")
	}
	if _, err := os.Stat(m.cleanupResult.Folders[0].Path); err != nil {
		t.Fatal(err)
	}
	m, _ = cleanupKey(m, tea.Key{Code: 'n'})
	if m.cleanupConfirm {
		t.Fatal("n should cancel the cleanup prompt")
	}
}

func TestCleanupRuleChangeDiscardsStaleMatches(t *testing.T) {
	m := cleanupModel(t)
	m, _ = cleanupKey(m, tea.Key{Code: 'm'})
	m, _ = cleanupKey(m, tea.Key{Code: tea.KeyDown})
	m, _ = cleanupKey(m, tea.Key{Code: 'd'})
	if strings.Join(m.settings.CleanupFolders, ",") != "venv" {
		t.Fatalf("folder name was not removed: %v", m.settings.CleanupFolders)
	}
	m, _ = cleanupKey(m, tea.Key{Code: tea.KeyEscape})
	if m.cancel != nil {
		m.cancel()
	}
	if m.screen != screenCleanupScanning || len(m.cleanupResult.Folders) != 0 {
		t.Fatalf("stale matches stayed deletable: screen=%v folders=%v", m.screen, m.cleanupResult.Folders)
	}
}

func TestCleanupResultsDropIdleSpinnerTicks(t *testing.T) {
	m := cleanupModel(t)
	if _, cmd := m.Update(m.spinner.Tick()); cmd != nil {
		t.Fatal("idle cleanup results should stop the spinner tick loop")
	}
}

func TestCleanupDeletionNoticeCountsFailedFolders(t *testing.T) {
	m := cleanupModel(t)
	root := m.cleanupResult.Root
	m.cleanupResult.Folders = []scan.CleanupFolder{
		{Name: "node_modules", Path: filepath.Join(root, "a/node_modules")},
		{Name: "node_modules", Path: filepath.Join(root, "b/node_modules")},
		{Name: "node_modules", Path: filepath.Join(root, "c/node_modules")},
	}
	m.cleanupBusy = true
	m.scanID = 5
	warnings := make([]scan.Warning, 41)
	for i := range warnings {
		warnings[i] = scan.Warning{Path: root, Err: errors.New("denied")}
	}
	updated, _ := m.Update(cleanupDeletedMsg{id: 5, removed: []string{m.cleanupResult.Folders[0].Path}, warnings: warnings})
	m = updated.(Model)
	if !strings.Contains(m.cleanupNotice, "Deleted 1 folders; 2 could not be removed") {
		t.Fatalf("unexpected notice: %q", m.cleanupNotice)
	}
	if len(m.cleanupResult.Folders) != 2 {
		t.Fatalf("remaining folders = %d, want 2", len(m.cleanupResult.Folders))
	}
}
