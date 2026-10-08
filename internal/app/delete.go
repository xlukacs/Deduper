package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/report"
	"github.com/xlukacs/Deduper/internal/scan"
)

type duplicatesDeletedMsg struct {
	id       int64
	removed  []string
	warnings []scan.Warning
	err      error
}

func (m *Model) toggleDeleteMark() {
	if m.showWarnings {
		m.deleteNotice = "Press w to return to Matching files before flagging a file."
		return
	}
	if m.focus != focusDetails {
		m.deleteNotice = "Tab to Matching files, select a file, then press d to flag it."
		return
	}
	if m.selectedGroup < 0 || m.selectedGroup >= len(m.groups) {
		return
	}
	group := m.groups[m.selectedGroup]
	if m.detailOffset < 0 || m.detailOffset >= len(group.Files) {
		return
	}
	path := group.Files[m.detailOffset].Path
	if m.deleteMarked[path] {
		delete(m.deleteMarked, path)
		m.deleteNotice = ""
		return
	}
	marked := 0
	for _, file := range group.Files {
		if m.deleteMarked[file.Path] {
			marked++
		}
	}
	if marked >= len(group.Files)-1 {
		m.deleteNotice = "Keep at least one unflagged copy in each duplicate group."
		return
	}
	if m.deleteMarked == nil {
		m.deleteMarked = make(map[string]bool)
	}
	m.deleteMarked[path] = true
	m.deleteNotice = ""
}

func (m Model) markedBytes() int64 {
	var total int64
	for _, group := range m.result.Groups {
		for _, file := range group.Files {
			if m.deleteMarked[file.Path] {
				total += file.Size
			}
		}
	}
	return total
}

func (m Model) markedPaths() []string {
	paths := make([]string, 0, len(m.deleteMarked))
	for path := range m.deleteMarked {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (m Model) viewDeleteReview() string {
	paths := m.markedPaths()
	visible := max(2, m.height-13)
	start := min(m.deleteReviewOffset, max(0, len(paths)-visible))
	end := min(len(paths), start+visible)
	body := fmt.Sprintf("Permanently delete %d flagged files (%s)?\n\n", len(paths), report.Bytes(m.markedBytes()))
	body += "These files will be erased from disk.\n\n" + strings.Join(paths[start:end], "\n")
	if len(paths) > visible {
		body += fmt.Sprintf("\n\nShowing %d–%d of %d flagged files (↑/↓ to scroll).", start+1, end, len(paths))
	}
	return m.chrome(body, "y confirm deletion  •  n/esc cancel and keep marks  •  u discard marks")
}

func (m Model) updateDeleteReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.deleteBusy {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == keyCancel {
			if m.cancel != nil {
				m.cancel()
			}
			m.deleteAfter = ""
			m.deleteNotice = "Stopping deletion..."
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "y":
			return m.deleteDuplicates()
		case "n", keyCancel:
			m.deleteConfirm = false
			m.deleteAfter = ""
			return m, nil
		case keyDeleteClear:
			m.deleteConfirm = false
			m.deleteMarked = nil
			return m.finishDeleteAction()
		case keyMoveUp, keyMoveUpAlt:
			m.deleteReviewOffset = max(0, m.deleteReviewOffset-1)
		case keyMoveDown, keyMoveDownAlt:
			m.deleteReviewOffset = min(max(0, len(m.deleteMarked)-max(2, m.height-13)), m.deleteReviewOffset+1)
		}
	}
	return m, nil
}

func (m Model) deleteDuplicates() (tea.Model, tea.Cmd) {
	if len(m.deleteMarked) == 0 {
		m.deleteConfirm = false
		return m, nil
	}
	m.deleteConfirm = false
	m.deleteBusy = true
	m.deleteNotice = ""
	m.scanID++
	id := m.scanID
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	root := m.result.Root
	groups := append([]scan.DuplicateGroup(nil), m.result.Groups...)
	selected := make(map[string]bool, len(m.deleteMarked))
	for path := range m.deleteMarked {
		selected[path] = true
	}
	return m, tea.Batch(m.spinner.Tick, func() tea.Msg {
		removed, warnings, err := scan.DeleteDuplicates(ctx, root, groups, selected)
		return duplicatesDeletedMsg{id: id, removed: removed, warnings: warnings, err: err}
	})
}

func (m Model) updateDuplicateDeletion(msg duplicatesDeletedMsg) (tea.Model, tea.Cmd) {
	if msg.id != m.scanID || !m.deleteBusy {
		return m, nil
	}
	m.deleteBusy = false
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	removed := make(map[string]bool, len(msg.removed))
	for _, path := range msg.removed {
		removed[path] = true
		delete(m.deleteMarked, path)
	}
	groups := make([]scan.DuplicateGroup, 0, len(m.result.Groups))
	for _, group := range m.result.Groups {
		files := make([]scan.FileRecord, 0, len(group.Files))
		for _, file := range group.Files {
			if !removed[file.Path] {
				files = append(files, file)
			}
		}
		if len(files) > 1 {
			group.Files = files
			group.ReclaimableBytes = group.Size * int64(len(files)-1)
			groups = append(groups, group)
		}
	}
	m.result.Groups = groups
	m.result.Warnings = append(m.result.Warnings, msg.warnings...)
	m.result.Stats.Errors += int64(len(msg.warnings))
	m.applyFilter()
	m.deleteNotice = fmt.Sprintf("Deleted %d flagged files.", len(msg.removed))
	if msg.err != nil {
		if errors.Is(msg.err, context.Canceled) {
			m.deleteNotice += " Deletion stopped."
		} else {
			m.deleteNotice += " Deletion failed: " + msg.err.Error()
		}
	}
	if len(msg.warnings) > 0 {
		m.deleteNotice += fmt.Sprintf(" %d files could not be deleted; press w for warnings.", len(msg.warnings))
	}
	if msg.err != nil || len(m.deleteMarked) > 0 {
		m.deleteAfter = ""
		return m, nil
	}
	return m.finishDeleteAction()
}

func (m Model) finishDeleteAction() (tea.Model, tea.Cmd) {
	action := m.deleteAfter
	m.deleteAfter = ""
	switch action {
	case keyQuit:
		return m, tea.Quit
	case keyCancel, keyNewScan:
		m.screen = screenStart
		m.focus = focusInput
		m.status = ""
		return m, m.pathInput.Focus()
	case keyRescan:
		return m.launchScan(m.result.Root)
	}
	return m, nil
}
