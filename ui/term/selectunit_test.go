package term

import (
	"testing"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/input"
)

// click presses and lets go at col,row as the clicks'th click of a
// quick run.
func click(t *testing.T, term *Terminal, col, row, clicks int) {
	t.Helper()
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: col, Row: row, Clicks: clicks})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseRelease, Button: input.MouseLeft, Col: col, Row: row})
}

// clicks clicks n times at col,row, as a double or triple click does.
func clicks(t *testing.T, term *Terminal, col, row, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		click(t, term, col, row, i)
	}
}

func TestDoubleClickSelectsTheWord(t *testing.T) {
	cases := []struct {
		name string
		col  int
		want string
	}{
		{"a plain word", 1, "echo"},
		{"a path, slashes and dot and all", 8, `C:\Users\me/x.go`},
		{"a run of one mark", 26, "=="},
		{"a mark on its own", 28, "("},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			term, f := newTestTerm(t, 40, 4, Config{})
			f.feed(t, term, `echo C:\Users\me/x.go    == (x`)
			draw(term, 40, 4)

			clicks(t, term, c.col, 0, 2)

			if got := term.SelectionText(); got != c.want {
				t.Errorf("double click at %d selected %q, want %q", c.col, got, c.want)
			}
		})
	}
}

// TestDoubleClickOnBlanksTakesTheRun checks a double click between two
// words picks the blanks there, which copy as nothing, as trailing
// blanks do.
func TestDoubleClickOnBlanksTakesTheRun(t *testing.T) {
	term, f := newTestTerm(t, 30, 4, Config{})
	f.feed(t, term, "one    two")
	draw(term, 30, 4)

	clicks(t, term, 5, 0, 2)
	term.GrowSelection(1, true)

	if got := term.g.Selection(); got.Anchor.X != 3 || got.Cursor.X != 6 {
		t.Errorf("selected %d to %d, want the blanks, 3 to 6", got.Anchor.X, got.Cursor.X)
	}
}

func TestDoubleClickOnAOneLetterWordKeepsIt(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "a b c")
	draw(term, 20, 4)

	clicks(t, term, 2, 0, 2)

	if got := term.SelectionText(); got != "b" {
		t.Errorf("selected %q, want %q: the release took a one-letter word for a click", got, "b")
	}
}

func TestTripleClickSelectsTheLine(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "first line\r\nsecond one here\r\nthird")
	draw(term, 20, 4)

	clicks(t, term, 3, 1, 3)

	if got := term.SelectionText(); got != "second one here" {
		t.Errorf("triple click selected %q, want the whole line", got)
	}
}

// TestTripleClickTakesAWrappedLineWhole checks a line the screen wrapped
// is selected whole from any of its rows, and copied as the one line it
// was written as.
func TestTripleClickTakesAWrappedLineWhole(t *testing.T) {
	term, f := newTestTerm(t, 10, 4, Config{})
	f.feed(t, term, "abcdefghij0123456789xyz\r\nnext")
	draw(term, 10, 4)

	clicks(t, term, 4, 1, 3)

	if got, want := term.SelectionText(), "abcdefghij0123456789xyz"; got != want {
		t.Errorf("triple click selected %q, want %q", got, want)
	}
}

func TestDoubleClickTakesAWordWrappedAcrossRows(t *testing.T) {
	term, f := newTestTerm(t, 10, 4, Config{})
	f.feed(t, term, "ls /home/someone/x y")
	draw(term, 10, 4)

	clicks(t, term, 2, 1, 2)

	if got, want := term.SelectionText(), "/home/someone/x"; got != want {
		t.Errorf("double click selected %q, want %q", got, want)
	}
}

func TestFourthClickStartsAgainFromCells(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "hello world")
	draw(term, 20, 4)

	clicks(t, term, 2, 0, 4)

	if got := term.SelectionText(); got != "" {
		t.Errorf("a fourth click left %q selected, want a plain click", got)
	}
}

func TestDragAfterADoubleClickGoesByWords(t *testing.T) {
	term, f := newTestTerm(t, 30, 4, Config{})
	f.feed(t, term, "one two three four")
	draw(term, 30, 4)

	click(t, term, 5, 0, 1)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 5, Clicks: 2})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 9})
	if got, want := term.SelectionText(), "two three"; got != want {
		t.Errorf("dragging right selected %q, want %q", got, want)
	}
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 1})
	if got, want := term.SelectionText(), "one two"; got != want {
		t.Errorf("dragging left selected %q, want %q: the word pressed on stays whole", got, want)
	}
}

func TestDragAfterATripleClickGoesByLines(t *testing.T) {
	term, f := newTestTerm(t, 20, 4, Config{})
	f.feed(t, term, "one\r\ntwo\r\nthree")
	draw(term, 20, 4)

	click(t, term, 1, 0, 1)
	click(t, term, 1, 0, 2)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 1, Clicks: 3})
	mouseTo(t, term, input.MouseEvent{Kind: input.MouseMove, Button: input.MouseLeft, Col: 0, Row: 1})

	if got, want := term.SelectionText(), "one\ntwo"; got != want {
		t.Errorf("dragging down selected %q, want %q", got, want)
	}
}

// TestPickedWordGrowsOutFromTheClick checks a double click's word does
// not show all at once: it grows from the cell clicked to its ends, the
// copy taking the whole word all along.
func TestPickedWordGrowsOutFromTheClick(t *testing.T) {
	term, f := newTestTerm(t, 30, 4, Config{})
	f.feed(t, term, "say abcdefghij now")
	draw(term, 30, 4)

	click(t, term, 9, 0, 1)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 9, Clicks: 2})

	if !term.SelectionGrowing() {
		t.Fatal("the word is not growing")
	}
	if got := term.g.Selection(); got != (grid.Selection{Anchor: grid.Point{X: 9}, Cursor: grid.Point{X: 9}, Active: true}) {
		t.Errorf("shown at first: %+v, want the cell clicked", got)
	}
	if got, want := term.SelectionText(), "abcdefghij"; got != want {
		t.Errorf("copy while growing takes %q, want %q", got, want)
	}

	term.GrowSelection(0.5, false)
	half := term.g.Selection()
	// Half of 5 cells back is 2.5, rounded out to 3.
	if half.Anchor.X != 6 || half.Cursor.X != 11 {
		t.Errorf("half grown shows %d to %d, want 6 to 11", half.Anchor.X, half.Cursor.X)
	}

	term.GrowSelection(1, false)
	if term.SelectionGrowing() {
		t.Error("still growing once grown")
	}
	if got := term.g.Selection(); got.Anchor.X != 4 || got.Cursor.X != 13 {
		t.Errorf("grown shows %d to %d, want the word, 4 to 13", got.Anchor.X, got.Cursor.X)
	}
}

// TestTripleClickGrowsFromTheWord checks the line grows out from the
// word the double click picked, not from the one cell again.
func TestTripleClickGrowsFromTheWord(t *testing.T) {
	term, f := newTestTerm(t, 30, 4, Config{})
	f.feed(t, term, "say abcdefghij now")
	draw(term, 30, 4)

	click(t, term, 9, 0, 1)
	click(t, term, 9, 0, 2)
	term.GrowSelection(1, true)
	mouseTo(t, term, input.MouseEvent{Kind: input.MousePress, Button: input.MouseLeft, Col: 9, Clicks: 3})

	if got := term.g.Selection(); got.Anchor.X != 4 || got.Cursor.X != 13 {
		t.Errorf("shown at first: %d to %d, want the word, 4 to 13", got.Anchor.X, got.Cursor.X)
	}
}

func TestBetweenStepsAcrossRows(t *testing.T) {
	a := grid.Selection{Anchor: grid.Point{X: 5, Y: 2}, Cursor: grid.Point{X: 5, Y: 2}, Active: true}
	b := grid.Selection{Anchor: grid.Point{X: 0, Y: 1}, Cursor: grid.Point{X: 9, Y: 3}, Active: true}

	got := between(a, b, 0.5, 10)

	// 25 to 10 is half way at 17, 7.5 back rounded out to 8; 25 to 39
	// at 32.
	if got.Anchor != (grid.Point{X: 7, Y: 1}) || got.Cursor != (grid.Point{X: 2, Y: 3}) {
		t.Errorf("half way: %+v to %+v", got.Anchor, got.Cursor)
	}
	if got := between(a, b, 1.2, 10); got.Anchor != b.Anchor || got.Cursor != b.Cursor {
		t.Errorf("past the end: %+v, want %+v", got, b)
	}
}
