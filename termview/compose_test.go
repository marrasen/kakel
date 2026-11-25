package termview

import (
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/themes"
)

// shot writes what the window shows to the folder KAKEL_SHOTS names.
func (tm *testTerm) shot(t *testing.T, name string) {
	t.Helper()
	if dir := os.Getenv("KAKEL_SHOTS"); dir != "" {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, tm.win))
	}
}

// inkIn counts the pixels of a cell of the screen that are not its
// ground.
func (tm *testTerm) inkIn(t *testing.T, col, row int) (n int) {
	t.Helper()
	img := gunimtest.Pixels(t, tm.win)
	at, cell := tm.Drawn()
	box, _ := tm.stage.u.Bounds(tm.Term)
	x, y := int(box.Min.X+at.X+float32(col)*cell.W), int(box.Min.Y+at.Y+float32(row)*cell.H)
	ground := tm.compose.ground
	for dy := range int(cell.H) {
		for dx := range int(cell.W) {
			if c := img.RGBAAt(x+dx, y+dy); c != (color.RGBA{R: ground.R, G: ground.G, B: ground.B, A: 0xff}) {
				n++
			}
		}
	}
	return n
}

// What an input method composes is drawn at the cursor, and is the
// program's once it is done; on a desktop, which leaves it out, it is
// not taken.
func TestWhatIsComposedShowsAtTheCursor(t *testing.T) {
	sess := sessiontest.NewPrinted([]byte("$ "))
	tm := onStage(t, geom.Sz(500, 200), sess, screen.Hooks{})
	tm.await(t, "the prompt never came", func() bool { return strings.HasPrefix(tm.sh.T.AllText(), "$") })
	tm.frames(5)
	tm.win.Input(gi.Composing{Text: "ab", Selected: [2]int{2, 2}})
	tm.frames(3)
	if tm.Composing() != "" {
		t.Error("with no text input it composes")
	}
	tm.TextInput = true
	tm.win.Input(gi.Composing{Text: "a日e\u0301", Selected: [2]int{4, 4}})
	tm.frames(3)
	if tm.Composing() == "" {
		t.Fatal("nothing is composed")
	}
	tm.shot(t, "compose")
	for col := 2; col < 6; col++ {
		if tm.inkIn(t, col, 0) == 0 {
			t.Errorf("nothing is drawn in column %d", col)
		}
	}
	if sent := sess.Sent(); sent != "" {
		t.Errorf("before it is done the program has %q", sent)
	}
	tm.win.Input(gi.Composing{})
	tm.win.Input(gi.TextInput{Text: "日", Time: time.Now()})
	tm.frames(3)
	if tm.Composing() != "" || tm.inkIn(t, 4, 0) != 0 {
		t.Error("what is done still shows")
	}
	tm.await(t, "the program never had it", func() bool { return sess.Sent() == "日" })

	// Too long for the row it stands on, its end shows.
	cols, _ := tm.cells.GridSize()
	tm.win.Input(gi.Composing{Text: strings.Repeat("x", 3*cols), Selected: [2]int{3 * cols, 3 * cols}})
	tm.frames(3)
	tm.shot(t, "compose-long")
	if tm.inkIn(t, 0, 0) == 0 || tm.inkIn(t, cols-2, 0) == 0 {
		t.Error("a long one does not fill the row")
	}
	// The keyboard gone, so is it.
	tm.stage.u.Focus(nil)
	tm.frames(3)
	if tm.Composing() != "" {
		t.Error("without the keyboard it still composes")
	}
}

// A terminal's colours glide to a new theme's, every frame on the way.
func TestANewThemesColoursGlide(t *testing.T) {
	tm := onStage(t, geom.Sz(400, 200), sessiontest.NewPrinted([]byte("$ ")), screen.Hooks{})
	tm.await(t, "the prompt never came", func() bool { return strings.HasPrefix(tm.sh.T.AllText(), "$") })
	tm.frames(10)
	from, err := look.Of(themes.Built()[0])
	if err != nil {
		t.Fatal(err)
	}
	to, err := look.Of(themes.Built()[1])
	if err != nil {
		t.Fatal(err)
	}
	u := tm.stage.u
	tm.sh.SetPalette(from.Palette)
	u.UseTheme(from.Theme)
	tm.Sync()
	tm.frames(120)
	at := func() int {
		img := gunimtest.Pixels(t, tm.win)
		return int(img.RGBAAt(200, 60).R)
	}
	was, want := at(), int(to.Palette.BG.R)
	if was != int(from.Palette.BG.R) {
		t.Fatalf("at rest its ground is %d", was)
	}
	tm.sh.SetPalette(to.Palette)
	u.UseTheme(to.Theme)
	tm.Sync()
	u.Invalidate()
	var seen []int
	for i := range 60 {
		tm.frames(1)
		seen = append(seen, at())
		if i%4 == 0 && i < 24 {
			tm.shot(t, "recolour-"+strconv.Itoa(i))
		}
	}
	between := 0
	for _, v := range seen {
		if d := v - was; d != 0 && v != want {
			between++
		}
	}
	if between < 5 || seen[len(seen)-1] != want {
		t.Errorf("the ground went from %d to %d as %v", was, want, seen)
	}
}
