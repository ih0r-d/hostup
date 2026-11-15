package setupwizard

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/tui/palette"
)

const (
	minimumWidth  = 32
	minimumHeight = 14
	maximumWidth  = 76
	compactWidth  = 68
	railWidth     = 21
	railGap       = 3
)

func (m *model) wide() bool { return m.width >= 110 && m.height >= 28 }

func (m *model) panelWidth() int {
	if m.wide() {
		return maximumWidth
	}
	return min(compactWidth, m.width-4)
}

func (m *model) contentWidth() int { return max(1, m.panelWidth()-6) }

// Keep the action bar outside the scrollable panel. Collapse the navigation
// rail and step tiles rather than shrinking the actual configuration fields.
func (m *model) layout(body string, t theme) string {
	width := m.panelWidth()
	header, steps := m.header(t, width), m.steps(t, width)
	panelStyle := t.panel
	if m.height < 28 {
		panelStyle = panelStyle.Padding(0, 2)
	}
	lines := strings.Split(lipgloss.Wrap(body, m.contentWidth(), " "), "\n")
	footer := m.footer(t, width, false)
	overhead := lipgloss.Height(header) + lipgloss.Height(steps) + 1 + panelStyle.GetVerticalFrameSize()
	available := max(1, m.height-overhead-lipgloss.Height(footer))
	if len(lines) > available {
		footer = m.footer(t, width, true)
		available = max(1, m.height-overhead-lipgloss.Height(footer))
	}
	start := min(m.scroll, max(0, len(lines)-available))
	panel := panelStyle.Width(width).Render(strings.Join(lines[start:min(len(lines), start+available)], "\n"))
	panel = palette.Surface(panel, m.profile, palette.Panel, m.noColor)
	frame := header + "\n" + steps + "\n\n" + panel + "\n" + footer
	if m.wide() {
		frame = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar(t, lipgloss.Height(frame)), strings.Repeat(" ", railGap), frame)
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, frame)
}

func (m *model) header(t theme, width int) string {
	if m.wide() {
		return t.title.Render("System Setup") + "\n" + t.muted.Render("Hostname and timezone")
	}
	brand := t.accent.Render(">_ HOSTUP")
	if m.logo != "" && !m.noColor && m.width >= 70 && m.height >= 24 {
		brand = lipgloss.JoinHorizontal(lipgloss.Center, m.logo, " "+t.accent.Render("HOSTUP"))
	}
	title := t.title.Render("System Setup")
	gap := strings.Repeat(" ", max(1, width-ansi.StringWidth(brand)-ansi.StringWidth(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, brand, gap, title)
}

func (m *model) sidebar(t theme, height int) string {
	inner := railWidth - t.rail.GetHorizontalFrameSize()
	brand := t.accent.Render(">_ HOSTUP")
	if m.logo != "" && !m.noColor {
		brand = lipgloss.JoinHorizontal(lipgloss.Center, m.logo, " "+t.accent.Render("HOSTUP"))
	}
	top := brand + "\n\n" + t.muted.Render("LINUX MANAGEMENT") + "\n\n" + t.selected.Render("02 System Setup")
	bottom := t.muted.Render("LOCAL HOST\n" + ansi.Truncate(m.platform, inner, "…"))
	gap := max(0, height-t.rail.GetVerticalFrameSize()-lipgloss.Height(top)-lipgloss.Height(bottom))
	return palette.Surface(t.rail.Width(railWidth).Height(height).Render(top+strings.Repeat("\n", gap+1)+bottom), m.profile, palette.Sidebar, m.noColor)
}

func (m *model) steps(t theme, width int) string {
	index := min(int(m.page), 3)
	names := []string{"Welcome", "Configure", "Review", "Apply"}
	if width < 54 {
		return t.accent.Render(fmt.Sprintf("[%d/4] %s", index+1, names[index]))
	}
	items := make([]string, len(names))
	tiles := m.height >= 28 && width >= compactWidth
	for i, name := range names {
		label := fmt.Sprintf("%02d %s", i+1, name)
		if tiles {
			tileWidth := (width - 3) / 4
			if i == len(names)-1 {
				tileWidth += (width - 3) % 4
			}
			style := t.tile
			if i == index {
				style = t.activeTile
			}
			background := palette.Panel
			if i == index {
				background = palette.Selected
			}
			items[i] = palette.Surface(style.Width(tileWidth).Render(label), m.profile, background, m.noColor)
		} else if i == index {
			items[i] = t.accent.Render("[" + label + "]")
		} else {
			items[i] = t.muted.Render(label)
		}
	}
	if tiles {
		return lipgloss.JoinHorizontal(lipgloss.Top, items[0], " ", items[1], " ", items[2], " ", items[3])
	}
	return strings.Join(items, "  ")
}

func (m *model) footer(t theme, width int, scrollable bool) string {
	if m.height < 20 || width < 46 {
		return t.muted.Render(lipgloss.Wrap(m.controls(scrollable), width, " "))
	}
	primary, enabled := "enter Continue", true
	switch m.page {
	case welcome:
		if !m.loaded {
			primary, enabled = "Reading…", false
		}
		if m.err != nil {
			primary = "enter Close"
		}
	case configure:
		primary = "enter Review"
		enabled = !m.planning && m.inputs[0].Err == nil && m.inputs[1].Err == nil
		if m.planning {
			primary = "Reading…"
		}
	case review:
		primary = "space Confirm"
		if m.confirmed {
			primary = "enter Apply"
		}
		if !m.plan.HasChanges() {
			primary = "enter Done"
		}
	case applying:
		primary, enabled = "ctrl+c Cancel", !m.cancelling
		if m.cancelling {
			primary = "Cancelling…"
		}
	case results:
		primary = "enter Done"
	}
	back := ""
	if m.page == configure || m.page == review {
		back = t.muted.Render("esc Back")
	}
	bar := t.footer.Width(width).Render(aligned(back, t.button(primary, enabled), width-t.footer.GetHorizontalFrameSize()))
	bar = palette.Surface(bar, m.profile, palette.Panel, m.noColor)
	var hints []string
	if m.page == configure {
		hints = append(hints, "tab field")
	}
	if m.page < applying {
		hints = append(hints, "ctrl+c cancel")
	}
	if m.err != nil && m.page != configure {
		hints = append(hints, "d details")
	}
	if scrollable {
		hints = append(hints, "pgup/pgdn scroll")
	}
	if len(hints) > 0 {
		bar += "\n" + t.muted.Render(lipgloss.Wrap(strings.Join(hints, " · "), width, " "))
	}
	return bar
}

func aligned(left, right string, width int) string {
	return left + strings.Repeat(" ", max(1, width-ansi.StringWidth(left)-ansi.StringWidth(right))) + right
}

// Focus follows wrapped field height, including inline validation messages.
func (m *model) focusOffset() int {
	if m.focus == 0 {
		return 0
	}
	return lipgloss.Height(lipgloss.Wrap(m.fieldView(0), m.contentWidth(), " ")) + 1
}
