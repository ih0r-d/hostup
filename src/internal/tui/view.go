package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/tui/palette"
	setupwizard "github.com/ih0r-d/hostup/src/internal/tui/setup"
)

const railWidth = 23

func (m *model) wide() bool { return m.width >= 96 && m.height >= 22 }

func (m *model) contentSize() (int, int) {
	width := min(128, m.width) - 4
	if m.wide() {
		width -= railWidth + 2
	}
	return max(1, width), max(1, m.height-7)
}

func (m *model) prepareLogo() {
	if !m.monochrome() && m.profile >= colorprofile.ANSI256 && m.wide() && m.logo == "" {
		m.logo = setupwizard.RenderLogo()
	}
}

func (m *model) View() tea.View {
	if m.wizard != nil {
		view := m.wizard.View()
		view.Content = fit(view.Content, m.width, m.height)
		if m.monochrome() {
			view.Content = ansi.Strip(view.Content)
		}
		return view
	}
	var content string
	if m.width < 24 || m.height < 12 {
		content = fit("HOSTUP\nResize to 24 × 12\nCtrl+C exit", m.width, m.height)
	} else {
		content = m.layout()
	}
	view := tea.NewView(palette.Viewport(content, m.width, m.height, m.profile, m.monochrome()))
	view.AltScreen = true
	return view
}

func (m *model) layout() string {
	t := m.theme()
	width, height := m.contentSize()
	title := sectionNames[m.selected]
	caption := "Read-only system information"
	if !m.opened {
		caption = "Select a section"
	} else if m.selected == services {
		caption = "Systemd services · read only"
	} else if m.selected != overview {
		caption = "Planned module"
	}
	if !m.wide() {
		title = "HOSTUP / " + title
	}
	header := t.title.Render(title) + "\n" + t.muted.Render(caption) + "\n" + t.muted.Render(strings.Repeat("─", width))
	body := m.body(t, width, height)
	if !m.opened && !m.wide() {
		body = m.navigation(t, width, height)
	} else if m.selected != services || !m.opened || m.services.details {
		lines := strings.Split(lipgloss.Wrap(body, width, " "), "\n")
		start := min(m.scroll, max(0, len(lines)-height))
		body = strings.Join(lines[start:min(len(lines), start+height)], "\n")
	}
	body = lipgloss.NewStyle().Width(width).Height(height).Render(fit(body, width, height))
	footer := t.muted.Render(ansi.Truncate(m.hints(), width, "…"))
	main := header + "\n\n" + body + "\n" + footer
	if m.wide() {
		main = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(t, m.height), "  ", main)
	}
	frame := lipgloss.NewStyle().Padding(0, 2).Render(main)
	frame = fit(frame, min(128, m.width), m.height)
	return lipgloss.PlaceHorizontal(m.width, lipgloss.Center, frame)
}

func (m *model) sidebar(t theme, height int) string {
	inner := railWidth - 2
	brand := t.accent.Render("HOSTUP")
	if m.logo != "" && !m.monochrome() {
		brand = lipgloss.JoinHorizontal(lipgloss.Center, m.logo, " "+brand)
	}
	focus := "NAVIGATION"
	if m.opened {
		focus = "SECTIONS"
	}
	body := brand + "\n\n" + t.muted.Render("LINUX MANAGEMENT") + "\n\n" + t.muted.Render(focus) + "\n\n" + m.navigation(t, inner, 9)
	return palette.Surface(t.rail.Width(railWidth).Height(height).Render(fit(body, inner, height-2)), m.profile, palette.Sidebar, m.monochrome())
}

func (m *model) navigation(t theme, width, height int) string {
	start := max(0, int(m.selected)-height+1)
	var lines []string
	for i := start; i < min(len(sectionNames), start+height); i++ {
		marker, style := "  ", t.muted
		if section(i) == m.selected {
			marker, style = "> ", t.selected
		}
		line := fmt.Sprintf("%s%02d %s", marker, i+1, sectionNames[i])
		lines = append(lines, style.Width(width).Render(ansi.Truncate(line, width, "…")))
	}
	return strings.Join(lines, "\n")
}

func (m *model) body(t theme, width, height int) string {
	if !m.opened {
		return t.accent.Render(sectionNames[m.selected]) + "\n\n" + t.muted.Render("Enter to open")
	}
	switch m.selected {
	case overview:
		return m.overviewBody(t, width)
	case services:
		return m.servicesBody(t, width, height)
	default:
		return m.placeholderBody(t)
	}
}

func (m *model) maxScroll() int {
	width, height := m.contentSize()
	return max(0, lipgloss.Height(lipgloss.Wrap(m.body(m.theme(), width, height), width, " "))-height)
}

func (m *model) hints() string {
	width, _ := m.contentSize()
	if width < 42 {
		if !m.opened {
			return "↑↓/jk  enter  q"
		}
		if m.selected == services && !m.services.details && len(m.services.items) > 0 {
			return "↑↓/jk  enter  esc"
		}
		return "↑↓  esc  ^C"
	}
	if !m.opened {
		return "↑/↓ j/k select · enter open · q quit"
	}
	if m.selected == services {
		if m.services.details {
			return "↑/↓ scroll · esc list · ctrl+c exit"
		}
		if !m.services.loading && m.services.err == nil && len(m.services.items) > 0 {
			return "↑/↓ j/k select · enter details · esc back"
		}
	}
	return "↑/↓ scroll · esc sections · ctrl+c exit"
}

// Last-line defense for extreme resizes and long/untrusted host/unit names.
// The layout reserves space for controls before applying this viewport bound.
func fit(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	lines = lines[:min(height, len(lines))]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}

func safe(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, ansi.Strip(value))
}
