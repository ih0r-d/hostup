package tui

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/ih0r-d/hostup/src/internal/tui/palette"
)

type theme struct {
	text, title, accent, muted, success, warning, panel, rail, selected lipgloss.Style
}

func (m *model) monochrome() bool {
	return m.noColor || m.profile == colorprofile.ASCII || m.profile == colorprofile.NoTTY
}

func (m *model) theme() theme {
	t := theme{panel: lipgloss.NewStyle().Padding(1, 2), rail: lipgloss.NewStyle().Padding(1)}
	if m.monochrome() {
		return t
	}
	text := palette.Color(m.profile, palette.Text)
	cyan := palette.Color(m.profile, palette.Cyan)
	t.text = lipgloss.NewStyle().Foreground(text)
	t.title = t.text.Bold(true)
	t.accent = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	t.muted = lipgloss.NewStyle().Foreground(palette.Color(m.profile, palette.Secondary))
	t.success = lipgloss.NewStyle().Foreground(palette.Color(m.profile, palette.Success))
	t.warning = lipgloss.NewStyle().Foreground(palette.Color(m.profile, palette.Warning))
	t.panel = t.panel.Background(palette.Color(m.profile, palette.Panel)).Foreground(text)
	t.rail = t.rail.Background(palette.Color(m.profile, palette.Sidebar)).Foreground(text)
	t.selected = t.accent.Background(palette.Color(m.profile, palette.Selected))
	return t
}

func (t theme) serviceState(state string) string {
	switch state {
	case "active":
		return t.success.Render(safe(state))
	case "failed":
		return t.warning.Render(safe(state))
	default:
		return t.text.Render(safe(state))
	}
}
