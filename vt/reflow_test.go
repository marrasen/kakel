package vt

import (
	"strings"
	"testing"

	"github.com/marrasen/kakel/grid"
)

// every is each line from the oldest history keeps to the screen's last row, as text with its blank end cut off.
func (h *harness) every() []string {
	h.t.Helper()
	s := h.term.Screen()
	_, rows := s.Size()
	var out []string
	for n := s.OldestLine(); n <= int(s.LineNumber(rows-1)); n++ {
		out = append(out, cellText(s.LineAt(n)))
	}
	return out
}

// cellText is a row of cells as text, with its blank end cut off.
func cellText(cells []grid.Cell) string {
	var sb strings.Builder
	for _, c := range cells {
		switch {
		case c.Width == 0:
		case c.Rune == 0:
			sb.WriteByte(' ')
		default:
			sb.WriteRune(c.Rune)
		}
	}
	return strings.TrimRight(sb.String(), " ")
}

// A pane made narrower and then wide again shows its lines whole.
func TestANarrowedPaneWidenedAgainShowsItsLinesWhole(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("0123456789abcdefghij\r\nshort\r\n$ ")

	h.term.Resize(10, 4)
	h.term.Resize(20, 4)

	h.wantLines("0123456789abcdefghij", "short", "$")
	h.wantCursor(2, 2)
}

// A narrower pane wraps a long line onto more rows rather than cutting it.
func TestANarrowerPaneWrapsALongLine(t *testing.T) {
	h := newHarness(t, 20, 6)
	h.write("0123456789abcdefghij\r\nshort\r\n$ ")

	h.term.Resize(10, 6)

	h.wantLines("0123456789", "abcdefghij", "short", "$")
	h.wantCursor(2, 3)
}

// A wider pane joins the rows of a line the terminal wrapped.
func TestAWiderPaneJoinsAWrappedLine(t *testing.T) {
	h := newHarness(t, 10, 6)
	h.write("0123456789abcdefghij\r\n$ ")

	h.term.Resize(20, 6)

	h.wantLines("0123456789abcdefghij", "$")
	h.wantCursor(2, 1)
}

// Lines the program ended itself stay apart however wide the pane gets.
func TestLinesTheProgramEndedAreNotJoined(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("0123456789\r\nabcdefghij\r\n")

	h.term.Resize(20, 4)

	h.wantLines("0123456789", "abcdefghij")
}

// History is re-wrapped too, so scrolling back after a narrow spell finds every line whole.
func TestHistoryComesBackWhole(t *testing.T) {
	h := newHarness(t, 30, 3)
	var want []string
	for i := range 20 {
		line := strings.Repeat(string(rune('a'+i)), 25)
		want = append(want, line)
		h.write(line + "\r\n")
	}
	h.write("$ ")
	want = append(want, "$")

	h.term.Resize(10, 3)
	h.term.Resize(30, 3)

	got := h.every()
	got = got[len(got)-len(want):]
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// Typing after a resize carries on from where the cursor was in the text.
func TestTheCursorStaysInItsText(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("$ 0123456789abc")

	h.term.Resize(8, 4)
	h.write("Z")
	h.term.Resize(20, 4)

	h.wantLines("$ 0123456789abcZ")
	h.wantCursor(16, 0)
}

// A cursor that owes a wrap still owes it when the pane is the width of its line.
func TestAWrapOwedIsStillOwed(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("0123456789")

	h.term.Resize(20, 4)
	h.wantCursor(10, 0)
	h.term.Resize(10, 4)
	h.write("Z")

	h.wantLines("0123456789", "Z")
}

// A wide character that would straddle the new edge moves down whole, and comes back up when the pane widens.
func TestAWideCharacterMovesDownWhole(t *testing.T) {
	h := newHarness(t, 10, 4)
	h.write("abcd中文\r\n")

	h.term.Resize(5, 4)
	h.wantLines("abcd", "中文")
	h.term.Resize(10, 4)

	h.wantLines("abcd中文")
}

// Where the last command's output began follows the text when lines above it are re-wrapped.
func TestCommandOutputFollowsAResize(t *testing.T) {
	h := newHarness(t, 20, 8)
	h.write("0123456789abcdefghij\r\n$ ls\r\n\x1b]133;C\x07out\r\n")
	from, _ := h.term.Command().Output()
	if got := cellText(h.term.Screen().LineAt(int(from))); got != "out" {
		t.Fatalf("the output starts on %q, want %q", got, "out")
	}

	h.term.Resize(10, 8)

	from, _ = h.term.Command().Output()
	if got := cellText(h.term.Screen().LineAt(int(from))); got != "out" {
		t.Errorf("after narrowing the output starts on %q, want %q", got, "out")
	}
}

// A pin follows its line.
func TestAPinFollowsItsLine(t *testing.T) {
	h := newHarness(t, 20, 8)
	h.write("0123456789abcdefghij\r\n$ ")
	_, row := h.term.Screen().CursorPos()
	pin := h.term.Pin(h.term.Screen().LineNumber(row))

	h.term.Resize(10, 8)

	if got := cellText(h.term.Screen().LineAt(int(pin.Line()))); got != "$" {
		t.Errorf("the pin is on %q, want the prompt", got)
	}
	_, row = h.term.Screen().CursorPos()
	if got, want := pin.Line(), h.term.Screen().LineNumber(row); got != want {
		t.Errorf("the pin is on line %d, want the cursor's line %d", got, want)
	}
}

// The returned move takes a cell to where its text went.
func TestAResizeSaysWhereACellWent(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("0123456789abcdefghij")

	moved := h.term.Resize(10, 4)

	line, col := moved(0, 12)
	if cell := h.term.Screen().LineAt(int(line))[col]; cell.Rune != 'c' {
		t.Errorf("column 12 went to line %d column %d, which holds %q, want 'c'", line, col, cell.Rune)
	}
}

// A view scrolled back stays on the text it showed.
func TestAScrolledBackViewStaysOnItsText(t *testing.T) {
	h := newHarness(t, 20, 3)
	for i := range 10 {
		h.write(strings.Repeat(string(rune('a'+i)), 15) + "\r\n")
	}
	h.term.Screen().ScrollView(5)
	top := h.line(0)

	h.term.Resize(10, 3)

	if got := h.line(0); !strings.HasPrefix(top, got) || got == "" {
		t.Errorf("the view's top row is %q, want the start of %q", got, top)
	}
}

// A clear stays a clear when the pane is narrowed and widened.
func TestAResizeDoesNotUndoAClear(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("0123456789abcdefghij\r\none\r\ntwo\r\n")
	h.write("\x1b[H\x1b[2J\x1b[3J$ ")

	h.term.Resize(10, 4)
	h.term.Resize(20, 6)

	h.wantLines("$", "", "", "", "", "")
	scr := h.term.Screen()
	if scr.LineNumber(0) != scr.Floor() {
		t.Errorf("the top row is line %d, want the floor at %d", scr.LineNumber(0), scr.Floor())
	}
}

// The ordinary screen is re-wrapped under a full-screen program, and its cursor comes back where its text went.
func TestTheOrdinaryScreenIsReWrappedUnderAFullScreenProgram(t *testing.T) {
	h := newHarness(t, 20, 6)
	h.write("0123456789abcdefghij\r\n$ ")
	h.write("\x1b[?1049h")

	h.term.Resize(10, 6)
	h.write("\x1b[?1049l")

	h.wantLines("0123456789", "abcdefghij", "$")
	h.wantCursor(2, 2)
}

// History past its limit is trimmed when narrowing makes more rows of it.
func TestHistoryStaysWithinItsLimit(t *testing.T) {
	h := newHarness(t, 20, 3)
	for range 150 {
		h.write("0123456789abcdefghij\r\n")
	}

	h.term.Resize(5, 3)

	if got := h.term.Screen().History(); got > 100 {
		t.Errorf("history holds %d rows, more than its limit of 100", got)
	}
}

// A full-screen program on the alternate screen without a saved cursor leaves the ordinary screen's prompt in place.
func TestTheOrdinaryCursorIsKeptWithoutASave(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("0123456789abcdefghij\r\nline2\r\nline3\r\n$ ")
	h.write("\x1b[?1047h")

	h.term.Resize(10, 4)
	h.write("\x1b[?1047l")

	h.wantLines("abcdefghij", "line2", "line3", "$")
}

// Rows are padded in the default colours, whatever the pen is.
func TestPaddingTakesNoColourFromThePen(t *testing.T) {
	h := newHarness(t, 20, 4)
	h.write("abc\r\ndef\r\n\x1b[41m$ ")

	h.term.Resize(10, 4)

	if got := h.term.Screen().LineAt(0)[5].BG; got != DefaultPalette().BG {
		t.Errorf("the padding of row 0 is %v, want the default background", got)
	}
}

// A screen that keeps no history keeps none after a resize either.
func TestNoHistoryStaysNoHistory(t *testing.T) {
	term := New(20, 3, DefaultPalette(), 0, Callbacks{})
	feed(t, term, "0123456789abcdefghij\r\n0123456789abcdefghij\r\n$ ")

	term.Resize(10, 3)

	if got := term.Screen().History(); got != 0 {
		t.Errorf("history holds %d rows, want none", got)
	}
}

// A point past the end of a line's text comes back to its column when the pane is widened again.
func TestAPointPastTheTextComesBack(t *testing.T) {
	h := newHarness(t, 40, 4)
	h.write("abc\r\n$ ")

	in, c := h.term.Resize(10, 4)(0, 30)
	out, col := h.term.Resize(40, 4)(in, c)

	if out != 0 || col != 30 {
		t.Errorf("column 30 of line 0 came back as line %d column %d", out, col)
	}
}
