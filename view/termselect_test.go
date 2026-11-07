package view

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// A double click in a terminal picks the word, growing out from the
// cell clicked to its ends over the frames after, and a third click the
// line, growing out from the word. With KAKEL_SHOTS set the frames are
// written there too, to see it move.
func TestADoubleClickedWordGrowsOutInThePane(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	win, sh, publish := windowStageOf(t, geom.Sz(700, 240))
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.NewPrinted([]byte("$ cat /home/someone/projects/notes.txt | less")), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"}
	publish(st)
	tm := win.terms["p1"]
	pal := vt.DefaultPalette()
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(topRowOf(sh.Get("p1")), "less") {
		if time.Now().After(deadline) {
			t.Fatal("the program's output never arrived")
		}
		time.Sleep(10 * time.Millisecond)
	}
	publish(st)

	box, _ := lastUI.Bounds(tm)
	cell := tm.cells.CellSize()
	// On the r of someone, the 14th cell.
	at := geom.Pt(box.Min.X+13.5*cell.W, box.Min.Y+0.5*cell.H)
	shot := func(name string) {
		if dir != "" {
			gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
		}
	}
	press := func(n int) {
		now := time.Now()
		lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: n, Time: now})
		lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Time: now})
	}
	// shown is the first and last cell of the top row drawn in the
	// selection's colour, -1 for none.
	shown := func() (from, to int) {
		from, to = -1, -1
		sh.Get("p1").Drawn(func(g *grid.Grid) {
			cols, _ := g.Size()
			for x := range cols {
				if g.BGOf(x, 0) == pal.Selection {
					if from < 0 {
						from = x
					}
					to = x
				}
			}
		})
		return from, to
	}

	press(1)
	lastWindow.Frame(time.Second / 60)
	press(2)
	if got := sh.Get("p1").T.SelectionText(); got != "/home/someone/projects/notes.txt" {
		t.Fatalf("a double click picked %q", got)
	}
	lastWindow.Frame(time.Second / 60)
	from, to := shown()
	if from <= 6 && to >= 37 {
		t.Fatalf("the word showed whole, %d to %d, at the first frame: it popped in", from, to)
	}
	wasFrom, wasTo := from, to
	for i := range 30 {
		lastWindow.Frame(time.Second / 60)
		from, to := shown()
		if from > wasFrom || to < wasTo {
			t.Fatalf("frame %d: the word shrank, %d to %d after %d to %d", i, from, to, wasFrom, wasTo)
		}
		wasFrom, wasTo = from, to
		if i < 4 {
			shot("word-" + strconv.Itoa(i))
		}
	}
	if from, to := shown(); from != 6 || to != 37 {
		t.Fatalf("grown, the word shows %d to %d, want 6 to 37", from, to)
	}

	press(3)
	if got := sh.Get("p1").T.SelectionText(); got != "$ cat /home/someone/projects/notes.txt | less" {
		t.Fatalf("a triple click picked %q", got)
	}
	lastWindow.Frame(time.Second / 60)
	if from, to := shown(); from < 1 {
		t.Fatalf("the line showed %d to %d at the first frame: it popped in", from, to)
	}
	for i := range 30 {
		lastWindow.Frame(time.Second / 60)
		if i < 4 {
			shot("line-" + strconv.Itoa(i))
		}
	}
	if growingIn(sh.Get("p1")) {
		t.Fatal("the line is still growing after half a second")
	}
}

// topRowOf is the text of a shell's top row.
func topRowOf(sh *screen.Shell) string {
	var b strings.Builder
	sh.Drawn(func(g *grid.Grid) {
		cols, _ := g.Size()
		for x := range cols {
			if r := g.At(x, 0).Rune; r != 0 {
				b.WriteRune(r)
			}
		}
	})
	return b.String()
}

// growingIn reports whether a shell's selection is still growing.
func growingIn(sh *screen.Shell) bool {
	growing, _ := sh.T.SelectionGrowing()
	return growing
}
