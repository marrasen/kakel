package guestlook

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"unicode/utf8"

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
	// the terminals' palette.
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

// ReadTheme reads what Theme.Bytes wrote.
func ReadTheme(raw json.RawMessage) (Theme, error) {
	if len(raw) > mostTheme {
		return Theme{}, fmt.Errorf("the theme is %d bytes, over the %d it may be", len(raw), mostTheme)
	}
	var w written
	if err := json.Unmarshal(raw, &w); err != nil {
		return Theme{}, fmt.Errorf("read the theme: %w", err)
	}
	w.Theme.Edits = w.Edits
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
