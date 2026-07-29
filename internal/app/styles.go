package app

import "charm.land/lipgloss/v2"

type styles struct {
	title, subtitle, accent, muted, errorText lipgloss.Style
	panel, focusedPanel, selected             lipgloss.Style
}

func newStyles() styles {
	accent := lipgloss.Color("#7D56F4")
	muted := lipgloss.Color("#7C7C7C")
	return styles{
		title:        lipgloss.NewStyle().Bold(true).Foreground(accent),
		subtitle:     lipgloss.NewStyle().Foreground(muted),
		accent:       lipgloss.NewStyle().Foreground(accent),
		muted:        lipgloss.NewStyle().Foreground(muted),
		errorText:    lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F")),
		panel:        lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(muted).Padding(0, 1),
		focusedPanel: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1),
		selected:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(accent),
	}
}
