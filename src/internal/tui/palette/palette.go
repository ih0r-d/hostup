// Package palette contains Hostup's fixed brand colors and viewport surface.
package palette

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

const (
	Canvas    = "#09111E"
	Sidebar   = "#0D1929"
	Panel     = "#101E30"
	Text      = "#E2E8F0"
	Secondary = "#94A3B8"
	Cyan      = "#22D3EE"
	Selected  = "#153E50"
	Success   = "#4ADE80"
	Warning   = "#FBBF24"
)

func Color(profile colorprofile.Profile, hex string) color.Color {
	return profile.Convert(lipgloss.Color(hex))
}

// Viewport fills unused cells as well as content. Lip Gloss preserves nested
// panel/selection backgrounds without changing terminal configuration.
func Viewport(content string, width, height int, profile colorprofile.Profile, noColor bool) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	lines = lines[:min(height, len(lines))]
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	surface := lipgloss.NewStyle().Width(width).Height(height)
	if noColor || profile <= colorprofile.ASCII {
		return surface.Render(ansi.Strip(strings.Join(lines, "\n")))
	}
	return paint(strings.Join(lines, "\n"), width, height, profile, Canvas)
}

// Surface gives nested text the panel/sidebar background even after an inner
// style resets SGR. The public Lip Gloss cell canvas preserves explicit colors.
func Surface(content string, profile colorprofile.Profile, background string, noColor bool) string {
	if noColor || profile <= colorprofile.ASCII {
		return ansi.Strip(content)
	}
	return paint(content, lipgloss.Width(content), lipgloss.Height(content), profile, background)
}

func paint(content string, width, height int, profile colorprofile.Profile, background string) string {
	canvas := lipgloss.NewCanvas(width, height).Compose(lipgloss.NewLayer(content))
	bg, fg := Color(profile, background), Color(profile, Text)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			cell := canvas.CellAt(x, y).Clone()
			if cell.Width == 0 {
				continue
			}
			if cell.Style.Bg == nil {
				cell.Style.Bg = bg
			}
			if cell.Style.Fg == nil {
				cell.Style.Fg = fg
			}
			canvas.SetCell(x, y, cell)
		}
	}
	return canvas.Render()
}
