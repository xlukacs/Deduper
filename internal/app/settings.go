package app

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/scan"
)

const (
	settingIgnoreHidden = iota
	settingWorkers
	settingAddExcluded
	settingFirstExcluded
)

func (m Model) openSettings(returnTo screen) (tea.Model, tea.Cmd) {
	m.screen = screenSettings
	m.settingsReturn = returnTo
	m.settingsIndex = 0
	m.editingSettings = false
	m.settingInput.Blur()
	return m, nil
}

func (m Model) closeSettings() (tea.Model, tea.Cmd) {
	m.screen = m.settingsReturn
	if m.screen == screenStart {
		m.focus = focusInput
		return m, m.pathInput.Focus()
	}
	return m, nil
}

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.editingSettings {
		if key, ok := msg.(tea.KeyPressMsg); ok {
			switch key.String() {
			case keyCancel:
				m.editingSettings = false
				m.settingInput.Blur()
				return m, nil
			case "enter":
				if !m.commitSettingInput() {
					return m, nil
				}
				m.editingSettings = false
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
		case keyCancel, keyOptions:
			return m.closeSettings()
		case keyMoveUp, keyMoveUpAlt:
			if m.settingsIndex > 0 {
				m.settingsIndex--
			}
			return m, nil
		case keyMoveDown, keyMoveDownAlt:
			if m.settingsIndex+1 < m.settingsRowCount() {
				m.settingsIndex++
			}
			return m, nil
		case "enter":
			switch m.settingsIndex {
			case settingIgnoreHidden:
				m.settings.IgnoreHiddenFolders = !m.settings.IgnoreHiddenFolders
				m.saveSettings()
			case settingWorkers:
				m.settingInput.Prompt = "Workers: "
				m.settingInput.SetValue(strconv.Itoa(m.workerCount()))
				m.editingSettings = true
				return m, m.settingInput.Focus()
			case settingAddExcluded:
				m.settingInput.Prompt = "Exclude: "
				m.settingInput.SetValue("")
				m.editingSettings = true
				return m, m.settingInput.Focus()
			}
			return m, nil
		case "d", "backspace":
			if m.settingsIndex >= settingFirstExcluded {
				index := m.settingsIndex - settingFirstExcluded
				m.settings.ExcludedFolders = append(m.settings.ExcludedFolders[:index], m.settings.ExcludedFolders[index+1:]...)
				if m.settingsIndex >= m.settingsRowCount() {
					m.settingsIndex--
				}
				m.saveSettings()
			}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) commitSettingInput() bool {
	value := strings.TrimSpace(m.settingInput.Value())
	switch m.settingsIndex {
	case settingWorkers:
		workers, err := strconv.Atoi(value)
		if err != nil || workers < 1 {
			m.status = "Workers must be a positive whole number."
			return false
		}
		m.settings.Workers = workers
	case settingAddExcluded:
		if value == "" {
			m.status = "Enter a folder name or path."
			return false
		}
		m.settings.ExcludedFolders = append(m.settings.ExcludedFolders, value)
	}
	m.status = ""
	m.saveSettings()
	return true
}

func (m *Model) saveSettings() {
	if m.settingsStore == nil {
		m.status = "Settings cannot be saved on this system."
		return
	}
	if err := m.settingsStore.Save(m.settings); err != nil {
		m.status = "Could not save settings: " + err.Error()
		return
	}
	config, err := m.settingsStore.Load()
	if err != nil {
		m.status = "Could not reload settings: " + err.Error()
		return
	}
	m.settings = config
}

func (m Model) settingsRowCount() int {
	return settingFirstExcluded + len(m.settings.ExcludedFolders)
}

func (m Model) workerCount() int {
	if m.settings.Workers > 0 {
		return m.settings.Workers
	}
	return scan.DefaultOptions().Workers
}

func (m Model) scanOptions() scan.Options {
	options := scan.DefaultOptions()
	if m.settings.Workers > 0 {
		options.Workers = m.settings.Workers
	}
	options.IgnoreHiddenFolders = m.settings.IgnoreHiddenFolders
	options.ExcludedFolders = append([]string(nil), m.settings.ExcludedFolders...)
	return options
}

func (m Model) viewSettings() string {
	check := "[ ]"
	if m.settings.IgnoreHiddenFolders {
		check = "[x]"
	}
	rows := []string{
		fmt.Sprintf("%s Ignore hidden folders (.git, .turbo, .ssh)", check),
		fmt.Sprintf("Workers: %d", m.workerCount()),
		"Add excluded folder or path",
	}
	for _, folder := range m.settings.ExcludedFolders {
		rows = append(rows, "Exclude: "+folder)
	}
	for index, row := range rows {
		if index == m.settingsIndex {
			rows[index] = "> " + m.styles.selected.Render(row)
		} else {
			rows[index] = "  " + row
		}
	}
	body := m.styles.focusedPanel.Width(max(36, min(76, m.width-8))).Render("Scan settings\n\n" + strings.Join(rows, "\n"))
	if m.editingSettings {
		body += "\n\n" + m.settingInput.View()
	}
	body += m.errorLine()
	help := "↑/↓ or j/k choose  •  enter edit/toggle  •  d remove exclusion  •  esc back"
	if m.editingSettings {
		help = "enter save  •  esc cancel"
	}
	return m.chrome(body, help)
}
