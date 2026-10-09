package view

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/app"

	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/screen"

	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/vt"
)

func TestCtrlTabWalksThePanesByLastUse(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	var panes []app.Pane
	for _, id := range []string{"p1", "p2", "p3"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		panes = append(panes, app.Pane{ID: id, Title: "Terminal " + id})
	}
	// Used in the order p1, p2, p3.
	for _, id := range []string{"p1", "p2", "p3"} {
		publish(app.State{Panes: panes, Stage: &app.Box{Pane: id}, Focus: id})
	}
	for len(lastWindow.Client().Intents()) > 0 {
		<-lastWindow.Client().Intents()
	}
	step := func() string {
		t.Helper()
		lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: gi.ModControl})
		lastWindow.Frame(time.Second / 60)
		for {
			if f, ok := nextIntent(t).(app.FocusPane); ok {
				return f.Pane
			}
		}
	}
	if got := step(); got != "p2" {
		t.Fatalf("the first Ctrl+Tab went to %s, want the pane used before, p2", got)
	}
	if win.walkList == nil {
		t.Fatal("the walk shows no list")
	}
	// Its titles are on screen, one a pane.
	lastWindow.Frame(time.Second / 60)
	for i, l := range win.walkList.labels {
		if box, ok := lastUI.Bounds(l); !ok || box.Empty() {
			t.Fatalf("the walk's title %d, %q, is not on screen", i, l.Text)
		}
	}
	if got := step(); got != "p1" {
		t.Fatalf("the second went to %s, want p1", got)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl})
	lastWindow.Frame(time.Second / 60)
	if win.walk != nil || win.walkList != nil {
		t.Fatal("letting go of Ctrl left the walk going")
	}
	if win.recent[0] != "p1" {
		t.Fatalf("the walk ended on p1, and the most recent is %s", win.recent[0])
	}
	// From the palette, with no Ctrl to let go of, it takes one step.
	win.run("pane.next", lastUI)
	for {
		if f, ok := nextIntent(t).(app.FocusPane); ok {
			if f.Pane != "p3" {
				t.Fatalf("from the palette it went to %s, want p3", f.Pane)
			}
			break
		}
	}
	if win.walk != nil {
		t.Fatal("run from the palette, the walk goes on")
	}
}

// Another program taking the keyboard mid-walk, as a screenshot tool
// does, ends the walk: no release of Ctrl would come.
func TestAWalkEndsWhenTheWindowLosesTheKeyboard(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	var panes []app.Pane
	for _, id := range []string{"p1", "p2"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
		panes = append(panes, app.Pane{ID: id, Title: "Terminal " + id})
	}
	for _, id := range []string{"p1", "p2"} {
		publish(app.State{Panes: panes, Stage: &app.Box{Pane: id}, Focus: id})
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: gi.ModControl | gi.ModShift})
	lastWindow.Frame(time.Second / 60)
	if win.walk == nil {
		t.Fatal("Ctrl+Shift+Tab started no walk")
	}
	lastWindow.Input(driver.WindowFocus{Focused: false})
	lastWindow.Frame(time.Second / 60)
	if win.walk != nil || win.walkList != nil {
		t.Fatal("with the keyboard gone to another program, the walk goes on")
	}
}

// Walking to a pane that sits in a split rings it, the ring sliding from
// the pane left, and the ring goes once Ctrl is let go of.
func TestCtrlTabRingsAPaneInASplit(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	for _, id := range []string{"p1", "p2"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
	}
	panes := []app.Pane{{ID: "p1", Title: "a"}, {ID: "p2", Title: "b"}}
	split := &app.Box{ID: "s1", A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}, Share: 0.5}
	st := app.State{Panes: panes, Stage: split, Focus: "p1"}
	publish(st)
	st.Focus = "p2"
	publish(st)
	st.Focus = "p1"
	publish(st)
	frames := func() {
		for range 40 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	frames()
	lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: gi.ModControl})
	lastWindow.Frame(time.Second / 60)
	// The program puts p2 in front, in the same split.
	st.Focus = "p2"
	publish(st)
	frames()
	m := win.walkMark
	want, _ := win.standsAt("p2", lastUI)
	if m == nil || m.shown.Value() < 0.9 || m.box.Value() != want {
		t.Fatalf("walked to p2, the ring is %+v, want it shown round %v", m, want)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl})
	lastWindow.Frame(time.Second / 60)
	if win.walkMark != nil {
		t.Fatal("letting go of Ctrl left the ring")
	}
}

// Ctrl+Tab in a window of one pane, as the Settings window is, starts no
// walk and leaves nothing over the window: a ring left there took every
// click, the title bar's too.
func TestCtrlTabInAWindowOfOnePaneLeavesNothing(t *testing.T) {
	win, _, publish := windowStage(t)
	publish(app.State{Panes: []app.Pane{{ID: "s", Title: "Settings", Kind: app.KindSettings}}, Stage: &app.Box{Pane: "s"}, Focus: "s"})
	for range 3 {
		lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: gi.ModControl})
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl})
	lastWindow.Frame(time.Second / 60)
	if win.walk != nil || win.walkMark != nil || win.walkList != nil {
		t.Fatalf("one pane, the walk left %v, a ring %v and a list %v", win.walk, win.walkMark != nil, win.walkList != nil)
	}
	// And a ring, however it came to be, takes no clicks.
	if (&walkMark{}).Covers(geom.Pt(10, 10)) {
		t.Fatal("the ring takes the pointer")
	}
}

// A walk that reaches a pane taking no keyboard of its own, as Settings
// and the theme editor are, ends when Ctrl is let go of: the keyboard is
// then on nothing, and the release reached no one, which left the list
// on screen.
func TestAWalkIntoSettingsEndsOnCtrlUp(t *testing.T) {
	win, sh, publish := windowStage(t)
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	sh.Set("p1", screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
	t.Cleanup(func() { _ = sh.Get("p1").T.Close() })
	panes := []app.Pane{{ID: "p1", Title: "Terminal 1"}, {ID: "s", Title: "Settings", Kind: app.KindSettings}}
	for _, id := range []string{"s", "p1"} {
		publish(app.State{Panes: panes, Stage: &app.Box{Pane: id}, Focus: id})
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: gi.ModControl})
	lastWindow.Frame(time.Second / 60)
	if win.walk == nil {
		t.Fatal("Ctrl+Tab started no walk")
	}
	// The program puts Settings in front.
	publish(app.State{Panes: panes, Stage: &app.Box{Pane: "s"}, Focus: "s"})
	for range 10 {
		lastWindow.Frame(time.Second / 60)
	}
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyLeftControl})
	lastWindow.Frame(time.Second / 60)
	if win.walk != nil || win.walkList != nil {
		t.Fatal("letting go of Ctrl on Settings left the walk going")
	}
}
