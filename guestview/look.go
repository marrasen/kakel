package guestview

import (
	"image/color"

	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/look"
)

// Colours of the page's own words: those that matter less than what is
// beside them, and that the guest may type, and may only watch. They are
// worked out from the theme, so they can be read on it.
var (
	quietInk = theme.Color("kakel.page.quiet", color.NRGBA{R: 0x9a, G: 0xa3, B: 0xb6, A: 0xff})
	mayInk   = theme.Color("kakel.page.may", color.NRGBA{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff})
	watchInk = theme.Color("kakel.page.watch", color.NRGBA{R: 0xe6, G: 0xb8, B: 0x55, A: 0xff})
)

// leastContrast is the least the page's own words contrast with what they
// are on.
const leastContrast = 4.5

// touchLeast is how tall at least what a finger touches is.
const touchLeast = 44

// readable is ink, as far from its own colour as it has to be to be read
// on each of grounds.
func readable(ink color.NRGBA, grounds ...color.NRGBA) color.NRGBA {
	rgba := func(c color.NRGBA) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }
	out := rgba(ink)
	// Twice round, where the one moves it nearer the other.
	for range 2 {
		for _, g := range grounds {
			out = grid.Readable(out, rgba(g), leastContrast)
		}
	}
	return color.NRGBA(out)
}

// tinted is ground with ink over it as a badge's fill has it.
func tinted(ink, ground color.NRGBA, by float32) color.NRGBA {
	mix := func(a, b uint8) uint8 { return uint8(float32(a)*by + float32(b)*(1-by) + 0.5) }
	return color.NRGBA{R: mix(ink.R, ground.R), G: mix(ink.G, ground.G), B: mix(ink.B, ground.B), A: 0xff}
}

// Own is what the page says itself drawn in th, a theme with none of a
// host's changes, and with touch, with room for a finger.
func Own(th theme.Theme, touch bool) theme.Theme {
	bar, tint := widget.MenubarFill.In(th), widget.BadgeTint.In(th)
	// on is ink as a badge has it, on its own tint.
	on := func(ink color.NRGBA) color.NRGBA {
		for range 3 {
			ink = readable(ink, tinted(ink, bar, tint))
		}
		return ink
	}
	th = th.With(
		theme.Set(quietInk, readable(look.Faint.In(th), widget.Background.In(th), widget.DialogFill.In(th), bar)),
		theme.Set(mayInk, on(widget.Accent.In(th))),
		theme.Set(watchInk, on(look.Guest.In(th))),
	)
	if touch {
		th = forFingers(th)
	}
	return th
}

// forFingers is th with room for a finger in what is touched.
func forFingers(th theme.Theme) theme.Theme {
	for _, tok := range []theme.Token[float32]{widget.ButtonHeight, widget.FieldHeight, widget.ControlHeight, widget.MenuRowHeight, dock.BarHeight} {
		th = th.With(theme.Set(tok, max(tok.In(th), touchLeast)))
	}
	return th.With(theme.Set(barHeight, max(barHeight.In(th), touchLeast+8)))
}

// still is th with nothing in it animating.
func still(th theme.Theme) theme.Theme { return th.With(theme.Set(theme.Scale, 0)) }

// Still is th with nothing in it animating.
func Still(th look.Themed) look.Themed {
	th.Theme, th.Content = still(th.Theme), still(th.Content)
	return th
}
