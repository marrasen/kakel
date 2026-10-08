package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"

	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
)

// A pane dragged over the window from another lights it, and a drop
// moves the pane in.
func TestAPaneDroppedFromAnotherWindowMovesIn(t *testing.T) {
	_, _, publish := windowStage(t)
	publish(app.State{Window: 2, Panes: []app.Pane{{ID: "p1", Title: "Jobs", Kind: app.KindJobs}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	at := geom.Pt(450, 300)
	lastWindow.Input(gi.Drop{Pos: at, Data: app.PaneDrag{Pane: "p9", Window: 1}})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.PaneToWindow); !ok || in.Pane != "p9" {
		t.Fatalf("the drop sent %#v", in)
	}
	// Its own pane, dropped back on it, is no move.
	lastWindow.Input(gi.Drop{Pos: at, Data: app.PaneDrag{Pane: "p1", Window: 2}})
	lastWindow.Frame(time.Second / 60)
	select {
	case env := <-lastWindow.Client().Intents():
		if _, ok := env.Intent.(app.PaneToWindow); ok {
			t.Fatalf("its own pane dropped back sent %#v", env.Intent)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// A window with another pane dragged over it lights up.
func TestAWindowLightsUnderAPaneFromAnother(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Window: 2, Panes: []app.Pane{{ID: "p1", Title: "Jobs", Kind: app.KindJobs}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	if !win.paneDrop(gi.DragOver{Data: app.PaneDrag{Pane: "p9", Window: 1}}, lastUI) {
		t.Fatal("the window did not take a pane from another")
	}
	if win.paneDrop(gi.DragOver{Data: app.PaneDrag{Pane: "p1", Window: 2}}, lastUI) {
		t.Fatal("the window took its own pane")
	}
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.dropLit.Value() < 0.9 {
		t.Fatalf("the window is lit %.2f", win.dropLit.Value())
	}
}

// A click on a tile picks its pane.
func TestAClickOnATilePicksIt(t *testing.T) {
	win := switcherStage(t)
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	at := win.sw.tiles[1].box.Value().Center()
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.FocusPane); !ok || in.Pane != "p2" {
		t.Fatalf("the click sent %#v", in)
	}
}

// A tile dragged away lifts off, and let go outside every window asks
// for a window of its own, where the image was let go.
func TestATileLetGoOutsideAsksForAWindow(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	r := sw.tiles[1].box.Value()
	at := r.Min.Add(geom.Pt(10, 10))
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(20, 0))})
	lastWindow.Frame(time.Second / 60)
	if sw.carried == nil || sw.carried.id != "p2" {
		t.Fatal("the tile did not lift off")
	}
	sw.Handle(gi.DragEnd{Out: true, At: geom.Pt(1200, 300)}, lastUI)
	in, ok := nextIntent(t).(app.PaneToNewWindow)
	if !ok || in.Pane != "p2" || in.At != geom.Pt(1200, 300).Sub(sw.grab) || in.Size != sw.size {
		t.Fatalf("let go outside, the switcher sent %#v", in)
	}
	// It stays until the program says where the pane went.
	if len(sw.tiles) != 3 || sw.carried != nil {
		t.Fatalf("%d tiles are left, want 3", len(sw.tiles))
	}
}

// A tile let go over its own window goes back to its place.
func TestATileLetGoOverItsWindowGoesBack(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	r := sw.tiles[0].box.Value()
	at := r.Min.Add(geom.Pt(10, 10))
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(20, 20))})
	lastWindow.Frame(time.Second / 60)
	sw.Handle(gi.DragEnd{}, lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	if len(sw.tiles) != 3 || sw.tiles[0].fade.Value() < 0.95 || sw.carried != nil {
		t.Fatalf("%d tiles, the first at %.2f", len(sw.tiles), sw.tiles[0].fade.Value())
	}
}

// A tile another window took moves to that window's card once the
// program says the pane is there.
func TestATileTakenByAnotherWindowMoves(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	for _, id := range []string{"p1", "p2", "p3"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
	}
	all := []app.Pane{{ID: "p1", Title: "a"}, {ID: "p2", Title: "b"}, {ID: "p3", Title: "c", Window: 2}}
	st := app.State{Window: 1, Panes: all[:2], AllPanes: all, Stage: &app.Box{Pane: "p1"}, Focus: "p1",
		Overview: []app.OverWindow{
			{ID: 1, Tabs: []app.OverTab{{Group: 1, Box: &app.Box{Pane: "p1"}, Pane: "p1"}, {Group: 2, Box: &app.Box{Pane: "p2"}, Pane: "p2"}}, Front: 1},
			{ID: 2, Tabs: []app.OverTab{{Group: 3, Box: &app.Box{Pane: "p3"}, Pane: "p3"}}, Front: 3},
		}}
	publish(st)
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	sw := win.sw
	if len(sw.cards) != 2 || len(sw.tiles) != 3 {
		t.Fatalf("%d cards and %d tiles, want 2 and 3", len(sw.cards), len(sw.tiles))
	}
	at := sw.tileOf("p2").box.Value().Center()
	lastWindow.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary})
	lastWindow.Input(gi.PointerMove{Pos: at.Add(geom.Pt(30, 0))})
	lastWindow.Frame(time.Second / 60)
	sw.Handle(gi.DragEnd{Taken: true}, lastUI)
	// The program moves it into window 2.
	all[1].Window = 2
	st.Panes, st.AllPanes = all[:1], all
	st.Overview = []app.OverWindow{
		{ID: 1, Tabs: []app.OverTab{{Group: 1, Box: &app.Box{Pane: "p1"}, Pane: "p1"}}, Front: 1},
		{ID: 2, Tabs: []app.OverTab{{Group: 3, Box: &app.Box{Pane: "p3"}, Pane: "p3"}, {Group: 2, Box: &app.Box{Pane: "p2"}, Pane: "p2"}}, Front: 2},
	}
	publish(st)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	p2 := sw.tileOf("p2")
	if p2 == nil || p2.win != 2 || !sw.cards[1].box.Value().Contains(p2.box.Value().Center()) {
		t.Fatalf("p2 is not in window 2's card")
	}
}
