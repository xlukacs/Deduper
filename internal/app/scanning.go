package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/xlukacs/Deduper/internal/report"
	"github.com/xlukacs/Deduper/internal/scan"
)

type scanProgressMsg struct {
	id       int64
	progress scan.Progress
}

type scanWarningMsg struct {
	id int64
}

type scanFinishedMsg struct {
	id     int64
	result scan.Result
	err    error
}

type channelObserver struct {
	id     int64
	events chan<- tea.Msg
}

func (o channelObserver) OnProgress(progress scan.Progress) {
	select {
	case o.events <- scanProgressMsg{id: o.id, progress: progress}:
	default:
	}
}

func (o channelObserver) OnWarning(scan.Warning) {
	select {
	case o.events <- scanWarningMsg{id: o.id}:
	default:
	}
}

func (m Model) launchScan(root string) (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 128)
	m.scanID++
	id := m.scanID
	m.cancel = cancel
	m.events = events
	m.screen = screenScanning
	m.started = time.Now()
	m.latest = scan.Progress{Phase: scan.PhaseDiscovering, CurrentPath: root}
	m.scanWarnings = 0
	m.result = scan.Result{}
	m.showWarnings = false

	go func() {
		result, err := scan.Scan(ctx, root, channelObserver{id: id, events: events})
		select {
		case events <- scanFinishedMsg{id: id, result: result, err: err}:
		case <-ctx.Done():
		}
	}()
	return m, tea.Batch(m.spinner.Tick, waitForScan(events))
}

func waitForScan(events <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func (m Model) updateScanning(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == keyCancel {
		if m.cancel != nil {
			m.cancel()
		}
		m.cancel = nil
		m.scanID++
		m.screen = screenStart
		m.status = "Scan cancelled."
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
	case scanFinishedMsg:
		if typed.id != m.scanID {
			return m, nil
		}
		m.cancel = nil
		if typed.err != nil {
			if errors.Is(typed.err, context.Canceled) {
				m.status = "Scan cancelled."
			} else {
				m.status = "Scan failed: " + typed.err.Error()
			}
			m.screen = screenStart
			return m, m.pathInput.Focus()
		}
		m.result = typed.result
		m.groups = append([]scan.DuplicateGroup(nil), typed.result.Groups...)
		m.sortGroups()
		m.selectedGroup = 0
		m.detailOffset = 0
		m.focus = focusGroups
		m.screen = screenResults
		return m, nil
	default:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
}

func (m Model) viewScanning() string {
	phase := "Discovering files"
	status := m.spinner.View() + " " + phase
	if m.latest.Phase == scan.PhaseHashing {
		phase = "Hashing files"
		status = phase + fmt.Sprintf("  %d/%d", m.latest.FilesCompleted, m.latest.FilesTotal)
		fraction := 0.0
		if m.latest.BytesTotal > 0 {
			fraction = float64(m.latest.BytesCompleted) / float64(m.latest.BytesTotal)
		} else if m.latest.FilesTotal > 0 {
			fraction = float64(m.latest.FilesCompleted) / float64(m.latest.FilesTotal)
		}
		if fraction > 1 {
			fraction = 1
		}
		status += "\n\n" + m.progress.ViewAs(fraction)
	}
	elapsed := time.Since(m.started).Round(time.Second)
	body := status + "\n\n" + m.styles.subtitle.Render("Current") + "\n" + m.latest.CurrentPath
	body += fmt.Sprintf("\n\n%s processed  •  %s total  •  %s elapsed",
		report.Bytes(m.latest.BytesCompleted), report.Bytes(m.latest.BytesTotal), elapsed)
	if m.scanWarnings > 0 {
		body += fmt.Sprintf("  •  %d warnings", m.scanWarnings)
	}
	return m.chrome(body, "esc cancel scan  •  ctrl+c quit")
}
