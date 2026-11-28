package view

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// hatching is how many stripes of hatching were drawn last, and how strong
// the strongest was.
func hatching() (n int, most uint8) {
	c := beyondInk.Get(lastUI.Theme())
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Stroke.Width == 0 {
			if f := r.Fill.Solid; f.R == c.R && f.G == c.G && f.B == c.B && f.A > 0 {
				n++
				most = max(most, f.A)
			}
		}
	}
	return n, most
}

// sizedFor is a share whose guest's page sets the size of panes.
func sizedFor(id, name string, panes ...string) app.WindowLink {
	l := isIn(id, name, "p1")
	l.ForGuest, l.Sizes = true, panes
	return l
}

// A terminal a guest's page sets the size of shows its screen from the top
// left, hatches the rest, and says whose size it is; one larger than the
// pane is drawn smaller to fit. All of it fades in and out.
func TestAPaneSizedForAGuestIsHatched(t *testing.T) {
	win, st, publish := linkStage(t, isIn("l1", "Bob", "p1"))
	settle()
	if n, _ := hatching(); n != 0 {
		t.Fatal("a pane sized for the window is hatched")
	}
	tm := win.terms["p1"]
	tm.Shell().T.Hold(20, 5)
	st.Links = []app.WindowLink{sizedFor("l1", "Bob", "p1")}
	publish(st)
	frames(1)
	_, early := hatching()
	settle()
	n, late := hatching()
	if n == 0 || early >= late {
		t.Fatalf("%d stripes, as strong as %d at first and %d after", n, early, late)
	}
	box, _ := lastUI.Bounds(tm)
	scr := tm.screenRect()
	if scr.Min != (geom.Point{}) || scr.Max.X >= box.Size().W || scr.Max.Y >= box.Size().H {
		t.Errorf("a smaller screen is drawn at %v in a pane of %v", scr, box.Size())
	}
	if tm.sized.note.Hidden() || tm.sized.note.Text != "Sized for Bob's page" {
		t.Errorf("the note is %q, hidden %v", tm.sized.note.Text, tm.sized.note.Hidden())
	}
	if w := win.terms["p2"]; !w.sized.note.Hidden() {
		t.Error("a pane the guest does not size has the note")
	}

	tm.Shell().T.Hold(400, 200)
	settle()
	scr = tm.screenRect()
	if r := (geom.Rect{Max: box.Size().Point()}); scr.Max.X > r.Max.X+0.5 || scr.Max.Y > r.Max.Y+0.5 || scr.Min.X < -0.5 || scr.Min.Y < -0.5 {
		t.Errorf("a larger screen is drawn at %v in a pane of %v", scr, box.Size())
	}

	// Two guests are both named.
	st.Links = []app.WindowLink{sizedFor("l1", "Bob", "p1"), sizedFor("l2", "Eve", "p1")}
	publish(st)
	if got := tm.sized.note.Text; got != "Sized for Bob's and Eve's pages" {
		t.Errorf("with two, the note is %q", got)
	}

	tm.Shell().T.Hold(20, 5)
	settle()
	_, full := hatching()
	st.Links = []app.WindowLink{isIn("l1", "Bob", "p1")}
	publish(st)
	frames(1)
	if n, a := hatching(); n == 0 || a >= full {
		t.Errorf("at once, %d stripes as strong as %d, from %d", n, a, full)
	}
	settle()
	tm.Shell().T.Release()
	if n, _ := hatching(); n != 0 || !tm.sized.note.Hidden() {
		t.Errorf("sized for the window again, %d stripes are drawn", n)
	}
}

// The hatching is what the screen leaves of the pane, on every side.
func TestTheHatchingIsWhatTheScreenLeaves(t *testing.T) {
	all := geom.Rc(0, 0, 100, 50)
	for _, tc := range []struct {
		inner geom.Rect
		area  float32
	}{
		{geom.Rc(0, 0, 100, 50), 0},
		{geom.Rc(0, 0, 60, 30), 100*50 - 60*30},
		{geom.Rc(20, 0, 60, 50), 100*50 - 60*50},
		{geom.Rc(-10, -10, 200, 200), 0},
	} {
		var area float32
		for _, r := range beside(all, tc.inner) {
			area += r.Size().W * r.Size().H
			if r.Min.X < all.Min.X || r.Min.Y < all.Min.Y || r.Max.X > all.Max.X || r.Max.Y > all.Max.Y {
				t.Errorf("%v leaves %v, outside the pane", tc.inner, r)
			}
		}
		if area != tc.area {
			t.Errorf("%v leaves %v of the pane, want %v", tc.inner, area, tc.area)
		}
	}
}

// The card's choice of whose size the terminals have, and the dialog's,
// say so.
func TestWhoseSizeIsChosen(t *testing.T) {
	win, st, publish := linkStage(t, isIn("l1", "Bob", "p1"))
	win.chips.order[0].do(lastUI)
	settle()
	n, ok := win.linkBox.list.Row(widget.Key("l1:guest"))
	if !ok {
		t.Fatal("no card")
	}
	c := n.(*linkCard)
	if !c.sizeRow.Open() || !slices.Contains(win.linkBox.Focusables(), gunim.Node(c.size)) {
		t.Fatal("the card offers no choice of size")
	}
	sentIntents()
	lastUI.Focus(c.size)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight, Time: time.Now()})
	frames(2)
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.SetLinkSize{ID: "l1", ForGuest: true})) {
		t.Errorf("the guest's size sent %+v", got)
	}
	// As the share has it, whatever was clicked.
	publish(st)
	settle()
	if c.size.Selected() != sizeIndex(false) {
		t.Error("the card shows a size the share does not have")
	}

	b := win.linkBox
	b.turn(linkMakes, lastUI)
	settle()
	b.relay.SetText(st.LinkSetup.Relay, lastUI)
	b.size.SetSelected(sizeIndex(true), lastUI)
	sentIntents()
	hit(gi.KeyEnter)
	if got := sentIntents(); !slices.ContainsFunc(got, func(in gunim.Intent) bool { s, ok := in.(app.ShareWindow); return ok && s.ForGuest }) {
		t.Errorf("making a link sized for the guest sent %+v", got)
	}
}

// Screenshots of a pane sized for a guest's page, coming and going, and of
// the choice on the card and in the dialog: run as TestShots says.
func TestShotsGuestSized(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	win, sh, publish := windowStageOf(t, geom.Sz(1100, 700))
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, "guestsized-"+name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	for id, text := range map[string]string{"p1": "bob@web:~/web$ npm run dev\r\n  ready in 312 ms\r\nbob@web:~/web$ ", "p2": "anna@desk:~$ tail -f log\r\n10:04 up\r\n10:05 ready\r\n"} {
		sh.Set(id, screen.Open(sessiontest.NewPrinted([]byte(text)), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
	}
	in := isIn("l1", "Bob", "p1")
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "npm run dev"}, {ID: "p2", Title: "log"}}, Focus: "p1", Marks: look.MarksOf(vt.DefaultPalette()),
		Layout: boxLayout(&app.Box{ID: "s", Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}}), Links: []app.WindowLink{in},
		LinkSetup: app.LinkSetup{Name: "Anna", Relay: "https://relay.example.com/k/0123456789abcdef"}}
	publish(st)
	settle()
	shot("before")
	tm := win.terms["p1"]
	tm.Shell().T.Hold(40, 12)
	st.Links = []app.WindowLink{sizedFor("l1", "Bob", "p1")}
	publish(st)
	for i := 1; i <= 4; i++ {
		frames(3)
		shot("coming-" + strconv.Itoa(i))
	}
	settle()
	shot("smaller")
	tm.Shell().T.Hold(140, 60)
	for i := 1; i <= 3; i++ {
		frames(4)
		shot("growing-" + strconv.Itoa(i))
	}
	settle()
	shot("larger")
	tm.Shell().T.Hold(40, 12)
	settle()
	st.Links = []app.WindowLink{in}
	publish(st)
	for i := 1; i <= 3; i++ {
		frames(3)
		shot("going-" + strconv.Itoa(i))
	}
	tm.Shell().T.Release()
	settle()
	shot("after")

	st.Links = []app.WindowLink{sizedFor("l1", "Bob", "p1")}
	publish(st)
	win.chips.order[0].do(lastUI)
	settle()
	shot("card")
	win.dialog.Close(lastUI)
	settle()
	st.Links = nil
	publish(st)
	settle()
	win.linkDialog(lastUI)
	settle()
	shot("dialog")
}
