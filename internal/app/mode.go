package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	modeDuplicates = iota
	modeCleanup
	modeEditCleanupFolders
)

func (m Model) updateModeSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "enter":
			switch m.modeSelected {
			case modeDuplicates:
				return m.launchScan(m.selectedRoot)
			case modeCleanup:
				return m.launchCleanupScan(m.selectedRoot)
			case modeEditCleanupFolders:
				return m.openCleanupSettings(screenModeSelect)
			}
		case keyMoveUp, keyMoveUpAlt:
			if m.modeSelected > modeDuplicates {
				m.modeSelected--
			}
			return m, nil
		case keyMoveDown, keyMoveDownAlt:
			if m.modeSelected < modeEditCleanupFolders {
				m.modeSelected++
			}
			return m, nil
		case keyCancel:
			m.screen = screenStart
			m.focus = focusInput
			return m, m.pathInput.Focus()
		case keyOptions:
			return m.openSettings(screenModeSelect)
		case keyQuit:
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) viewModeSelect() string {
	titles := []string{
		"Find duplicate files",
		"Clean up matching folders",
		"Edit cleanup folder names",
	}
	descriptions := []string{
		"Compare file contents and show duplicate groups. This mode does not change files.",
		"Find configured folder names, review the matches, and confirm before deleting.",
		"Add, rename, or remove the exact directory names cleanup mode searches for.",
	}
	lines := []string{"Selected folder", m.selectedRoot, "", "Choose what to do:"}
	for index, title := range titles {
		prefix := "  "
		if index == m.modeSelected {
			prefix = "> "
			title = m.styles.selected.Render(title)
		}
		lines = append(lines, prefix+title, "    "+descriptions[index], "")
	}
	body := m.styles.focusedPanel.Width(max(48, min(88, m.width-8))).Render(strings.Join(lines, "\n"))
	body += m.errorLine()
	return m.chrome(body, "↑/↓ or j/k choose  •  enter select  •  esc change folder  •  o scan settings  •  q quit")
}
