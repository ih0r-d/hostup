package setupwizard

import (
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/ih0r-d/hostup/src/internal/tui/palette"
)

// The logo's cyan terminal and green upward stroke become focus and success
// accents. Keep backgrounds native and retain text/shape cues in monochrome.
type theme struct {
	accent, title, muted, success, warning               lipgloss.Style
	panel, action, disabled                              lipgloss.Style
	rail, selected, row, ready, tile, activeTile, footer lipgloss.Style
}

func newTheme(noColor bool, profile colorprofile.Profile) theme {
	t := theme{
		title: lipgloss.NewStyle().Bold(true), accent: lipgloss.NewStyle().Bold(true),
		success: lipgloss.NewStyle().Bold(true), warning: lipgloss.NewStyle().Bold(true),
		panel:    lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2),
		action:   lipgloss.NewStyle().Bold(true),
		rail:     lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).Padding(1),
		selected: lipgloss.NewStyle().Bold(true).Padding(0, 1),
		row:      lipgloss.NewStyle().Padding(0, 1), ready: lipgloss.NewStyle().Bold(true).Padding(0, 1),
		tile:       lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
		activeTile: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Bold(true),
		footer:     lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1),
	}
	if noColor || profile <= colorprofile.ASCII {
		t.activeTile = t.activeTile.Border(lipgloss.ThickBorder())
		return t
	}
	cyan := palette.Color(profile, palette.Cyan)
	green := palette.Color(profile, palette.Success)
	neutral := palette.Color(profile, palette.Secondary)
	text := palette.Color(profile, palette.Text)
	canvas := palette.Color(profile, palette.Canvas)
	panel := palette.Color(profile, palette.Panel)
	border := palette.Color(profile, palette.Selected)
	t.accent = t.accent.Foreground(cyan)
	t.success = t.success.Foreground(green)
	t.warning = t.warning.Foreground(palette.Color(profile, palette.Warning))
	t.muted = lipgloss.NewStyle().Foreground(neutral)
	t.disabled = t.muted
	t.panel = t.panel.BorderForeground(border).Background(panel).Foreground(text)
	t.row = t.row.Background(border).Foreground(text)
	t.ready = t.ready.Background(border).Foreground(green)
	t.rail = t.rail.Background(palette.Color(profile, palette.Sidebar)).Foreground(text).BorderForeground(border)
	t.selected = t.selected.Foreground(cyan).Background(border)
	t.tile = t.tile.Background(panel).Foreground(neutral).BorderForeground(border)
	t.activeTile = t.activeTile.Foreground(cyan).Background(border).BorderForeground(cyan)
	t.footer = t.footer.Background(panel).Foreground(text).BorderForeground(border)
	t.action = t.action.Foreground(canvas).Background(cyan)
	return t
}

func (t theme) button(label string, enabled bool) string {
	style := t.disabled
	if enabled {
		style = t.action
	}
	return style.Render("[ " + label + " ]")
}
