package vt

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/grid"
)

// alike says how two terminals differ in what the next output lands on,
// or nothing when they do not.
//
// wraps has the rows that run on to the next compared too. The bottom row
// of a screen may say it does, after a resize has cut off what it ran on
// to, which does not travel and shows once it has scrolled.
func alike(a, b *Terminal, wraps bool) string {
	var out strings.Builder
	say := func(format string, args ...any) { fmt.Fprintf(&out, format+"\n", args...) }
	sa, sb := a.scr, b.scr
	if sa.cols != sb.cols || sa.rows != sb.rows {
		return fmt.Sprintf("the size is %dx%d and %dx%d", sa.cols, sa.rows, sb.cols, sb.rows)
	}
	if sa.OnAltBuffer() != sb.OnAltBuffer() {
		return fmt.Sprintf("on the alternate screen: %v and %v", sa.OnAltBuffer(), sb.OnAltBuffer())
	}
	lines := func(what string, la, lb []line) {
		for y := range la {
			for x := range la[y] {
				ca, cb := la[y][x], lb[y][x]
				if ca.Rune == 0 && ca.Width == 1 {
					ca.Rune = ' '
				}
				if cb.Rune == 0 && cb.Width == 1 {
					cb.Rune = ' '
				}
				// A blank's dimness is not drawn, and not sent.
				if ca.Rune == ' ' {
					ca.Attr &^= grid.AttrDim
					cb.Attr &^= grid.AttrDim
				}
				if a.LinkURL(ca.Link) != b.LinkURL(cb.Link) && ca.Rune != ' ' && ca.Width > 0 {
					say("%s %d,%d links to %q and %q", what, x, y, a.LinkURL(ca.Link), b.LinkURL(cb.Link))
				}
				ca.Link, cb.Link, ca.Wrapped, cb.Wrapped = 0, 0, ca.Wrapped && x == sa.cols-1, cb.Wrapped && x == sa.cols-1
				// The cell a cursor owes a wrap from is written again to send
				// that, which loses that its row ran on already.
				owes := false
				for _, c := range []cursorState{sa.cursor, sa.saved[0], sa.saved[1]} {
					owes = owes || c.WrapNext && c.Y == y
				}
				if y == sa.rows-1 || !wraps || owes {
					ca.Wrapped, cb.Wrapped = false, false
				}
				if !ca.Equal(cb) || ca.Wrapped != cb.Wrapped {
					say("%s %d,%d is %s wrapped %v and %s wrapped %v", what, x, y, show(ca), ca.Wrapped, show(cb), cb.Wrapped)
				}
			}
		}
	}
	lines("screen", sa.cur.lines, sb.cur.lines)
	if sa.OnAltBuffer() {
		lines("under", sa.pri.lines, sb.pri.lines)
	}
	cur := func(what string, ca, cb cursorState) {
		if ca.X != cb.X || ca.Y != cb.Y || ca.WrapNext != cb.WrapNext || ca.Origin != cb.Origin {
			say("%s is %d,%d wrap %v origin %v and %d,%d wrap %v origin %v", what, ca.X, ca.Y, ca.WrapNext, ca.Origin, cb.X, cb.Y, cb.WrapNext, cb.Origin)
		}
		if !sameStyle(ca.Pen, cb.Pen) || a.LinkURL(ca.Pen.Link) != b.LinkURL(cb.Pen.Link) {
			say("%s draws %s and %s", what, show(ca.Pen), show(cb.Pen))
		}
	}
	cur("the cursor", sa.cursor, sb.cursor)
	cur("the saved cursor", sa.saved[0], sb.saved[0])
	if sa.OnAltBuffer() {
		cur("the alternate saved cursor", sa.saved[1], sb.saved[1])
	}
	if sa.top != sb.top || sa.bot != sb.bot {
		say("the region is %d-%d and %d-%d", sa.top, sa.bot, sb.top, sb.bot)
	}
	ma, mb := sa.mode, sb.mode
	ma.Sync, mb.Sync = false, false
	if ma != mb {
		say("the modes are %+v and %+v", ma, mb)
	}
	if fmt.Sprint(sa.tabs) != fmt.Sprint(sb.tabs) {
		say("the tabs are %v and %v", sa.tabs, sb.tabs)
	}
	if sa.curStyle != sb.curStyle || sa.curBlink != sb.curBlink {
		say("the cursor's shape is %v %v and %v %v", sa.curStyle, sa.curBlink, sb.curStyle, sb.curBlink)
	}
	if a.title != b.title {
		say("the title is %q and %q", a.title, b.title)
	}
	if a.lastRune != b.lastRune {
		say("the last character is %q and %q", a.lastRune, b.lastRune)
	}
	if len(a.LivePlaced()) != len(b.LivePlaced()) {
		say("the images are %d and %d", len(a.LivePlaced()), len(b.LivePlaced()))
	}
	return out.String()
}

// adopted is a terminal brought to where a stands.
func adopted(a *Terminal, with *Terminal) *Terminal {
	if with == nil {
		with = New(9, 4, DefaultPalette(), 50, Callbacks{})
	}
	seq, _ := a.Replay()
	cols, rows := a.scr.Size()
	with.Adopt(cols, rows, []byte(seq))
	return with
}

// said is a piece of what a program might write, picked by r.
func said(r *rand.Rand, cols, rows int) string {
	n := func(most int) int { return r.IntN(most) + 1 }
	pick := func(of ...string) string { return of[r.IntN(len(of))] }
	switch r.IntN(34) {
	case 0, 1, 2, 3, 4:
		return strings.Repeat(pick("a", "b ", "word ", "漢", "é", "e\u0301", "─"), n(2*cols))
	case 5, 6, 7:
		return "\r\n"
	case 8:
		return fmt.Sprintf("\x1b[%d;%dH", n(rows+2), n(cols+2))
	case 9:
		return fmt.Sprintf("\x1b[%d%s", n(4), pick("A", "B", "C", "D", "E", "F", "G", "d"))
	case 10:
		return "\x1b[" + pick("", "1", "2", "3") + pick("J", "K")
	case 11:
		return fmt.Sprintf("\x1b[%d%s", n(4), pick("L", "M", "P", "@", "X", "S", "T", "b", "I", "Z"))
	case 12, 13:
		return "\x1b[" + pick("0", "1", "2", "3", "4", "7", "8", "9", "31", "44", "38;5;99", "48;2;1;2;3", "91", "39", "49", "27") + "m"
	case 14:
		return fmt.Sprintf("\x1b[%d;%dr", n(rows), n(rows))
	case 15:
		return "\x1b[r"
	case 16:
		return pick("\x1b7", "\x1b8", "\x1b[s", "\x1b[u")
	case 17:
		return "\x1b[?6" + pick("h", "l", "l")
	case 18:
		return "\x1b[?7" + pick("h", "h", "l")
	case 19:
		return "\x1b[4" + pick("h", "l", "l")
	case 20:
		return "\x1b[?" + pick("1", "5", "25", "1000", "1002", "1003", "1004", "1006", "2004") + pick("h", "l")
	case 21:
		return "\x1b[?1049" + pick("h", "l")
	case 22:
		return pick("\x1b=", "\x1b>", "\x1bH", "\x1b[3g", "\x1b[g", "\x1bD", "\x1bM", "\x1bE", "\x1b#8")
	case 23:
		return fmt.Sprintf("\x1b[%d q", r.IntN(7))
	case 24:
		return pick("\x1b]0;a title\x07", "\x1b]2;övrigt\x1b\\", "\x1b]2;\x07")
	case 25:
		return pick("\x1b]8;;https://kakel.example/a;b\x07", "\x1b]8;;\x07", "\x1b]8;;https://other.example\x1b\\")
	case 26:
		return pick("\t", "\b", "\r", "\n", "\x07")
	case 27:
		return pick("\x1bPq#0;2;0;0;0\x1b\\", "\x1b_an apc\x1b\\", "\x1b^pm\x1b\\", "\x1bXsos\x1b\\", "\x1b]52;c;aGVq\x07")
	case 28:
		return pick("\xff", "\xe2\x82", "\xc3", "\xe2\xe2", "\xf0\x9f\x98", "\xf0\x9f\xe2", "\x9b", "\x90", "\xc3\x1b", "\xe2\n", "\x1b\n]0;late", "\x1b\r\n[3", "\x1b\nP", "\x1b\b_", "\x1b]0;a\nb", "\xe2\x1b\x1b", "\x1b[3\x1b[", "\x1b]0;a\x1b", "\x1b[3\n1m", "\x1b[12\x18", "\x1b[>c", "\x1b[38;2;1", "\x1b", "\x1b]0;cut")
	case 29:
		return strings.Repeat("x", cols)
	case 30:
		return strings.Repeat("漢", cols/2+1)
	case 31:
		return pick("\x1b[?47h", "\x1b[?1047l", "\x1bc", "\x1b[?1048h", "\x1b[?1048l") // 47 and 1047 leave by 1049 below
	case 32:
		return pick("\x1b]1337;File=inline=1;width=2;height=1:aaaa\x07", "\x1b]1338;nothing;1\x07", "\x1b]1337;Other\x07")
	}
	return "\x1b[0m"
}

// A terminal brought to where another stands stays like it through
// whatever is written to both next, wherever the program was cut.
func TestATerminalBroughtToAnotherStaysLikeIt(t *testing.T) {
	runs := 1500
	if testing.Short() {
		runs = 300
	}
	for seed := range runs {
		r := rand.New(rand.NewPCG(uint64(seed), 7))
		cols, rows := r.IntN(30)+1, r.IntN(10)+1
		a := New(cols, rows, DefaultPalette(), 50, Callbacks{})
		var b *Terminal
		var log []string
		for step := range 12 {
			var text strings.Builder
			for range r.IntN(12) {
				s := said(r, cols, rows)
				if strings.Contains(s, "47h") {
					// What an alternate screen holds while it is hidden is
					// not sent, and only 1049 clears it on the way in.
					s = "\x1b[?1049h"
				}
				text.WriteString(s)
			}
			out := text.String()
			// Cut anywhere, a sequence and a character among it.
			if len(out) > 0 && r.IntN(2) == 0 {
				out = out[:r.IntN(len(out))]
			}
			log = append(log, out)
			_, _ = a.Write([]byte(out))
			if b != nil {
				_, _ = b.Write([]byte(out))
				if diff := alike(a, b, false); diff != "" {
					t.Fatalf("seed %d step %d: they part:\n%s\nafter %q", seed, step, diff, log)
				}
			}
			if c := a.scr.cursor; c.Origin && (c.Y < a.scr.top || c.Y > a.scr.bot) {
				// Restored from before the region was set, which no
				// sequence can say: not sent, see writeCursorState.
				break
			}
			fresh := false
			switch r.IntN(4) {
			case 0:
				fresh = true
				cols, rows = r.IntN(30)+1, r.IntN(10)+1
				a.Resize(cols, rows)
				log = append(log, fmt.Sprintf("<%dx%d>", cols, rows))
				b = adopted(a, b)
			case 1:
				fresh = true
				b = adopted(a, b)
				log = append(log, "<adopted>")
			}
			if b != nil {
				if diff := alike(a, b, fresh); diff != "" {
					t.Fatalf("seed %d step %d: it is not like it to begin with:\n%s\nafter %q", seed, step, diff, log)
				}
			}
		}
	}
}

// A control character is acted on where it stands in the middle of a
// sequence, and is not acted on again where the sequence is replayed.
func TestAControlInsideASequenceActsOnce(t *testing.T) {
	for _, begun := range []string{
		"\x1b\n]0;title", "\x1b\n[3", "\x1b[\n3", "\x1b\nPq", "\x1b\n_x", "\x1b\n\n\n]1337;x", "\x1b\n(", "\x1bP\n1q", "\x1b\n", "\xe2\n",
	} {
		a := New(20, 6, DefaultPalette(), 50, Callbacks{})
		_, _ = a.Write([]byte("one\r\n" + begun))
		b := adopted(a, nil)
		if diff := alike(a, b, true); diff != "" {
			t.Errorf("%q: %s", begun, diff)
		}
		for _, v := range []*Terminal{a, b} {
			_, _ = v.Write([]byte("\x07\x1b\\end"))
		}
		if diff := alike(a, b, true); diff != "" {
			t.Errorf("%q, ended: %s", begun, diff)
		}
	}
}

// history is what has scrolled off t, a line each.
func history(t *Terminal) string {
	var b strings.Builder
	for _, l := range t.scr.pri.scrollback {
		var row strings.Builder
		for _, c := range l {
			if c.Width > 0 {
				row.WriteRune(max(c.Rune, ' '))
			}
		}
		b.WriteString(strings.TrimRight(row.String(), " ") + "\n")
	}
	return b.String()
}

// A screen sent again, at whatever size, leaves history as the program
// left it: nothing eaten, and nothing twice.
func TestAScreenSentAgainLeavesHistoryAlone(t *testing.T) {
	for name, line := range map[string]func(i int) string{
		"the same": func(int) string { return "y" },
		"turns":    func(i int) string { return fmt.Sprint("turn ", i%3) },
		"each":     func(i int) string { return fmt.Sprint("line ", i) },
	} {
		a := New(40, 10, DefaultPalette(), 500, Callbacks{})
		b := adopted(a, New(9, 4, DefaultPalette(), 500, Callbacks{}))
		write := func(from, to int) {
			for i := from; i < to; i++ {
				_, _ = a.Write([]byte(line(i) + "\r\n"))
				_, _ = b.Write([]byte(line(i) + "\r\n"))
			}
		}
		write(0, 100)
		for i := range 30 {
			b = adopted(a, b)
			if got, want := history(b), history(a); got != want {
				t.Fatalf("%s, sent again %d times: history has %d lines, want %d", name, i+1, strings.Count(got, "\n"), strings.Count(want, "\n"))
			}
		}
		for i, size := range [][2]int{{40, 16}, {40, 5}, {40, 12}, {40, 12}, {44, 6}, {43, 20}, {60, 3}, {40, 10}} {
			a.Resize(size[0], size[1])
			b = adopted(a, b)
			write(100+i, 101+i)
			if got, want := history(b), history(a); got != want {
				t.Fatalf("%s, at %v: history has %d lines, want %d", name, size, strings.Count(got, "\n"), strings.Count(want, "\n"))
			}
		}
		// A screen that was cleared takes nothing back out of history, here
		// or there.
		_, _ = a.Write([]byte("\x1b[H\x1b[2J"))
		_, _ = b.Write([]byte("\x1b[H\x1b[2J"))
		a.Resize(40, 18)
		b = adopted(a, b)
		if got, want := history(b), history(a); got != want {
			t.Fatalf("%s, cleared: history has %d lines, want %d", name, strings.Count(got, "\n"), strings.Count(want, "\n"))
		}
	}
}

// The largest screen of the same line over and over is taken at once.
func TestALargeScreenIsTakenAtOnce(t *testing.T) {
	a := New(1000, 500, DefaultPalette(), 5000, Callbacks{})
	row := strings.Repeat("y", 999)
	for range 1500 {
		_, _ = a.Write([]byte(row + "\r\n"))
	}
	_, _ = a.Write([]byte("\x1b[250H different"))
	b := adopted(a, New(9, 4, DefaultPalette(), 5000, Callbacks{}))
	for range 1500 {
		_, _ = b.Write([]byte(row + "\r\n"))
	}
	start := time.Now()
	seq, _ := a.Replay()
	sent := time.Since(start)
	start = time.Now()
	b.Adopt(1000, 500, []byte(seq))
	if took := time.Since(start); took > 20*sent+time.Second {
		t.Errorf("it took %v, and %v to write", took, sent)
	}
}
