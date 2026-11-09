package term

import (
	"strings"
	"testing"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
)

// A pane split in two and then given its room back shows every line whole.
func TestAPaneGivenItsRoomBackShowsItsLinesWhole(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "0123456789abcdefghij\r\nPS> ")

	term.Layout(ui.Size{Cols: 10, Rows: 4})
	g := draw(term, 10, 4)
	for y, want := range []string{"0123456789", "abcdefghij", "PS>"} {
		if got := rowText(g, y); got != want {
			t.Errorf("narrowed, row %d is %q, want %q", y, got, want)
		}
	}

	term.Layout(ui.Size{Cols: 20, Rows: 4})
	g = draw(term, 20, 4)
	for y, want := range []string{"0123456789abcdefghij", "PS>"} {
		if got := rowText(g, y); got != want {
			t.Errorf("widened again, row %d is %q, want %q", y, got, want)
		}
	}
}

// A selection stays on its text when a resize re-wraps the lines above it.
func TestASelectionStaysOnItsTextThroughAResize(t *testing.T) {
	term, f := newTestTerm(t, 20, 6, Config{WriteClipboard: func(string) {}})
	f.feed(t, term, "0123456789abcdefghij\r\nkeep me\r\n$ ")
	draw(term, 20, 6)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Row: 1})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 6, Row: 1})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 6, Row: 1})
	if got := term.SelectionText(); got != "keep me" {
		t.Fatalf("selected %q", got)
	}

	term.Layout(ui.Size{Cols: 10, Rows: 6})

	if got := term.SelectionText(); got != "keep me" {
		t.Errorf("after narrowing, the selection reads %q, want %q", got, "keep me")
	}
}

// A selection on a full-screen program's screen stays on its rows when the ordinary screen under it is re-wrapped.
func TestASelectionOnTheAlternateScreenStaysOnItsRows(t *testing.T) {
	term, f := newTestTerm(t, 20, 6, Config{WriteClipboard: func(string) {}})
	f.feed(t, term, strings.Repeat("0123456789abcdefghij\r\n", 6)+"$ \x1b[?1049h\x1b[Hone\r\ntwo\r\nthree")
	draw(term, 20, 6)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Row: 0})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 2, Row: 0})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: 2, Row: 0})
	if got := term.SelectionText(); got != "one" {
		t.Fatalf("selected %q", got)
	}

	term.Layout(ui.Size{Cols: 10, Rows: 6})

	if got := term.SelectionText(); got != "one" {
		t.Errorf("after narrowing, the selection reads %q, want %q", got, "one")
	}
}
