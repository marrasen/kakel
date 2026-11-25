package guestlook

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"maps"
	"strings"
	"unicode/utf8"

	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/themes"
)

// The most of a theme there is, at either end.
const (
	mostTheme = 128 << 10
	mostFont  = 100
)

// LeastFontSize and MostFontSize are the smallest and largest size of the
// terminals' text.
const LeastFontSize, MostFontSize float32 = 8, 96

// Theme is how the host's window is drawn.
type Theme struct {
	// Look is the theme, made: the window's own colours, sizes and
	// motion, with the theme editor's changes, those of the panes, and
	// the terminals' palette. As ReadTheme made it, the editor's changes
	// are of colours alone, and are for the panes and their tabs: a page
	// draws what it says itself in Look.Plain, where that is Readable.
	Look look.Themed

	// FontSize is the size of the terminals' text, in logical pixels. A
	// page that scales the text to fit has it for the text of its own.
	FontSize float32

	// Font is the family the terminals are drawn in, for a page that has
	// it. Most have not.
	Font string
}

// Dark reports whether the theme is drawn on a dark ground.
func (t Theme) Dark() bool {
	ground := t.Look.Palette.BG
	return grid.Contrast(color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, ground) >= grid.Contrast(color.RGBA{A: 0xff}, ground)
}

// written is a Theme as it is sent: the theme it was made from, which
// makes the same at the other end, the two being one build. That is much
// less than what it makes, and it is sent with every state.
type written struct {
	Theme    themes.Theme    `json:"theme"`
	Edits    json.RawMessage `json:"edits,omitempty"`
	FontSize float32         `json:"fontSize,omitempty"`
	Font     string          `json:"font,omitempty"`
}

// Bytes writes the theme down.
func (t Theme) Bytes() (json.RawMessage, error) {
	if t.Look.Source.Name == "" && len(t.Look.Source.ANSI) == 0 {
		return nil, errors.New("there is no theme to write down")
	}
	raw, err := json.Marshal(written{Theme: t.Look.Source, Edits: t.Look.Source.Edits, FontSize: t.FontSize, Font: t.Font})
	if err != nil {
		return nil, fmt.Errorf("write the theme: %w", err)
	}
	if len(raw) > mostTheme {
		return nil, fmt.Errorf("the theme is %d bytes, over the %d it may be", len(raw), mostTheme)
	}
	return raw, nil
}

// pageTokens begin the names of what a page draws itself with.
const pageTokens = "kakel.page."

// editsFor is what a page keeps of the theme editor's changes: colours,
// but for the page's own. They come from somebody else, and any size, room
// or motion there is could be made one that hides what the page says.
func editsFor(raw json.RawMessage) json.RawMessage {
	var all map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &all) != nil {
		return nil
	}
	maps.DeleteFunc(all, func(key string, v json.RawMessage) bool {
		info, ok := theme.Lookup(key)
		// A colour is written "#rrggbbaa" at most.
		return !ok || len(v) == 0 || v[0] != '"' || len(v) > 11 || info.Kind != theme.KindColor && info.Kind != theme.KindForeground || strings.HasPrefix(key, pageTokens)
	})
	if len(all) == 0 {
		return nil
	}
	out, err := json.Marshal(all)
	if err != nil {
		return nil
	}
	return out
}

// leastContrast is the least the words of a page contrast with what they
// are on for it to be read, and leastButton those of the button that
// stands out, as kakel's own themes have it.
const leastContrast, leastButton = 3, 2

// Readable reports whether a page in Look.Plain can be read: that what is
// written in it stands out from what it is written on.
func (t Theme) Readable() bool {
	th := t.Look.Plain
	on := func(ink, fill theme.Token[color.NRGBA], least float64) bool {
		// Over the ground, where a fill is not a whole one.
		ground := over(fill.In(th), widget.Background.In(th))
		return grid.Contrast(over(ink.In(th), color.NRGBA(ground)), ground) >= least
	}
	for _, fill := range []theme.Token[color.NRGBA]{widget.Background, widget.MenubarFill, widget.CardFill, widget.DialogFill, widget.FieldFill, widget.ButtonFill, look.TermBackground} {
		if !on(widget.Ink, fill, leastContrast) {
			return false
		}
	}
	return on(widget.ButtonPrimaryInk, widget.ButtonPrimaryFill, leastButton)
}

// over is c on ground.
func over(c, ground color.NRGBA) color.RGBA {
	mix := func(a, b uint8) uint8 { return uint8((int(a)*int(c.A) + int(b)*(255-int(c.A))) / 255) }
	return color.RGBA{R: mix(c.R, ground.R), G: mix(c.G, ground.G), B: mix(c.B, ground.B), A: 0xff}
}

// ReadTheme reads what Theme.Bytes wrote.
func ReadTheme(raw json.RawMessage) (Theme, error) {
	if len(raw) > mostTheme {
		return Theme{}, fmt.Errorf("the theme is %d bytes, over the %d it may be", len(raw), mostTheme)
	}
	var w written
	if err := json.Unmarshal(raw, &w); err != nil {
		return Theme{}, fmt.Errorf("read the theme: %w", err)
	}
	w.Theme.Edits = editsFor(w.Edits)
	made, err := look.Of(w.Theme)
	if err != nil {
		return Theme{}, fmt.Errorf("read the theme: %w", err)
	}
	if utf8.RuneCountInString(w.Font) > mostFont {
		w.Font = ""
	}
	if !(w.FontSize >= LeastFontSize) {
		w.FontSize = LeastFontSize
	}
	return Theme{Look: made, FontSize: min(w.FontSize, MostFontSize), Font: w.Font}, nil
}
