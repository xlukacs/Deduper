// Package app contains the interactive terminal application.
package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/history"
	"github.com/xlukacs/Deduper/internal/scan"
	"github.com/xlukacs/Deduper/internal/settings"
)

type screen int

const (
	screenStart screen = iota
	screenScanning
	screenResults
	screenSettings
)

type focus int

const (
	focusInput focus = iota
	focusRecent
	focusGroups
	focusDetails
)

type groupSort int

const (
	sortReclaimable groupSort = iota
	sortSize
	sortCopies
	sortUpdated
	sortPath
	sortCount
)

// Model is the top-level Bubble Tea application model.
type Model struct {
	screen screen
	focus  focus
	width  int
	height int

	pathInput       textinput.Model
	filterInput     textinput.Model
	settingInput    textinput.Model
	filtering       bool
	recent          []string
	recentIndex     int
	history         *history.Store
	settings        settings.Config
	settingsStore   *settings.Store
	settingsIndex   int
	editingSettings bool
	settingsReturn  screen
	status          string

	spinner      spinner.Model
	progress     progress.Model
	latest       scan.Progress
	started      time.Time
	scanWarnings int
	cancel       context.CancelFunc
	scanID       int64
	events       <-chan tea.Msg

	result        scan.Result
	groups        []scan.DuplicateGroup
	selectedGroup int
	detailOffset  int
	showWarnings  bool
	groupSort     groupSort

	styles styles
}

// New constructs an application and loads recent roots.
func New() Model {
	pathInput := textinput.New()
	pathInput.Prompt = "> "
	pathInput.Placeholder = "/path/to/folder"
	pathInput.CharLimit = 4096
	pathInput.SetVirtualCursor(true)
	pathInput.SetWidth(72)
	pathInput.Focus()

	filterInput := textinput.New()
	filterInput.Prompt = "Filter: "
	filterInput.Placeholder = "part of a path"
	filterInput.CharLimit = 512
	filterInput.SetVirtualCursor(true)

	settingInput := textinput.New()
	settingInput.CharLimit = 512
	settingInput.SetVirtualCursor(true)

	model := Model{
		screen:       screenStart,
		focus:        focusInput,
		pathInput:    pathInput,
		filterInput:  filterInput,
		settingInput: settingInput,
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot)),
		progress:     progress.New(progress.WithDefaultBlend()),
		styles:       newStyles(),
	}
	store, err := history.New()
	if err != nil {
		model.status = "History unavailable: " + err.Error()
	} else {
		model.history = store
		paths, loadErr := store.Load()
		if loadErr != nil {
			model.status = "History ignored: " + loadErr.Error()
		} else {
			model.recent = paths
		}
	}
	settingsStore, err := settings.New()
	if err != nil {
		if model.status == "" {
			model.status = "Settings unavailable: " + err.Error()
		}
	} else {
		model.settingsStore = settingsStore
		config, loadErr := settingsStore.Load()
		if loadErr != nil {
			if model.status == "" {
				model.status = "Settings ignored: " + loadErr.Error()
			}
		} else {
			model.settings = config
		}
	}
	return model
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.pathInput.Focus(), m.spinner.Tick)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch typed := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = typed.Width, typed.Height
		m.resize()
		return m, nil
	case tea.KeyPressMsg:
		if typed.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
	}

	switch m.screen {
	case screenStart:
		return m.updateStart(msg)
	case screenScanning:
		return m.updateScanning(msg)
	case screenResults:
		return m.updateResults(msg)
	case screenSettings:
		return m.updateSettings(msg)
	default:
		return m, nil
	}
}

func (m Model) View() tea.View {
	var content string
	switch m.screen {
	case screenStart:
		content = m.viewStart()
	case screenScanning:
		content = m.viewScanning()
	case screenResults:
		content = m.viewResults()
	case screenSettings:
		content = m.viewSettings()
	}
	view := tea.NewView(content)
	view.AltScreen = true
	view.WindowTitle = "Deduper"
	return view
}

func (m *Model) resize() {
	usable := max(20, m.width-8)
	m.pathInput.SetWidth(min(72, usable))
	m.filterInput.SetWidth(min(60, usable))
	m.progress.SetWidth(min(72, usable))
}

func (m Model) chrome(body, help string) string {
	header := m.styles.title.Render("DEDUPER") + "  " + m.styles.subtitle.Render("content-based duplicate finder")
	content := header + "\n\n" + body
	if m.height > 0 {
		lines := strings.Count(content, "\n") + strings.Count(help, "\n") + 2
		if gap := m.height - lines - 1; gap > 0 {
			content += strings.Repeat("\n", gap)
		}
	}
	return content + "\n" + m.styles.muted.Render(help)
}

func (m Model) errorLine() string {
	if m.status == "" {
		return ""
	}
	return "\n" + m.styles.errorText.Render(m.status)
}

func (m Model) String() string {
	return fmt.Sprintf("deduper screen=%d", m.screen)
}
