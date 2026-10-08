package app

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/xlukacs/Deduper/internal/report"
	"github.com/xlukacs/Deduper/internal/scan"
)

func (m Model) updateResults(msg tea.Msg) (tea.Model, tea.Cmd) {
	if typed, ok := msg.(duplicatesDeletedMsg); ok {
		return m.updateDuplicateDeletion(typed)
	}
	if m.deleteConfirm || m.deleteBusy {
		return m.updateDeleteReview(msg)
	}
	if m.filtering {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case keyCancel, "enter":
				m.filtering = false
				m.filterInput.Blur()
				if key.String() == keyCancel {
					m.filterInput.SetValue("")
					m.applyFilter()
				}
				return m, nil
			}
		}
		var cmd tea.Cmd
		m.filterInput, cmd = m.filterInput.Update(msg)
		m.applyFilter()
		return m, cmd
	}

	if key, ok := msg.(tea.KeyPressMsg); ok {
		if len(m.deleteMarked) > 0 {
			switch key.String() {
			case keyQuit, keyCancel, keyNewScan, keyRescan:
				m.deleteAfter = key.String()
				m.deleteConfirm = true
				m.deleteReviewOffset = 0
				return m, nil
			}
		}
		switch key.String() {
		case keyDeleteMark:
			m.toggleDeleteMark()
			return m, nil
		case keyDeleteReview:
			if len(m.deleteMarked) > 0 {
				m.deleteConfirm = true
				m.deleteAfter = ""
				m.deleteReviewOffset = 0
			}
			return m, nil
		case keyDeleteClear:
			m.deleteMarked = nil
			m.deleteNotice = "Deletion marks cleared."
			return m, nil
		case keyQuit:
			return m, tea.Quit
		case keyFilter:
			m.filtering = true
			return m, m.filterInput.Focus()
		case keyCancel, keyNewScan:
			m.screen = screenStart
			m.focus = focusInput
			m.status = ""
			return m, m.pathInput.Focus()
		case keyRescan:
			return m.launchScan(m.result.Root)
		case keyWarnings:
			m.showWarnings = !m.showWarnings
			return m, nil
		case keyOptions:
			return m.openSettings(screenResults)
		case keySort:
			m.groupSort = (m.groupSort + 1) % sortCount
			m.sortGroups()
			m.selectedGroup = 0
			m.detailOffset = 0
			return m, nil
		case keyNextFocus, keyPrevFocus:
			if m.focus == focusGroups {
				m.focus = focusDetails
			} else {
				m.focus = focusGroups
			}
			return m, nil
		case keyMoveUp, keyMoveUpAlt:
			if m.focus == focusGroups && m.selectedGroup > 0 {
				m.selectedGroup--
				m.detailOffset = 0
			} else if m.focus == focusDetails && m.detailOffset > 0 {
				m.detailOffset--
			}
			return m, nil
		case keyMoveDown, keyMoveDownAlt:
			if m.focus == focusGroups && m.selectedGroup+1 < len(m.groups) {
				m.selectedGroup++
				m.detailOffset = 0
			} else if m.focus == focusDetails && m.selectedGroup < len(m.groups) && m.detailOffset+1 < len(m.groups[m.selectedGroup].Files) {
				m.detailOffset++
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) applyFilter() {
	query := strings.ToLower(strings.TrimSpace(m.filterInput.Value()))
	if query == "" {
		m.groups = append(m.groups[:0], m.result.Groups...)
	} else {
		m.groups = m.groups[:0]
		for _, group := range m.result.Groups {
			for _, file := range group.Files {
				if strings.Contains(strings.ToLower(file.Path), query) {
					m.groups = append(m.groups, group)
					break
				}
			}
		}
	}
	m.selectedGroup = 0
	m.detailOffset = 0
	m.sortGroups()
}

func (m *Model) sortGroups() {
	sort.SliceStable(m.groups, func(i, j int) bool {
		left, right := m.groups[i], m.groups[j]
		switch m.groupSort {
		case sortSize:
			if left.Size != right.Size {
				return left.Size > right.Size
			}
		case sortCopies:
			if len(left.Files) != len(right.Files) {
				return len(left.Files) > len(right.Files)
			}
		case sortUpdated:
			leftUpdated, rightUpdated := groupUpdated(left), groupUpdated(right)
			if !leftUpdated.Equal(rightUpdated) {
				return leftUpdated.After(rightUpdated)
			}
		case sortPath:
			leftPath, rightPath := groupPath(left), groupPath(right)
			if leftPath != rightPath {
				return leftPath < rightPath
			}
		default:
			if left.ReclaimableBytes != right.ReclaimableBytes {
				return left.ReclaimableBytes > right.ReclaimableBytes
			}
		}

		// Keep ties deterministic and useful regardless of the selected mode.
		if left.ReclaimableBytes != right.ReclaimableBytes {
			return left.ReclaimableBytes > right.ReclaimableBytes
		}
		if left.Size != right.Size {
			return left.Size > right.Size
		}
		return bytes.Compare(left.Hash[:], right.Hash[:]) < 0
	})
}

func (s groupSort) label() string {
	switch s {
	case sortSize:
		return "biggest files"
	case sortCopies:
		return "most copies"
	case sortUpdated:
		return "last updated"
	case sortPath:
		return "path"
	default:
		return "space saved"
	}
}

func groupUpdated(group scan.DuplicateGroup) time.Time {
	var latest time.Time
	for _, file := range group.Files {
		if file.Info != nil && file.Info.ModTime().After(latest) {
			latest = file.Info.ModTime()
		}
	}
	return latest
}

func groupPath(group scan.DuplicateGroup) string {
	if len(group.Files) == 0 {
		return ""
	}
	path := group.Files[0].Path
	for _, file := range group.Files[1:] {
		if file.Path < path {
			path = file.Path
		}
	}
	return path
}

func (m Model) viewResults() string {
	if m.deleteConfirm {
		return m.viewDeleteReview()
	}
	duration := m.result.FinishedAt.Sub(m.result.StartedAt).Round(time.Millisecond)
	var reclaimable int64
	var copies int
	for _, group := range m.result.Groups {
		reclaimable += group.ReclaimableBytes
		copies += len(group.Files) - 1
	}
	summary := fmt.Sprintf("%s\n%d files discovered  •  %d hashed  •  %d groups  •  %d excess copies  •  %s reclaimable  •  %s",
		m.result.Root, m.result.Stats.FilesDiscovered, m.result.Stats.FilesHashed, len(m.result.Groups), copies, report.Bytes(reclaimable), duration)

	var body string
	if m.showWarnings {
		body = m.warningView()
	} else if len(m.result.Groups) == 0 {
		body = m.styles.accent.Render("No duplicates found.")
	} else {
		groups := m.groupView()
		details := m.detailView()
		if m.width > 0 && m.width < 100 {
			body = lipgloss.JoinVertical(lipgloss.Left, groups, details)
		} else {
			body = lipgloss.JoinHorizontal(lipgloss.Top, groups, "  ", details)
		}
	}
	if m.filtering || m.filterInput.Value() != "" {
		body = m.filterInput.View() + "\n\n" + body
	}
	if len(m.deleteMarked) > 0 {
		body += fmt.Sprintf("\n\n%d files flagged for deletion · %s", len(m.deleteMarked), report.Bytes(m.markedBytes()))
	}
	if m.deleteNotice != "" {
		body += "\n\n" + m.styles.accent.Render(m.deleteNotice)
	}
	help := "↑/↓ or j/k navigate  •  tab switch pane  •  d mark/unmark file  •  enter review deletion  •  u clear marks\n/ filter  •  s sort  •  o settings  •  r rescan  •  n new  •  w warnings  •  q quit"
	if m.deleteBusy {
		body += "\n\n" + m.spinner.View() + " Verifying and deleting flagged files..."
		help = "esc stop deletion  •  ctrl+c quit"
	}
	return m.chrome(summary+"\n\n"+body, help)
}

func (m Model) groupView() string {
	width := 42
	if m.width > 0 && m.width < 100 {
		width = max(24, m.width-4)
	}
	visible := max(3, m.height/3)
	start := 0
	if m.selectedGroup >= visible {
		start = m.selectedGroup - visible + 1
	}
	end := min(len(m.groups), start+visible)
	var lines []string
	for i := start; i < end; i++ {
		group := m.groups[i]
		line := fmt.Sprintf("%d files · %s · save %s", len(group.Files), report.Bytes(group.Size), report.Bytes(group.ReclaimableBytes))
		if m.groupSort == sortUpdated {
			updated := groupUpdated(group)
			if updated.IsZero() {
				line = fmt.Sprintf("%d files · %s · updated unknown", len(group.Files), report.Bytes(group.Size))
			} else {
				line = fmt.Sprintf("%d files · %s · updated %s", len(group.Files), report.Bytes(group.Size), updated.Format("2006-01-02"))
			}
		}
		marked := 0
		for _, file := range group.Files {
			if m.deleteMarked[file.Path] {
				marked++
			}
		}
		if marked > 0 {
			line += fmt.Sprintf(" · %d flagged", marked)
		}
		if i == m.selectedGroup {
			line = "> " + m.styles.selected.Render(line)
		} else {
			line = "  " + line
		}
		lines = append(lines, line)
	}
	style := m.styles.panel
	if m.focus == focusGroups {
		style = m.styles.focusedPanel
	}
	title := fmt.Sprintf("Duplicate groups · %s", m.groupSort.label())
	return style.Width(width).Render(title + "\n\n" + strings.Join(lines, "\n"))
}

func (m Model) detailView() string {
	width := 70
	if m.width > 0 {
		if m.width < 100 {
			width = max(24, m.width-4)
		} else {
			width = max(30, m.width-50)
		}
	}
	content := "No matching groups."
	if m.selectedGroup < len(m.groups) {
		group := m.groups[m.selectedGroup]
		lines := []string{fmt.Sprintf("SHA-256: %x", group.Hash), ""}
		visible := max(2, m.height/3)
		start := max(0, m.detailOffset-visible+1)
		end := min(len(group.Files), start+visible)
		for i := start; i < end; i++ {
			file := group.Files[i]
			marker := "[ ] "
			if m.deleteMarked[file.Path] {
				marker = "[DELETE] "
			}
			line := marker + file.Path
			if i == m.detailOffset && m.focus == focusDetails {
				line = "> " + m.styles.selected.Render(line)
			} else {
				line = "  " + line
			}
			lines = append(lines, line)
		}
		content = strings.Join(lines, "\n")
	}
	style := m.styles.panel
	if m.focus == focusDetails {
		style = m.styles.focusedPanel
	}
	return style.Width(width).Render("Matching files\n\n" + content)
}

func (m Model) warningView() string {
	if len(m.result.Warnings) == 0 && len(m.deleteWarnings) == 0 {
		return m.styles.panel.Render("Warnings\n\nNo warnings occurred.")
	}
	var lines []string
	remaining := max(3, m.height-12)
	addSection := func(title string, warnings []scan.Warning) {
		if len(warnings) == 0 || remaining <= 0 {
			return
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("%s (%d)", title, len(warnings)), "")
		shown := min(len(warnings), remaining)
		for _, warning := range warnings[:shown] {
			lines = append(lines, fmt.Sprintf("%s: %v", warning.Path, warning.Err))
		}
		remaining -= shown
	}
	addSection("Deletion problems", m.deleteWarnings)
	addSection("Scan warnings", m.result.Warnings)
	return m.styles.panel.Render(strings.Join(lines, "\n"))
}
