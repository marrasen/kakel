package guest

import (
	"context"
	"fmt"
	"image/color"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// shot is the screen of term.
func shot(r *rig, term *uiterm.Terminal) *grid.Grid {
	g := grid.New(1, 1, color.RGBA{}, color.RGBA{})
	r.until("the screen is let go of", func() bool { return term.Snapshot(g) })
	return g
}

// differ says how two screens differ, cell by cell, or nothing.
func differ(a, b *grid.Grid) string {
	var out strings.Builder
	ac, ar := a.Size()
	bc, br := b.Size()
	if ac != bc || ar != br {
		return fmt.Sprintf("%dx%d and %dx%d", ac, ar, bc, br)
	}
	for y := range ar {
		for x := range ac {
			ca, cb := a.At(x, y), b.At(x, y)
			for _, c := range []*grid.Cell{&ca, &cb} {
				if c.Rune == 0 && c.Width == 1 {
					c.Rune = ' '
				}
				if c.Rune == ' ' {
					// Not drawn.
					c.Attr &^= grid.AttrDim
				}
				c.Wrapped, c.Link = false, 0
			}
			if !ca.Equal(cb) && out.Len() < 2000 {
				fmt.Fprintf(&out, "%d,%d: %+v and %+v\n", x, y, ca, cb)
			}
		}
	}
	if ca, cb := a.Cursor(), b.Cursor(); ca != cb {
		fmt.Fprintf(&out, "the cursor: %+v and %+v\n", ca, cb)
	}
	return out.String()
}

// keys are what a terminal sends for a few keys, which says which modes
// it has.
func keys(term *uiterm.Terminal) string {
	var b strings.Builder
	for _, k := range []input.Key{input.KeyUp, input.KeyHome, input.KeyF1} {
		fmt.Fprintf(&b, "%q ", term.EncodeKey(input.Event{Kind: input.KeyPress, Key: k}))
	}
	fmt.Fprint(&b, term.Bracketed(), term.MouseTaken(0))
	return b.String()
}

// output is a piece of what programs write, picked by r.
func output(r *rand.Rand, cols, rows int) string {
	n := func(most int) int { return r.IntN(most) + 1 }
	pick := func(of ...string) string { return of[r.IntN(len(of))] }
	switch r.IntN(22) {
	case 0, 1, 2, 3:
		return strings.Repeat(pick("a", "b ", "word ", "漢", "e\u0301", "─"), n(2*cols))
	case 4, 5, 6:
		return "\r\n"
	case 7:
		return fmt.Sprintf("\x1b[%d;%dH", n(rows+2), n(cols+2))
	case 8:
		return "\x1b[" + pick("", "1", "2", "3") + pick("J", "K")
	case 9:
		return fmt.Sprintf("\x1b[%d%s", n(4), pick("A", "B", "C", "D", "L", "M", "P", "@", "X", "S", "T", "b"))
	case 10, 11:
		return "\x1b[" + pick("0", "1", "2", "4", "7", "31", "44", "38;5;99", "48;2;1;2;3", "39", "49") + "m"
	case 12:
		return fmt.Sprintf("\x1b[%d;%dr", n(rows), n(rows))
	case 13:
		return pick("\x1b7", "\x1b8", "\x1b[r", "\x1bM", "\x1bH", "\x1bc")
	case 14:
		return "\x1b[?" + pick("1", "5", "7", "25", "1000", "1002", "1006", "2004") + pick("h", "l")
	case 15:
		return "\x1b[?1049" + pick("h", "l")
	case 16:
		return "\x1b[4" + pick("h", "l", "l")
	case 17:
		return fmt.Sprintf("\x1b[%d q", r.IntN(7))
	case 18:
		return "\x1b[?2026h" + strings.Repeat("frame ", n(20)) + pick("\x1b[?2026l", "\x1b[?2026l", "")
	case 19:
		// A question, which the guest's end must not answer.
		return pick("\x1b[c", "\x1b[6n", "\x1b]11;?\x07", "\x1b[?2026$p")
	case 20:
		return strings.Repeat(fmt.Sprintf("line %d\r\n", n(1000)), n(50))
	}
	return pick("\xe2\x82", "\xff", "\x1b]52;c;aGVq\x07", "\x1b]8;;https://x.example\x07", "\x1bPq\x1b\\", "\t", "\b")
}

// The guest's screen is the host's, cell by cell, whatever the program
// writes, however it is cut, and whatever sizes the host's window has
// meanwhile.
func TestTheGuestsScreenIsTheHosts(t *testing.T) {
	seeds := 5
	if n, err := strconv.Atoi(os.Getenv("KAKEL_GUEST_SEEDS")); err == nil {
		seeds = n
	}
	r := newRig(t, false)
	s := func() *Session { r.publish(terminals(true)); return r.in(key(t)) }()
	for seed := range seeds {
		rnd := rand.New(rand.NewPCG(uint64(seed), 11))
		id := "pane" + strconv.Itoa(seed)
		term, p := terminal(t, 80, 24)
		r.share(id, term)
		r.publish(terminals(true, id))
		say(r, term, p, "before the guest came\r\n", "start")
		pane, err := s.Pane(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		there := shownOn(t, pane)
		for round := range 15 {
			cols, rows := term.Size().Cols, term.Size().Rows
			for range rnd.IntN(12) {
				var text strings.Builder
				for range rnd.IntN(10) {
					text.WriteString(output(rnd, cols, rows))
				}
				// Cut anywhere.
				for out := []byte(text.String()); len(out) > 0; {
					n := rnd.IntN(len(out)) + 1
					p.out <- out[:n:n]
					out = out[n:]
				}
				switch rnd.IntN(5) {
				case 0:
					cols, rows = rnd.IntN(120)+2, rnd.IntN(40)+2
					term.Layout(ui.Size{Cols: cols, Rows: rows})
				case 1:
					term.Hold(rnd.IntN(120)+2, rnd.IntN(40)+2)
				case 2:
					term.Release()
				case 3:
					if rnd.IntN(4) == 0 {
						term.Say("the window says this")
					}
				}
			}
			// Out of any string or update the program is part way through,
			// for the title to be taken.
			title := fmt.Sprintf("round-%d", round)
			say(r, term, p, "\x1b\\\x1b[?2026l", title)
			r.until("the guest takes "+title, func() bool { return there.Title() == title })
			if diff := differ(shot(r, term), shot(r, there)); diff != "" {
				t.Fatalf("seed %d round %d: the host's screen and the guest's differ:\n%s", seed, round, diff)
			}
			if a, b := keys(term), keys(there); a != b {
				t.Fatalf("seed %d round %d: the host's keys are %s and the guest's %s", seed, round, a, b)
			}
		}
		// The guest answered none of what the program asked.
		_ = pane.Close()
		_ = there.Close()
		host := strings.Count(p.typed(), "\x1b[?6c")
		r.until("the host answers alone", func() bool { return strings.Count(p.typed(), "\x1b[?6c") == host })
		_ = term.Close()
	}
}

// What a guest's terminal draws is at the host's size, from the first, and
// its own layout changes nothing.
func TestTheGuestHasTheHostsSize(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 50, 12)
	r.share("a", term)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	there := shownOn(t, pane)
	say(r, term, p, "hello", "one")
	r.until("the guest has it", func() bool { return there.Title() == "one" })
	size := func() ui.Size {
		g := grid.New(80, 24, color.RGBA{}, color.RGBA{})
		there.DrawScreen(g.View())
		return there.Size()
	}
	if got := size(); got != (ui.Size{Cols: 50, Rows: 12}) {
		t.Errorf("the guest's is %v", got)
	}
	there.Layout(ui.Size{Cols: 30, Rows: 5})
	there.Hold(31, 6)
	there.Release()
	if got := size(); got != (ui.Size{Cols: 50, Rows: 12}) || !there.Held() {
		t.Errorf("after a layout the guest's is %v", got)
	}
	term.Layout(ui.Size{Cols: 20, Rows: 4})
	r.until("the guest's follows", func() bool { return size() == ui.Size{Cols: 20, Rows: 4} })
	// And the host's is not the guest's to change.
	_ = pane.Resize(10, 10)
	if got := term.Size(); got != (ui.Size{Cols: 20, Rows: 4}) {
		t.Errorf("the host's is %v", got)
	}
}

// The lines that scroll off at the guest stay in its history, through
// screens sent again.
func TestTheGuestKeepsItsHistory(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 40, 5)
	r.share("a", term)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	there := shownOn(t, pane)
	// Once it follows, every line comes to it.
	say(r, term, p, "", "zero")
	r.until("the guest follows", func() bool { return there.Title() == "zero" })
	var text strings.Builder
	for i := range 20 {
		fmt.Fprintf(&text, "line %d\r\n", i)
	}
	say(r, term, p, text.String(), "one")
	r.until("the guest has it", func() bool { return there.Title() == "one" })
	for i := range 6 {
		term.Layout(ui.Size{Cols: 40 + i, Rows: 5 + i%3})
		title := "sized " + strconv.Itoa(i)
		say(r, term, p, "", title)
		r.until("the guest has it", func() bool { return there.Title() == title })
	}
	if a, b := strings.TrimSpace(term.AllText()), strings.TrimSpace(there.AllText()); a != b {
		t.Errorf("the host has\n%s\nand the guest\n%s", a, b)
	}
}
