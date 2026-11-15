package tui

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/ih0r-d/hostup/src/internal/tui/palette"
)

func TestFixedPaletteAndFullViewport(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256} {
		for _, size := range [][2]int{{140, 40}, {110, 28}, {96, 22}, {44, 18}, {24, 12}, {12, 4}} {
			for _, page := range []section{overview, services, vpn, systemSetup} {
				m, _ := testModel(t)
				m.noColor = false
				m.Update(tea.ColorProfileMsg{Profile: profile})
				m.Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				m.selected = page
				command := m.openSection()
				if page == overview || page == services {
					deliver(m, command)
				}
				m.Update(tea.BackgroundColorMsg{Color: color.Black})
				dark := m.View().Content
				m.Update(tea.BackgroundColorMsg{Color: color.White})
				light := m.View().Content
				if dark != light {
					t.Fatal("terminal appearance changed the application palette")
				}
				lines := uv.NewStyledString(light).Lines(ansi.GraphemeWidth)
				if len(lines) != size[1] {
					t.Fatalf("page %v height %d want %d", page, len(lines), size[1])
				}
				for y, line := range lines {
					if len(line) != size[0] {
						t.Fatalf("row %d width %d want %d", y, len(line), size[0])
					}
					for x, cell := range line {
						if cell.Width > 0 && cell.Style.Bg == nil {
							t.Fatalf("unpainted cell %d,%d on %s at %v profile %v", x, y, sectionNames[page], size, profile)
						}
					}
				}
				if color.RGBAModel.Convert(lines[size[1]-1][size[0]-1].Style.Bg) != color.RGBAModel.Convert(palette.Color(profile, palette.Canvas)) {
					t.Fatal("bottom-right viewport is not canvas")
				}
				m.close(nil)
			}
		}
	}
}

func TestSidebarHighlightDoesNotSpill(t *testing.T) {
	m, _ := testModel(t)
	m.noColor = false
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 32})
	lines := uv.NewStyledString(m.View().Content).Lines(ansi.GraphemeWidth)
	for y, line := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if start := strings.Index(line, "> 01 Overview"); start >= 0 {
			for x := start; x < start+railWidth-2; x++ {
				if color.RGBAModel.Convert(lines[y][x].Style.Bg) != color.RGBAModel.Convert(palette.Color(m.profile, palette.Selected)) {
					t.Fatalf("selected background misaligned at %d,%d", x, y)
				}
			}
			if color.RGBAModel.Convert(lines[y][start+railWidth].Style.Bg) == color.RGBAModel.Convert(palette.Color(m.profile, palette.Selected)) {
				t.Fatal("highlight overlaps content gutter")
			}
			return
		}
	}
	t.Fatal("selected navigation entry missing")
}
