package app

import (
	"sync"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/internal/testhome"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"
)

// The scale the window was at as kakel exits is kept with its place,
// and the next start opens it at that scale, for gunim to fit it to the
// scale the screen has then.
func TestTheWindowsScaleIsKeptAndGivenBack(t *testing.T) {
	testhome.New(t)
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	path, err := settings.Path()
	if err != nil {
		t.Fatal(err)
	}
	if a.settings, err = settings.Load(path); err != nil {
		t.Fatal(err)
	}
	a.cur.gw = w
	want := driver.Placement{Bounds: geom.Rc(10, 20, 800, 600), CoordsPerLogical: 1.5}
	a.placed = func(*ownWin) (driver.Placement, bool) { return want, true }
	a.leave()
	if got := (Options{}).WindowPlace(); got == nil || *got != want {
		t.Fatalf("started again, the window opens at %+v, want %+v", got, want)
	}
}

// A layout keeps the scale its window was saved at, and opens its
// window at it.
func TestALayoutKeepsItsWindowsScale(t *testing.T) {
	a, opened := openingApp(t)
	var mu sync.Mutex
	var asked *driver.Placement
	open := a.openWindow
	a.openWindow = func(gw *gunim.Window, at geom.Point, size geom.Size, place *driver.Placement) (gunim.Client, *gunim.Window, error) {
		mu.Lock()
		asked = place
		mu.Unlock()
		return open(gw, at, size, place)
	}
	kept := keep(t, a, settings.SavedLayout{
		Name:  "Work",
		Root:  item("p1"),
		Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal, Command: plainShell()}},
		Place: &settings.WindowPlace{X: 10, Y: 20, W: 800, H: 600, Scale: 1.5},
	})
	w := opens(t, a, opened, kept.ID)
	mu.Lock()
	got := asked
	mu.Unlock()
	if got == nil || got.CoordsPerLogical != 1.5 {
		t.Fatalf("the layout's window opened at %+v", got)
	}
	a.placed = func(*ownWin) (driver.Placement, bool) {
		return driver.Placement{Bounds: geom.Rc(10, 20, 800, 600), CoordsPerLogical: 2}, true
	}
	again, _, _, err := a.saveLayout(SaveLayout{Window: w.id, Name: "Again"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Place == nil || again.Place.Scale != 2 {
		t.Fatalf("saved again, the layout's place is %+v", again.Place)
	}
}

// A file manager window keeps the scale it closed at. Opened after the
// scale changed, its place is brought to the scale the screen has now
// before it is stepped clear of a window open there, so it does not land
// on one.
func TestAFilesWindowKeepsItsScaleAndStepsClearAfterItChanged(t *testing.T) {
	a, _ := agentApp(t)
	a.settings = mustSettings(t)
	a.files = newFakeFiles(t)
	var mu sync.Mutex
	var asked []*driver.Placement
	a.openWindow = func(_ *gunim.Window, _ geom.Point, _ geom.Size, place *driver.Placement) (gunim.Client, *gunim.Window, error) {
		mu.Lock()
		asked = append(asked, place)
		mu.Unlock()
		w := gunimtest.New(t, geom.Sz(400, 300), nil)
		return w.Client(), w, nil
	}
	where := map[*ownWin]driver.Placement{}
	a.placed = func(w *ownWin) (driver.Placement, bool) {
		p, ok := where[w]
		return p, ok
	}
	opened := func(i int) *driver.Placement {
		mu.Lock()
		defer mu.Unlock()
		if i < len(asked) {
			return asked[i]
		}
		return nil
	}
	open := func(n int) *ownWin {
		t.Helper()
		before := a.cur
		a.openFolder(t.TempDir())
		waitFor(t, a, "the window opens", func() bool {
			mu.Lock()
			got := len(asked)
			mu.Unlock()
			panes := a.panesIn(a.cur)
			return got == n && a.cur != before && len(panes) == 1 && panes[0].Kind == KindFileManager
		})
		return a.cur
	}

	first := open(1)
	where[first] = driver.Placement{Bounds: geom.Rc(100, 120, 1000, 700), CoordsPerLogical: 1}
	a.closeWindow(first)
	if p, _ := a.settings.FilesWindow(); p.Scale != 1 {
		t.Fatalf("the files window was kept at %+v", p)
	}

	// The screen is at twice the scale now.
	a.monitors = func() []driver.Monitor {
		return []driver.Monitor{{Bounds: geom.Rc(0, 0, 3840, 2160), WorkArea: geom.Rc(0, 0, 3840, 2160), Scale: 2, CoordsPerLogical: 2, Primary: true}}
	}
	second := open(2)
	at := opened(1)
	if at == nil || at.Bounds != geom.Rc(200, 240, 2000, 1400) || at.CoordsPerLogical != 2 {
		t.Fatalf("after the scale changed, the next opened at %+v", at)
	}
	where[second] = *at
	open(3)
	if p := opened(2); p == nil || p.Bounds != geom.Rc(200+cascade, 240+cascade, 2000, 1400) {
		t.Fatalf("one opened over another at %+v, not a step down and right", p)
	}
}
