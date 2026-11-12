package view

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/vt"
)

// waitingState is a window holding one of a layout's panes, waiting as
// st says, beside a file manager.
func waitingState(st app.Waiting) app.State {
	return app.State{
		Panes: []app.Pane{{ID: "f1", Title: "home", Kind: app.KindFileManager},
			{ID: "p1", Title: "web", Kind: app.KindWaiting, Becomes: settings.LayoutTerminal, Machine: "s1"}},
		Layout: boxLayout(&app.Box{ID: "s1", A: &app.Box{Pane: "f1"}, B: &app.Box{Pane: "p1"}, Share: 0.4}),
		Focus:  "p1",
		Waits:  map[string]app.Waiting{"p1": st},
	}
}

// liveState is waitingState once the pane waited for has come: a
// terminal in its place.
func liveState() app.State {
	return app.State{
		Panes:  []app.Pane{{ID: "f1", Title: "home", Kind: app.KindFileManager}, {ID: "t1", Title: "web", Machine: "s1"}},
		Layout: boxLayout(&app.Box{ID: "s1", A: &app.Box{Pane: "f1"}, B: &app.Box{Pane: "t1"}, Share: 0.4}),
		Focus:  "t1",
	}
}

// openShell gives pane id a shell that printed out.
func openShell(t *testing.T, sh *screen.Shells, id, out string) {
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set(id, screen.Open(sessiontest.NewPrinted([]byte(out)), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get(id).T.Close() })
}

// A waiting pane that fails fades across to why, frame by frame: what
// it waited for stays on screen while the reason comes, and the buttons
// grow in under it rather than appear, the keyboard going to Retry.
// Retry and Close ask the program for what they say.
func TestAWaitingPaneFailsWithoutAPop(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(waitingState(app.Waiting{Doing: "Connecting to web…"}))
	settle()
	v := win.waitViews["p1"]
	if v == nil || lastUI.Focused() != gunim.Node(v) {
		t.Fatalf("the waiting pane is %v, the keyboard on %T", v, lastUI.Focused())
	}
	was := v.pages.Page()

	publish(waitingState(app.Waiting{Failed: "Couldn't connect to web", Why: "Connection refused."}))
	if _, there := lastUI.Bounds(was); !there {
		t.Fatal("what it waited for went at once")
	}
	if v.buttons == nil || lastUI.Focused() != gunim.Node(v.retry) {
		t.Fatalf("failed, the keyboard is on %T", lastUI.Focused())
	}
	var heights []float32
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		r, _ := lastUI.Bounds(v.buttons)
		heights = append(heights, r.Size().H)
	}
	full := heights[len(heights)-1]
	if full < 10 || heights[0] > full*0.6 {
		t.Fatalf("the buttons were %v tall at first, and %v at last", heights[0], full)
	}
	for i := 1; i < len(heights); i++ {
		if heights[i]-heights[i-1] > full*0.5 {
			t.Fatalf("the buttons jumped from %v to %v in a frame: %v", heights[i-1], heights[i], heights)
		}
	}
	if _, there := lastUI.Bounds(was); there {
		t.Fatal("what it waited for is still there")
	}

	drain()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter, Time: time.Now()})
	lastWindow.Frame(time.Second / 60)
	if in, ok := nextIntent(t).(app.RetryWaiting); !ok || in.Pane != "p1" {
		t.Fatalf("Enter on Retry sent %#v", in)
	}
}

// When the pane waited for comes, the waiting pane gives way to it
// frame by frame: it stays where it stood, over the pane that came,
// fading away, and then it is gone.
func TestAWaitingPaneGivesWayWithoutAPop(t *testing.T) {
	win, sh, publish := windowStage(t)
	publish(waitingState(app.Waiting{Doing: "Connecting to web…"}))
	settle()
	v := win.waitViews["p1"]
	stood, ok := lastUI.Bounds(v)
	if !ok {
		t.Fatal("the waiting pane is not drawn")
	}

	openShell(t, sh, "t1", "deploy@web:~$ ")
	publish(liveState())
	var shown []float32
	for range 180 {
		at, there := lastUI.Bounds(v)
		if !there {
			break
		}
		if at != stood {
			t.Fatalf("leaving, the waiting pane moved from %v to %v", stood, at)
		}
		shown = append(shown, v.shown.Value())
		lastWindow.Frame(time.Second / 60)
	}
	if len(shown) < 3 || shown[0] < 0.5 {
		t.Fatalf("the waiting pane went at once: %v", shown)
	}
	for i := 1; i < len(shown); i++ {
		if shown[i] > shown[i-1]+0.001 {
			t.Fatalf("the waiting pane came back as it left: %v", shown)
		}
	}
	if _, there := lastUI.Bounds(v); there || len(win.stage.leaving) != 0 {
		t.Fatalf("the waiting pane is still there: %v", shown)
	}
	if lastUI.Presence(win.bareNode("t1")) == gunim.Exiting {
		t.Fatal("the terminal is not on stage")
	}
}

// TestShotsWaiting writes a waiting pane waiting, failed, and frames of
// it giving way to the terminal it waited for, for looking at: run with
// KAKEL_SHOTS set to a folder, under a private X server.
func TestShotsWaiting(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	_, sh, publish := windowStageOf(t, geom.Sz(1000, 600))
	publish(waitingState(app.Waiting{Doing: "Connecting to web…"}))
	settle()
	shot("wait-1-waiting")
	// As the program words a server that refuses.
	publish(waitingState(app.Waiting{Failed: "Couldn't connect to web", Why: "Remote: connect to 10.0.0.12:22: dial tcp 10.0.0.12:22: connect: connection refused."}))
	for i := 1; i <= 3; i++ {
		frames(2)
		shot("wait-2-failing-" + strconv.Itoa(i))
	}
	settle()
	shot("wait-3-failed")
	publish(waitingState(app.Waiting{Doing: "Connecting to web…"}))
	settle()
	openShell(t, sh, "t1", "Welcome to web\r\ndeploy@web:~$ ")
	publish(liveState())
	for i := 1; i <= 5; i++ {
		frames(2)
		shot("wait-4-swap-" + strconv.Itoa(i))
	}
	settle()
	shot("wait-5-live")
}

// twoWaitingTabs is a window of two of a layout's panes on one server,
// as tabs of one group, saying st, with the keyboard on front.
func twoWaitingTabs(st app.Waiting, front string) app.State {
	return app.State{
		Panes: []app.Pane{{ID: "p1", Title: "web", Kind: app.KindWaiting, Machine: "s1"},
			{ID: "p2", Title: "logs", Kind: app.KindWaiting, Machine: "s1"}},
		Layout: boxLayout(&app.Box{Pane: front}, map[string]string{"p1": "p2", "p2": "p1"}[front]),
		Focus:  front,
		Waits:  map[string]app.Waiting{"p1": st, "p2": st},
	}
}

// Two waiting tabs of one server, both looked at, fail together: the one
// behind changes off stage, and shows why once it is brought back. Retry
// brings both back to waiting the same way.
func TestWaitingTabsBehindChangeOffStage(t *testing.T) {
	win, _, publish := windowStage(t)
	waiting := app.Waiting{Doing: "Connecting to web…"}
	failed := app.Waiting{Failed: "Couldn't connect to web", Why: "Connection refused."}
	publish(twoWaitingTabs(waiting, "p1"))
	settle()
	publish(twoWaitingTabs(waiting, "p2"))
	settle()
	if lastUI.Presence(win.waitViews["p1"]) != gunim.Exiting {
		t.Fatal("the tab behind is on stage")
	}

	publish(twoWaitingTabs(failed, "p2"))
	settle()
	publish(twoWaitingTabs(failed, "p1"))
	settle()
	if v := win.waitViews["p1"]; v.retry == nil || lastUI.Presence(v.retry) == gunim.Exiting {
		t.Fatal("brought back, the tab does not say why")
	}

	publish(twoWaitingTabs(waiting, "p1"))
	settle()
	publish(twoWaitingTabs(waiting, "p2"))
	settle()
	if v := win.waitViews["p2"]; v.retry != nil || lastUI.Presence(v) == gunim.Exiting {
		t.Fatal("retried, the tab still says it failed")
	}
}

// A waiting pane hidden by the pane beside it filling the window fails
// out of sight, and shows why as the window is shared again.
func TestAWaitingPaneBesideAMaximisedOne(t *testing.T) {
	win, _, publish := windowStage(t)
	st := waitingState(app.Waiting{Doing: "Connecting to web…"})
	st.Focus = "f1"
	publish(st)
	settle()
	win.run("pane.maximize", lastUI)
	settle()
	if lastUI.Presence(win.waitViews["p1"]) != gunim.Exiting {
		t.Fatal("the waiting pane shows beside the pane filling the window")
	}

	st.Waits = map[string]app.Waiting{"p1": {Failed: "Couldn't connect to web", Why: "Connection refused."}}
	publish(st)
	settle()
	win.run("pane.maximize", lastUI)
	settle()
	if v := win.waitViews["p1"]; v.retry == nil || lastUI.Presence(v.retry) == gunim.Exiting {
		t.Fatal("shared again, the pane does not say why")
	}
}
