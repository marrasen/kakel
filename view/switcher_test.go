package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/vt"
)

// switcherStage puts three terminal panes in a window, p1 on stage, and
// opens the switcher over them.
func switcherStage(t *testing.T) *Window {
	t.Helper()
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	var panes []app.Pane
	for _, id := range []string{"p1", "p2", "p3"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		panes = append(panes, app.Pane{ID: id, Title: "Terminal " + id})
	}
	publish(app.State{Panes: panes, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	if win.sw == nil {
		t.Fatal("the switcher did not open")
	}
	return win
}

// Picking a pane, the others fade as it grows, rather than standing in
// their places until the overview goes.
func TestThePanesNotPickedFadeAsThePickGrows(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	for range 20 {
		lastWindow.Frame(time.Second / 60)
	}
	for i, tl := range sw.tiles {
		if i == sw.picked {
			continue
		}
		if a := tl.fade.Value(); a > 0.05 {
			t.Fatalf("a third of a second after the pick, %s is still at %.2f", tl.id, a)
		}
	}
}

// A pane that is no terminal shows in the switcher too, drawn small as
// it was last drawn.
func TestTheSwitcherShowsAPaneThatIsNoTerminal(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p2", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p2").T.Close() })
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "Copies", Kind: app.KindCopies}, {ID: "p2", Title: "Terminal 2"}},
		Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	for range 10 {
		lastWindow.Frame(time.Second / 60)
	}
	if d := win.drawings["p1"]; d == nil || d.Recording().Empty() {
		t.Fatal("the help pane's drawing was not kept")
	}
	win.run("view.switcher", lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	small := false
	for _, op := range lastWindow.Offscreen().Ops() {
		if tx, ok := op.(*paint.TextOp); ok && tx.Transform.A < 0.9 && tx.Transform.A > 0 {
			small = true
		}
	}
	if !small {
		t.Fatal("the switcher shows no text drawn small: the help pane's tile is empty")
	}
}

// The pane picked is live the moment it is picked: it is asked onto the
// stage at once, with the keyboard, and grows while the switcher keeps
// the stage covered, and stays until it has grown.
func TestThePanePickedIsLiveAtOnce(t *testing.T) {
	win := switcherStage(t)
	sw := win.sw
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight})
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	in, ok := nextIntent(t).(app.FocusPane)
	if !ok || in.Pane != sw.tiles[sw.picked].id {
		t.Fatalf("at the pick, it asked for %#v", in)
	}
	// The stage stays covered while the pane grows: what grows is the
	// pane, not a copy over it.
	covered := false
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Rect == sw.stageAt && r.Fill.Solid.A == 0xff {
			covered = true
		}
	}
	if sw.stageAt.Empty() || !covered {
		t.Fatalf("growing, the stage at %v is not covered", sw.stageAt)
	}
	if !sw.tiles[sw.picked].box.Active() || lastUI.Presence(sw) != gunim.Exiting {
		t.Fatal("the switcher went before the pane grew into place")
	}
}

// Opening, the stage is covered from the first frame: the tile standing
// where the pane stands is the pane, and the pane does not show beside
// it, fading.
func TestTheSwitcherCoversTheStageAtOnce(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "p1", Title: "a", Kind: app.KindFileManager}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1"})
	for range 5 {
		lastWindow.Frame(time.Second / 60)
	}
	win.openSwitcher(lastUI)
	lastWindow.Frame(time.Second / 60)
	sw := win.sw
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Rect == sw.stageAt && r.Fill.Solid.A == 0xff {
			return
		}
	}
	t.Fatalf("on its first frame, the switcher leaves the stage at %v showing", sw.stageAt)
}

// In a split, a pane in All Panes starts as it stands, its tab drawn
// over it where its bar has it: no line of its own pushes the pane down,
// and the tab leaves from the bar's tab.
func TestASplitsPanesStartUnderTheirTabs(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{
		{ID: "p1", Title: "a", Kind: app.KindFileManager},
		{ID: "p2", Title: "b", Kind: app.KindFileManager},
	}, Layout: boxLayout(&app.Box{A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}), Focus: "p1"})
	for range 30 {
		lastWindow.Frame(time.Second / 60)
	}
	win.openSwitcher(lastUI)
	lastWindow.Frame(time.Second / 60)
	sw := win.sw
	th := lastUI.Theme()
	if len(sw.tiles) != 2 {
		t.Fatalf("All Panes shows %d panes of the split's two", len(sw.tiles))
	}
	for _, tl := range sw.tiles {
		pane, ok := win.dock.FinalRect(tl.id)
		if !ok {
			t.Fatalf("pane %s has no place", tl.id)
		}
		if got := tl.box.Value(); got != pane {
			t.Fatalf("pane %s starts at %v in All Panes, and stands at %v", tl.id, got, pane)
		}
		if !tl.barred {
			t.Fatalf("pane %s of a split starts with no tab over it", tl.id)
		}
		tab, _ := win.dock.TabRect(tl.id)
		found := false
		for _, op := range lastWindow.Offscreen().Ops() {
			if r, ok := op.(*paint.RRectOp); ok && r.Rect == tab && r.Fill.Solid == dock.TabFront.Get(th) {
				found = true
			}
		}
		if !found {
			t.Fatalf("pane %s's tab is not drawn at %v, where its bar has it", tl.id, tab)
		}
	}
}

// A file manager pane's caption names where it runs alone: the file
// manager names the folder, and the two would say the folder twice.
func TestAFileManagerPanesCaptionNamesOnlyItsMachine(t *testing.T) {
	win, _, _ := windowStage(t)
	if got := captionIn(win.machineList, app.Pane{ID: "p1", Title: "tmp", Kind: app.KindFileManager}); got != "This computer" {
		t.Fatalf("a file manager pane here is captioned %q", got)
	}
	if got := captionIn(win.machineList, app.Pane{ID: "p2", Title: "Terminal 1"}); got != "This computer: Terminal 1" {
		t.Fatalf("a terminal here is captioned %q", got)
	}
}
