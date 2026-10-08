package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/theme"
)

// overviewStage is All Panes' own window over a screen 1600 by 1000,
// showing two windows: p1 alone in the first, at 40,40, and in the
// second, at 700,300, p2 and p3 split on one tab and p4 on another, in
// front.
func overviewStage(t *testing.T) (*gunim.Window, *Overview, app.OverState) {
	t.Helper()
	w := gunimtest.New(t, geom.Sz(1600, 1000), nil)
	sh := screen.NewShells()
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	for _, id := range []string{"p1", "p2", "p3", "p4"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
	}
	var o *Overview
	gunim.RegisterView(w, "overview", func(app.OverState) *Overview {
		o = NewOverview(sh, Shortcuts())
		return o
	}, func(o *Overview, st app.OverState, u *gunim.UI) {
		overviewUI = u
		o.Update(st, u)
	})
	c := w.Client()
	if err := c.Mount(gunim.Root, "overview", "overview", app.OverState{}, app.OverviewTopic); err != nil {
		t.Fatal(err)
	}
	split := &app.Box{ID: "s1", A: &app.Box{Pane: "p2"}, B: &app.Box{Pane: "p3"}, Share: 0.5}
	st := app.OverState{
		Windows: []app.OverWindow{
			{ID: 1, Tabs: []app.OverTab{{Group: 1, Box: &app.Box{Pane: "p1"}, Pane: "p1"}}, Front: 1,
				Stage: geom.Sz(600, 400), StageAt: geom.Rc(0, 40, 600, 400), Size: geom.Sz(600, 440), Home: geom.Rc(40, 40, 600, 440)},
			{ID: 2, Tabs: []app.OverTab{{Group: 2, Box: split, Pane: "p2"}, {Group: 3, Box: &app.Box{Pane: "p4"}, Pane: "p4"}}, Front: 3,
				Stage: geom.Sz(800, 500), StageAt: geom.Rc(0, 40, 800, 500), Size: geom.Sz(800, 540), Home: geom.Rc(700, 300, 800, 540)},
		},
		Panes: []app.Pane{{ID: "p1", Title: "one", Window: 1}, {ID: "p2", Title: "two", Window: 2}, {ID: "p3", Title: "three", Window: 2}, {ID: "p4", Title: "four", Window: 2}},
		Front: 2, Focus: "p4",
	}
	if err := c.Publish(app.OverviewTopic, st); err != nil {
		t.Fatal(err)
	}
	w.Frame(time.Second / 60)
	if o == nil || o.sw == nil {
		t.Fatal("All Panes did not open")
	}
	return w, o, st
}

// overviewIntent is the next intent All Panes' own window sends, past
// those of type skip.
func overviewIntent(t *testing.T, w *gunim.Window) gunim.Intent {
	t.Helper()
	for {
		select {
		case env := <-w.Client().Intents():
			return env.Intent
		case <-time.After(2 * time.Second):
			t.Fatal("All Panes sent nothing")
			return nil
		}
	}
}

// Each window's card starts where the window stands on the screen, its
// pane in front where it stands in it, and they shrink from there into
// cards side by side, with the ring on the pane with the keyboard.
func TestAllPanesCardsStartWhereTheirWindowsStand(t *testing.T) {
	w, o, st := overviewStage(t)
	sw := o.sw
	for i, c := range sw.cards {
		if got := c.box.Value(); got != st.Windows[i].Home {
			t.Fatalf("window %d's card starts at %v, want %v", c.win, got, st.Windows[i].Home)
		}
	}
	// p4 is in front in the second window, on its stage, its cells in
	// from the edges as a terminal's are.
	pad := geom.Uniform(termPadding.Get(lastTheme(w)))
	if got, want := sw.tileOf("p4").box.Value(), geom.Rc(700, 340, 800, 500).Inset(pad); got != want {
		t.Fatalf("p4 starts at %v, want %v", got, want)
	}
	for range 90 {
		w.Frame(time.Second / 60)
	}
	a, b := sw.cards[0].box.Value(), sw.cards[1].box.Value()
	if a == st.Windows[0].Home || a.Max.X > b.Min.X && b.Max.X > a.Min.X && a.Max.Y > b.Min.Y && b.Max.Y > a.Min.Y || !geom.Rc(0, 0, 1600, 1000).Contains(b.Max.Sub(geom.Pt(1, 1))) {
		t.Fatalf("the cards came to %v and %v", a, b)
	}
	if hot := sw.tiles[sw.hot]; hot.id != "p4" || hot.ring.Value() < 0.9 {
		t.Fatalf("the ring is on %s", hot.id)
	}
}

// Escape sends every window's panes in front back where they stand, and
// All Panes says it is done once they are there.
func TestAllPanesEscapeGoesBackAndIsDone(t *testing.T) {
	w, o, _ := overviewStage(t)
	sw := o.sw
	for range 90 {
		w.Frame(time.Second / 60)
	}
	w.Input(gi.KeyPress{Key: gi.KeyEscape})
	for range 90 {
		w.Frame(time.Second / 60)
	}
	if got, want := sw.tileOf("p1").box.Value(), geom.Rc(40, 80, 600, 400).Inset(geom.Uniform(termPadding.Get(lastTheme(w)))); got != want {
		t.Fatalf("p1 went back to %v, want %v", got, want)
	}
	if in, ok := overviewIntent(t, w).(app.OverviewDone); !ok {
		t.Fatalf("All Panes sent %#v", in)
	}
}

// A pane picked in a tab not in front gets the keyboard, grows into its
// place in its split on its window's stage, and All Panes is done once
// it has.
func TestAllPanesPickedPaneGrowsIntoItsWindow(t *testing.T) {
	w, o, _ := overviewStage(t)
	sw := o.sw
	for range 90 {
		w.Frame(time.Second / 60)
	}
	at := sw.tileOf("p3").box.Value().Center()
	w.Input(gi.PointerMove{Pos: at})
	w.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	w.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary})
	w.Frame(time.Second / 60)
	if in, ok := overviewIntent(t, w).(app.FocusPane); !ok || in.Pane != "p3" {
		t.Fatalf("the click sent %#v", in)
	}
	for range 90 {
		w.Frame(time.Second / 60)
	}
	got := sw.tileOf("p3").box.Value()
	if got.Min.X < 700+380 || got.Max.X > 1500 || got.Min.Y < 340 || got.Max.Y > 840 {
		t.Fatalf("p3 grew to %v, not its half of the second window's stage", got)
	}
	if in, ok := overviewIntent(t, w).(app.OverviewDone); !ok {
		t.Fatalf("All Panes sent %#v", in)
	}
}

// overviewUI is the UI of the window overviewStage made last.
var overviewUI *gunim.UI

// lastTheme is the theme the window overviewStage made last draws in.
func lastTheme(*gunim.Window) *theme.Live { return overviewUI.Theme() }
