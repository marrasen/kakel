package grid

import (
	"image/color"
	"math"
)

// Blend mixes two colours, at/of the way from the first to the second.
// Every channel is mixed, alpha included.
//
// An of of zero or less is the first colour: a gradient over no distance
// has only one end.
func Blend(from, to color.RGBA, at, of int) color.RGBA {
	if of <= 0 {
		return from
	}
	part := func(a, b uint8) uint8 { return uint8(int(a) + floorDiv((int(b)-int(a))*at, of)) }
	return color.RGBA{
		R: part(from.R, to.R),
		G: part(from.G, to.G),
		B: part(from.B, to.B),
		A: part(from.A, to.A),
	}
}

// floorDiv divides rounding down, where Go's own division rounds towards
// zero. A blend towards a darker colour has a negative numerator, and
// rounding the two ways apart moves such a cell by one.
func floorDiv(n, d int) int {
	q := n / d
	if n%d != 0 && (n < 0) != (d < 0) {
		q--
	}
	return q
}

// Contrast is the WCAG contrast ratio between two colours: 1 for a
// colour against itself, and 21 for black against white.
//
// Text is readable from about 4.5, and a change of ground reads from
// about 1.5. Alpha is ignored, so a colour that is drawn part way
// through another has to be blended first.
func Contrast(a, b color.RGBA) float64 {
	high, low := luminance(a), luminance(b)
	if high < low {
		high, low = low, high
	}
	return (high + 0.05) / (low + 0.05)
}

// luminance is the WCAG relative luminance of an sRGB colour, from 0 for
// black to 1 for white.
func luminance(c color.RGBA) float64 {
	return 0.2126*linear[c.R] + 0.7152*linear[c.G] + 0.0722*linear[c.B]
}

// linear is each 8-bit sRGB channel value as linear light, worked out
// once: luminance is asked of every cell drawn.
var linear = func() (out [256]float64) {
	for i := range out {
		s := float64(i) / 255
		if s <= 0.03928 {
			out[i] = s / 12.92
		} else {
			out[i] = math.Pow((s+0.055)/1.055, 2.4)
		}
	}
	return out
}()

// Readable returns fg, made to contrast with bg by ratio at least where
// it does not: lighter on a dark background, darker on a light one, by
// as little as it takes. A ratio neither way reaches gives whichever of
// white and black contrasts more. Its hue goes toward white or black
// with it, so blue text stays bluish, as a terminal with a minimum
// contrast does.
func Readable(fg, bg color.RGBA, ratio float64) color.RGBA {
	if Contrast(fg, bg) >= ratio {
		return fg
	}
	white, black := color.RGBA{0xff, 0xff, 0xff, fg.A}, color.RGBA{0, 0, 0, fg.A}
	toward := func(end color.RGBA) (color.RGBA, bool) {
		if Contrast(end, bg) < ratio {
			return end, false
		}
		// The least step toward end that reaches the ratio.
		lo, hi := 0, 256
		for lo < hi {
			mid := (lo + hi) / 2
			if Contrast(Blend(fg, end, mid, 256), bg) >= ratio {
				hi = mid
			} else {
				lo = mid + 1
			}
		}
		return Blend(fg, end, lo, 256), true
	}
	first, second := white, black
	if luminance(bg) > 0.5 {
		first, second = black, white
	}
	if c, ok := toward(first); ok {
		return c
	}
	if c, ok := toward(second); ok {
		return c
	}
	if Contrast(white, bg) >= Contrast(black, bg) {
		return white
	}
	return black
}
