package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m Model) updateStart(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case keyNextFocus, keyPrevFocus:
			if len(m.recent) > 0 && m.focus == focusInput {
				m.focus = focusRecent
				m.pathInput.Blur()
			} else {
				m.focus = focusInput
				return m, m.pathInput.Focus()
			}
			return m, nil
		case "enter":
			path := strings.TrimSpace(m.pathInput.Value())
			if m.focus == focusRecent && len(m.recent) > 0 {
				path = m.recent[m.recentIndex]
				m.pathInput.SetValue(path)
			}
			return m.beginScan(path)
		case keyMoveUp, keyMoveUpAlt:
			if m.focus == focusRecent && m.recentIndex > 0 {
				m.recentIndex--
			}
			return m, nil
		case keyMoveDown, keyMoveDownAlt:
			if m.focus == focusRecent && m.recentIndex+1 < len(m.recent) {
				m.recentIndex++
			}
			return m, nil
		case keyQuit:
			if m.focus == focusRecent {
				return m, tea.Quit
			}
		}
	}
	if m.focus == focusInput {
		var cmd tea.Cmd
		m.pathInput, cmd = m.pathInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) beginScan(path string) (tea.Model, tea.Cmd) {
	abs, err := validRoot(path)
	if err != nil {
		m.status = err.Error()
		return m, nil
	}
	m.status = ""
	if m.history != nil {
		paths, saveErr := m.history.Add(abs)
		m.recent = paths
		if saveErr != nil {
			m.status = "Could not save history: " + saveErr.Error()
		}
	}
	return m.launchScan(abs)
}

func validRoot(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("enter a folder to scan")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return "", fmt.Errorf("cannot access folder: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("the scan root cannot be a symbolic link")
	}
	if !info.IsDir() {
		return "", fmt.Errorf("the scan root is not a directory")
	}
	return filepath.Clean(abs), nil
}

func (m Model) viewStart() string {
	inputStyle := m.styles.panel
	if m.focus == focusInput {
		inputStyle = m.styles.focusedPanel
	}
	body := "Choose a folder. Deduper reads files but never changes them.\n\n"
	body += inputStyle.Render(m.pathInput.View())
	if len(m.recent) > 0 {
		body += "\n\n" + m.styles.subtitle.Render("Recent folders") + "\n"
		for i, path := range m.recent {
			prefix := "  "
			line := path
			if m.focus == focusRecent && i == m.recentIndex {
				prefix = "> "
				line = m.styles.selected.Render(path)
			}
			body += prefix + line + "\n"
		}
	}
	body += m.errorLine()
	return m.chrome(body, "enter scan  •  tab switch focus  •  ↑/↓ choose recent  •  ctrl+c quit")
}
