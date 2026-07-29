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
		switch key.String() {
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
	help := "↑/↓ or j/k navigate  •  tab switch pane  •  / filter  •  s sort  •  r rescan  •  n new  •  w warnings  •  q quit"
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
		end := min(len(group.Files), m.detailOffset+visible)
		for _, file := range group.Files[m.detailOffset:end] {
			lines = append(lines, file.Path)
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
	if len(m.result.Warnings) == 0 {
		return m.styles.panel.Render("Warnings\n\nNo warnings occurred.")
	}
	lines := make([]string, 0, len(m.result.Warnings)+1)
	lines = append(lines, fmt.Sprintf("Warnings (%d)", len(m.result.Warnings)), "")
	limit := min(len(m.result.Warnings), max(3, m.height-12))
	for _, warning := range m.result.Warnings[:limit] {
		lines = append(lines, fmt.Sprintf("%s: %v", warning.Path, warning.Err))
	}
	return m.styles.panel.Render(strings.Join(lines, "\n"))
}
