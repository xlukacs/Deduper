package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/scan"
	"github.com/xlukacs/Deduper/internal/settings"
)

type cleanupFinishedMsg struct {
	id     int64
	result scan.CleanupResult
	err    error
}

type cleanupDeletedMsg struct {
	id       int64
	removed  []string
	warnings []scan.Warning
	err      error
}

type cleanupDeleteProgressMsg struct {
	id       int64
	progress scan.CleanupDeleteProgress
}

func (m Model) launchCleanupScan(root string) (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 128)
	m.scanID++
	id := m.scanID
	m.cancel = cancel
	m.events = events
	m.screen = screenCleanupScanning
	m.started = time.Now()
	m.latest = scan.Progress{Phase: scan.PhaseDiscovering, CurrentPath: root}
	m.scanWarnings = 0
	m.cleanupResult = scan.CleanupResult{}
	m.cleanupSelected = 0
	m.cleanupConfirm = false
	m.cleanupBusy = false
	m.cleanupCancelRequested = false
	m.cleanupDeleteProgress = scan.CleanupDeleteProgress{}
	m.cleanupNotice = ""
	m.showWarnings = false

	names := m.cleanupFolderNames()
	go func() {
		result, err := scan.FindCleanupFolders(ctx, root, names, channelObserver{id: id, events: events})
		select {
		case events <- cleanupFinishedMsg{id: id, result: result, err: err}:
		case <-ctx.Done():
		}
	}()
	return m, tea.Batch(m.spinner.Tick, waitForScan(events))
}

func (m Model) cleanupFolderNames() []string {
	if m.settings.CleanupFolders == nil {
		return settings.DefaultCleanupFolders()
	}
	return append([]string(nil), m.settings.CleanupFolders...)
}

func (m Model) updateCleanupScanning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == keyCancel {
		if m.cancel != nil {
			m.cancel()
		}
		m.cancel = nil
		m.scanID++
		m.screen = screenStart
		m.status = "Cleanup search cancelled."
		return m, m.pathInput.Focus()
	}

	switch typed := msg.(type) {
	case scanProgressMsg:
		if typed.id != m.scanID {
			return m, nil
		}
		m.latest = typed.progress
		return m, waitForScan(m.events)
	case scanWarningMsg:
		if typed.id != m.scanID {
			return m, nil
		}
		m.scanWarnings++
		return m, waitForScan(m.events)
	case cleanupFinishedMsg:
		if typed.id != m.scanID {
			return m, nil
		}
		m.cancel = nil
		if typed.err != nil {
			if errors.Is(typed.err, context.Canceled) {
				m.status = "Cleanup search cancelled."
			} else {
				m.status = "Cleanup search failed: " + typed.err.Error()
			}
			m.screen = screenStart
			return m, m.pathInput.Focus()
		}
		m.cleanupResult = typed.result
		m.cleanupSelected = 0
		m.screen = screenCleanupResults
		return m, nil
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

func (m Model) viewCleanupScanning() string {
	status := m.spinner.View() + " Searching for configured folder names"
	body := status + "\n\n" + m.styles.subtitle.Render("Current") + "\n" + m.latest.CurrentPath
	body += fmt.Sprintf("\n\n%d directories checked  •  %d matches  •  %s elapsed",
		m.latest.DirectoriesVisited, m.latest.MatchesFound, time.Since(m.started).Round(time.Second))
	body += "\nOnly directory names are checked; files are not opened or hashed. Matches are pruned."
	if m.scanWarnings > 0 {
		body += fmt.Sprintf("  •  %d warnings", m.scanWarnings)
	}
	return m.chrome(body, "esc cancel search  •  ctrl+c quit")
}

func (m Model) updateCleanupResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	if typed, ok := msg.(cleanupDeleteProgressMsg); ok {
		if typed.id != m.scanID {
			return m, nil
		}
		m.cleanupDeleteProgress = typed.progress
		return m, waitForScan(m.events)
	}
	if _, ok := msg.(cleanupDeletedMsg); ok {
		return m.updateCleanupDeletion(msg)
	}
	if m.cleanupConfirm {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case "y", "enter":
				m.cleanupConfirm = false
				return m.deleteCleanupFolders()
			case "n", keyCancel:
				m.cleanupConfirm = false
				return m, nil
			}
		}
		return m, nil
	}
	if m.cleanupBusy {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == keyCancel {
			if m.cancel != nil {
				m.cancel()
			}
			m.cleanupCancelRequested = true
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case keyQuit:
			return m, tea.Quit
		case keyCancel, keyNewScan:
			m.screen = screenStart
			m.focus = focusInput
			m.status = ""
			return m, m.pathInput.Focus()
		case keyRescan:
			return m.launchCleanupScan(m.cleanupResult.Root)
		case "m":
			return m.openCleanupSettings(screenCleanupResults)
		case keyWarnings:
			m.showWarnings = !m.showWarnings
			return m, nil
		case "d":
			if len(m.cleanupResult.Folders) > 0 {
				m.cleanupConfirm = true
			}
			return m, nil
		case keyMoveUp, keyMoveUpAlt:
			if m.cleanupSelected > 0 {
				m.cleanupSelected--
			}
			return m, nil
		case keyMoveDown, keyMoveDownAlt:
			if m.cleanupSelected+1 < len(m.cleanupResult.Folders) {
				m.cleanupSelected++
			}
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return m, cmd
}

func (m Model) deleteCleanupFolders() (tea.Model, tea.Cmd) {
	m.cleanupBusy = true
	m.cleanupCancelRequested = false
	m.cleanupNotice = ""
	m.status = ""
	m.scanID++
	id := m.scanID
	root := m.cleanupResult.Root
	folders := append([]scan.CleanupFolder(nil), m.cleanupResult.Folders...)
	m.cleanupDeleteProgress = scan.CleanupDeleteProgress{FoldersTotal: len(folders)}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 128)
	m.cancel = cancel
	m.events = events
	go func() {
		var lastProgressAt time.Time
		lastFoldersCompleted := -1
		emitProgress := func(progress scan.CleanupDeleteProgress) {
			now := time.Now()
			if !lastProgressAt.IsZero() && now.Sub(lastProgressAt) < 200*time.Millisecond &&
				progress.FoldersCompleted == lastFoldersCompleted && progress.FoldersCompleted != progress.FoldersTotal {
				return
			}
			select {
			case events <- cleanupDeleteProgressMsg{id: id, progress: progress}:
				lastProgressAt = now
				lastFoldersCompleted = progress.FoldersCompleted
			default:
			}
		}
		removed, warnings, err := scan.DeleteCleanupFolders(ctx, root, folders, func(progress scan.CleanupDeleteProgress) {
			emitProgress(progress)
		})
		events <- cleanupDeletedMsg{id: id, removed: removed, warnings: warnings, err: err}
	}()
	return m, tea.Batch(m.spinner.Tick, waitForScan(events))
}

func (m Model) updateCleanupDeletion(msg tea.Msg) (tea.Model, tea.Cmd) {
	typed, ok := msg.(cleanupDeletedMsg)
	if !ok || typed.id != m.scanID {
		return m, nil
	}
	m.cleanupBusy = false
	m.cleanupCancelRequested = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if typed.err != nil && !errors.Is(typed.err, context.Canceled) {
		m.status = "Cleanup failed: " + typed.err.Error()
		return m, nil
	}
	removed := make(map[string]struct{}, len(typed.removed))
	for _, path := range typed.removed {
		removed[path] = struct{}{}
	}
	remaining := m.cleanupResult.Folders[:0]
	for _, folder := range m.cleanupResult.Folders {
		if _, ok := removed[folder.Path]; !ok {
			remaining = append(remaining, folder)
		}
	}
	m.cleanupResult.Folders = remaining
	m.cleanupResult.Warnings = append(m.cleanupResult.Warnings, typed.warnings...)
	if errors.Is(typed.err, context.Canceled) {
		m.cleanupNotice = fmt.Sprintf("Deletion stopped after removing %d folders. Remaining matches may be partially deleted; rescan before retrying.", len(typed.removed))
	} else if len(typed.warnings) == 0 {
		m.cleanupNotice = fmt.Sprintf("Deleted %d cleanup folders.", len(typed.removed))
	} else {
		m.cleanupNotice = fmt.Sprintf("Deleted %d folders; %d could not be removed. See warnings.", len(typed.removed), len(typed.warnings))
	}
	if m.cleanupSelected >= len(m.cleanupResult.Folders) {
		m.cleanupSelected = max(0, len(m.cleanupResult.Folders)-1)
	}
	return m, nil
}

func (m Model) viewCleanupResults() string {
	duration := m.cleanupResult.FinishedAt.Sub(m.cleanupResult.StartedAt).Round(time.Millisecond)
	summary := fmt.Sprintf("%s\n%d matching folders  •  %s", m.cleanupResult.Root, len(m.cleanupResult.Folders), duration)

	var body string
	if m.showWarnings {
		body = m.cleanupWarningView()
	} else if len(m.cleanupResult.Folders) == 0 {
		body = m.styles.accent.Render("No matching folders found.")
	} else {
		body = m.cleanupFolderView()
	}
	body = "Search stops at each matching directory; deletion removes its entire tree.\n\n" + body
	rules := strings.Join(m.cleanupResult.Names, ", ")
	if rules == "" {
		rules = "(none configured)"
	}
	body = "Matching exact folder names: " + rules + "\n\n" + body
	if m.cleanupNotice != "" {
		body += "\n\n" + m.styles.accent.Render(m.cleanupNotice)
	}
	if m.cleanupBusy {
		progress := m.cleanupDeleteProgress
		body += fmt.Sprintf("\n\n%s Deleting folders: %d/%d complete  •  %d entries removed",
			m.spinner.View(), progress.FoldersCompleted, progress.FoldersTotal, progress.EntriesRemoved)
		if progress.CurrentPath != "" {
			body += "\nCurrent: " + progress.CurrentPath
		}
		if m.cleanupCancelRequested {
			body += "\nStopping after the current filesystem operation..."
		}
	}
	if m.cleanupConfirm {
		body += "\n\n" + m.styles.errorText.Render(fmt.Sprintf("Delete all %d matching folders and everything inside them? Press y to confirm or Esc to cancel.", len(m.cleanupResult.Folders)))
	}
	body += m.errorLine()
	help := "↑/↓ navigate  •  d delete all  •  m edit folder names  •  r rescan  •  w warnings  •  n new root  •  q quit"
	if m.cleanupConfirm {
		help = "y confirm delete  •  esc cancel"
	} else if m.cleanupBusy {
		help = "esc cancel deletion  •  ctrl+c quit"
	}
	return m.chrome(summary+"\n\n"+body, help)
}

func (m Model) cleanupFolderView() string {
	visible := max(3, m.height-16)
	start := 0
	if m.cleanupSelected >= visible {
		start = m.cleanupSelected - visible + 1
	}
	end := min(len(m.cleanupResult.Folders), start+visible)
	lines := []string{fmt.Sprintf("Found folders (%d)", len(m.cleanupResult.Folders)), ""}
	for i := start; i < end; i++ {
		folder := m.cleanupResult.Folders[i]
		line := folder.Path
		if i == m.cleanupSelected {
			line = "> " + m.styles.selected.Render(line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	width := max(36, m.width-8)
	return m.styles.focusedPanel.Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) cleanupWarningView() string {
	if len(m.cleanupResult.Warnings) == 0 {
		return m.styles.panel.Render("Warnings\n\nNo warnings occurred.")
	}
	lines := []string{fmt.Sprintf("Warnings (%d)", len(m.cleanupResult.Warnings)), ""}
	limit := min(len(m.cleanupResult.Warnings), max(3, m.height-14))
	for _, warning := range m.cleanupResult.Warnings[:limit] {
		lines = append(lines, fmt.Sprintf("%s: %v", warning.Path, warning.Err))
	}
	return m.styles.panel.Width(max(36, m.width-8)).Render(strings.Join(lines, "\n"))
}

func (m Model) openCleanupSettings(returnTo screen) (tea.Model, tea.Cmd) {
	if m.settings.CleanupFolders == nil {
		m.settings.CleanupFolders = settings.DefaultCleanupFolders()
	}
	m.screen = screenCleanupSettings
	m.cleanupSettingsReturn = returnTo
	m.cleanupSettingsIndex = 0
	m.cleanupEditing = false
	m.cleanupEditingIndex = -1
	m.settingInput.Blur()
	return m, nil
}

func (m Model) closeCleanupSettings() (tea.Model, tea.Cmd) {
	m.screen = m.cleanupSettingsReturn
	if m.screen == screenStart {
		m.focus = focusInput
		return m, m.pathInput.Focus()
	}
	return m, nil
}

func (m Model) updateCleanupSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.cleanupEditing {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case keyCancel:
				m.cleanupEditing = false
				m.settingInput.Blur()
				m.status = ""
				return m, nil
			case "enter":
				if !m.commitCleanupFolder() {
					return m, nil
				}
				m.cleanupEditing = false
				m.settingInput.Blur()
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.settingInput, cmd = m.settingInput.Update(msg)
		return m, cmd
	}

	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case keyCancel:
			return m.closeCleanupSettings()
		case keyMoveUp, keyMoveUpAlt:
			if m.cleanupSettingsIndex > 0 {
				m.cleanupSettingsIndex--
			}
			return m, nil
		case keyMoveDown, keyMoveDownAlt:
			if m.cleanupSettingsIndex+1 < 1+len(m.settings.CleanupFolders) {
				m.cleanupSettingsIndex++
			}
			return m, nil
		case "enter":
			m.cleanupEditingIndex = m.cleanupSettingsIndex - 1
			m.settingInput.Prompt = "Folder name: "
			m.settingInput.SetValue("")
			if m.cleanupEditingIndex >= 0 {
				m.settingInput.SetValue(m.settings.CleanupFolders[m.cleanupEditingIndex])
			}
			m.cleanupEditing = true
			return m, m.settingInput.Focus()
		case "d", "backspace":
			if m.cleanupSettingsIndex > 0 {
				index := m.cleanupSettingsIndex - 1
				m.settings.CleanupFolders = append(m.settings.CleanupFolders[:index], m.settings.CleanupFolders[index+1:]...)
				if m.cleanupSettingsIndex >= 1+len(m.settings.CleanupFolders) {
					m.cleanupSettingsIndex--
				}
				m.saveSettings()
				if m.cleanupSettingsReturn == screenCleanupResults {
					m.cleanupNotice = "Folder rules changed. Press r to search again."
				}
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) commitCleanupFolder() bool {
	value := strings.TrimSpace(m.settingInput.Value())
	if value == "" || value == "." || value == ".." || strings.ContainsAny(value, `/\\`) {
		m.status = "Enter a single folder name, without a path."
		return false
	}
	for i, folder := range m.settings.CleanupFolders {
		if folder == value && i != m.cleanupEditingIndex {
			m.status = "That folder name is already in the cleanup list."
			return false
		}
	}
	if m.cleanupEditingIndex < 0 {
		m.settings.CleanupFolders = append(m.settings.CleanupFolders, value)
		m.cleanupSettingsIndex = len(m.settings.CleanupFolders)
	} else {
		m.settings.CleanupFolders[m.cleanupEditingIndex] = value
	}
	m.status = ""
	m.saveSettings()
	if m.cleanupSettingsReturn == screenCleanupResults {
		m.cleanupNotice = "Folder rules changed. Press r to search again."
	}
	return true
}

func (m Model) viewCleanupSettings() string {
	rows := []string{"Add a folder name"}
	for _, folder := range m.settings.CleanupFolders {
		rows = append(rows, folder)
	}
	for index, row := range rows {
		if index == m.cleanupSettingsIndex {
			rows[index] = "> " + m.styles.selected.Render(row)
		} else {
			rows[index] = "  " + row
		}
	}
	body := "Matches exact directory names anywhere below the selected root. Search skips a matched directory's contents; deletion removes its whole tree.\n\n"
	body += m.styles.focusedPanel.Width(max(36, min(76, m.width-8))).Render("Cleanup folder names\n\n" + strings.Join(rows, "\n"))
	if m.cleanupEditing {
		body += "\n\n" + m.settingInput.View()
	}
	body += m.errorLine()
	help := "↑/↓ or j/k choose  •  enter add/edit  •  d remove  •  esc back"
	if m.cleanupEditing {
		help = "enter save  •  esc cancel"
	}
	return m.chrome(body, help)
}
