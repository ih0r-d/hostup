package setupwizard

import "charm.land/lipgloss/v2"

// The logo's cyan terminal and green upward stroke become focus and success
// accents. Keep backgrounds native and retain text/shape cues in monochrome.
type theme struct {
	accent, title, muted, success, warning                     lipgloss.Style
	panel, action, disabled                                    lipgloss.Style
	page, rail, selected, row, ready, tile, activeTile, footer lipgloss.Style
}

func newTheme(noColor, dark bool) theme {
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
	if noColor {
		t.activeTile = t.activeTile.Border(lipgloss.ThickBorder())
		return t
	}
	color := lipgloss.LightDark(dark)
	cyan := color(lipgloss.Color("#0E7490"), lipgloss.Color("#22D3EE"))
	green := color(lipgloss.Color("#15803D"), lipgloss.Color("#4ADE80"))
	neutral := color(lipgloss.Color("#475569"), lipgloss.Color("#94A3B8"))
	text := color(lipgloss.Color("#0F172A"), lipgloss.Color("#E2E8F0"))
	canvas := color(lipgloss.Color("#FFFFFF"), lipgloss.Color("#09111E"))
	panel := color(lipgloss.Color("#F8FAFC"), lipgloss.Color("#101E30"))
	border := color(lipgloss.Color("#CBD5E1"), lipgloss.Color("#263D53"))
	t.accent = t.accent.Foreground(cyan)
	t.success = t.success.Foreground(green)
	t.warning = t.warning.Foreground(color(lipgloss.Color("#B45309"), lipgloss.Color("#FBBF24")))
	t.muted = lipgloss.NewStyle().Foreground(neutral)
	t.disabled = t.muted
	t.page = lipgloss.NewStyle().Foreground(text).Background(canvas)
	t.panel = t.panel.BorderForeground(border).Background(panel).Foreground(text)
	t.row = t.row.Background(color(lipgloss.Color("#EFF6FF"), lipgloss.Color("#172A3C"))).Foreground(text)
	t.ready = t.ready.Background(color(lipgloss.Color("#DCFCE7"), lipgloss.Color("#123D34"))).Foreground(green)
	t.rail = t.rail.Background(color(lipgloss.Color("#F1F5F9"), lipgloss.Color("#0D1929"))).Foreground(text).BorderForeground(border)
	t.selected = t.selected.Foreground(cyan).Background(color(lipgloss.Color("#CFFAFE"), lipgloss.Color("#153E50")))
	t.tile = t.tile.Background(panel).Foreground(neutral).BorderForeground(border)
	t.activeTile = t.activeTile.Foreground(cyan).Background(color(lipgloss.Color("#CFFAFE"), lipgloss.Color("#153E50"))).BorderForeground(cyan)
	t.footer = t.footer.Background(panel).Foreground(text).BorderForeground(border)
	t.action = t.action.Foreground(lipgloss.Color("#082F49")).Background(lipgloss.Color("#67E8F9"))
	return t
}

func (t theme) button(label string, enabled bool) string {
	style := t.disabled
	if enabled {
		style = t.action
	}
	return style.Render("[ " + label + " ]")
}
