package setupwizard

import (
	"image/color"
	"image/png"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/ih0r-d/hostup/assets"
)

// Render the existing PNG into a small 9×3 cell mark using half blocks. This
// needs no image protocol, external program, or replacement logo. Call only on
// resize/theme changes; rendering the view itself never decodes image data.
func renderLogo(dark bool) string {
	file, err := assets.OpenLogo()
	if err != nil {
		return ">_"
	}
	defer file.Close()
	logo, err := png.Decode(file)
	if err != nil {
		return ">_"
	}
	background := color.RGBA{R: 0xF1, G: 0xF5, B: 0xF9, A: 0xFF}
	if dark {
		background = color.RGBA{R: 0x0D, G: 0x19, B: 0x29, A: 0xFF}
	}
	// Sparse area sampling preserves the cyan/green silhouette without scaling
	// an entire image buffer. Source RGBA values are already alpha-premultiplied.
	sample := func(x, y int) color.RGBA {
		var red, green, blue uint32
		bounds := logo.Bounds()
		for dy := 0; dy < 4; dy++ {
			for dx := 0; dx < 4; dx++ {
				px := bounds.Min.X + (x*8+dx*2+1)*bounds.Dx()/(9*8)
				py := bounds.Min.Y + (y*8+dy*2+1)*bounds.Dy()/(6*8)
				r, g, b, a := logo.At(px, py).RGBA()
				red += r + uint32(background.R)*(65535-a)/255
				green += g + uint32(background.G)*(65535-a)/255
				blue += b + uint32(background.B)*(65535-a)/255
			}
		}
		return color.RGBA{R: uint8(red / (16 * 257)), G: uint8(green / (16 * 257)), B: uint8(blue / (16 * 257)), A: 255}
	}
	var lines []string
	for y := 0; y < 6; y += 2 {
		var line strings.Builder
		for x := 0; x < 9; x++ {
			line.WriteString(lipgloss.NewStyle().Foreground(sample(x, y)).Background(sample(x, y+1)).Render("▀"))
		}
		lines = append(lines, line.String())
	}
	return strings.Join(lines, "\n")
}

func (m *model) prepareLogo() {
	if !m.noColor && m.width >= 70 && m.height >= 24 && m.logo == "" {
		m.logo = renderLogo(m.dark)
	}
}
