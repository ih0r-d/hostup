package palette

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

func TestViewportBackgroundEveryCell(t *testing.T) {
	for _, profile := range []colorprofile.Profile{colorprofile.TrueColor, colorprofile.ANSI256, colorprofile.ANSI, colorprofile.ASCII} {
		for _, noColor := range []bool{false, true} {
			frame := lipgloss.NewStyle().Background(Color(profile, Panel)).Render("panel") + "\n" + lipgloss.NewStyle().Foreground(Color(profile, Cyan)).Render("text")
			content := Viewport(frame, 40, 10, profile, noColor)
			lines := uv.NewStyledString(content).Lines(ansi.GraphemeWidth)
			if len(lines) != 10 {
				t.Fatal("viewport height")
			}
			for y, line := range lines {
				if len(line) != 40 {
					t.Fatalf("row %d width %d", y, len(line))
				}
				for x, cell := range line {
					if noColor || profile <= colorprofile.ASCII {
						if cell.Style.Bg != nil {
							t.Fatal("color in monochrome")
						}
						continue
					}
					if cell.Style.Bg == nil {
						t.Fatalf("unpainted cell %d,%d (%v)", x, y, profile)
					}
					if y == 9 && color.RGBAModel.Convert(cell.Style.Bg) != color.RGBAModel.Convert(Color(profile, Canvas)) {
						t.Fatal("unused space is not canvas")
					}
				}
			}
			if noColor && strings.Contains(content, "\x1b[") {
				t.Fatal("ANSI in NO_COLOR")
			}
		}
	}
}
