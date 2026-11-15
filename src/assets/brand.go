// Package assets provides Hostup's original, embedded brand assets.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed hostup-icon.png
var branding embed.FS

// OpenLogo returns the original PNG without depending on the working directory.
func OpenLogo() (fs.File, error) { return branding.Open("hostup-icon.png") }
