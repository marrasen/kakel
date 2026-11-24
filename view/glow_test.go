package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/look"

	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/paint"

	"github.com/marrasen/kakel/vt"
)

func TestASharedPaneGlows(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	marks := look.MarksOf(vt.DefaultPalette())
	st := app.State{Panes: []app.Pane{{ID: "p1", Title: "Terminal 1"}}, Layout: boxLayout(&app.Box{Pane: "p1"}), Focus: "p1", Marks: marks}
	ring := func() bool {
		for _, op := range lastWindow.Offscreen().Ops() {
			if r, ok := op.(*paint.RRectOp); ok && r.Stroke.Width == markWidth && r.Stroke.Color.R == marks.Agent.R && r.Stroke.Color.G == marks.Agent.G {
				return true
			}
		}
		return false
	}
	publish(st)
	if ring() {
		t.Fatal("a pane shared with nobody has a ring")
	}
	st.Share = app.Share{Panes: []app.SharedPane{{Pane: "p1"}}}
	publish(st)
	if !ring() {
		t.Fatal("a pane shared with an agent has no ring")
	}
	if !win.glowing {
		t.Fatal("the ring glows without frames to glow in")
	}
	st.Share = app.Share{}
	publish(st)
	for range 5 {
		lastWindow.Frame(time.Second / 10)
	}
	if ring() || win.glowing {
		t.Fatal("unshared, the pane still glows")
	}
}
