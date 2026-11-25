package look

import (
	"image/color"

	"github.com/marrasen/gunim/theme"

	"github.com/marrasen/kakel/vt"
)

// filesOf is nothing in a browser, which has no file manager: naming its
// colours would put all of it, and every kind of image and archive it
// reads, in what a guest downloads.
func filesOf(vt.Palette, color.NRGBA) []theme.Entry { return nil }
