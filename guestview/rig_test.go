package guestview

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/themes"
)

// stage is the page in a window of its own, shown the states a test
// gives it.
type stage struct {
	t      *testing.T
	win    *gunim.Window
	page   *page
	u      *gunim.UI
	shells *screen.Shells
	typed  map[string]*sessiontest.Printed
	st     State
	cfg    Config
}

// themeAt is one of kakel's themes.
func themeAt(t *testing.T, i int) look.Themed {
	t.Helper()
	th, err := look.Of(themes.Built()[i])
	if err != nil {
		t.Fatal(err)
	}
	return th
}

// newStage shows st in a window of a size.
func newStage(t *testing.T, size geom.Size, st State, change ...func(*Config)) *stage {
	t.Helper()
	th := themeAt(t, 0)
	s := &stage{t: t, shells: screen.NewShells(), typed: map[string]*sessiontest.Printed{}, st: st}
	s.st.Theme, s.st.Own = th, th.Plain
	s.cfg.CanFill = true
	for _, do := range change {
		do(&s.cfg)
	}
	s.win = gunimtest.NewWith(t, gunim.WindowOptions{Size: size, Theme: Own(th.Theme, s.cfg.Touch)})
	// UI.Bounds answers from the frame last painted.
	s.win.PaintWhenLooked(false)
	for _, p := range st.Panes {
		s.open(p)
	}
	gunim.RegisterView(s.win, "guest", func(st State) *page {
		s.page = newPage(s.shells, s.cfg, st)
		return s.page
	}, func(p *page, st State, u *gunim.UI) {
		s.u = u
		p.Update(st, u)
	})
	if err := s.win.Client().Mount(gunim.Root, "guest", "guest", s.st, topic); err != nil {
		t.Fatal(err)
	}
	s.frames(3)
	return s
}

// open starts a terminal for a pane, with a program that prints its ID.
func (s *stage) open(p guest.Pane) {
	if p.Kind != guest.KindTerminal || s.shells.Get(p.ID) != nil {
		return
	}
	prog := sessiontest.NewPrinted([]byte("this is " + p.ID + "\r\n$ "))
	s.typed[p.ID] = prog
	sh := screen.Open(prog, s.st.Theme.Palette, screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}})
	s.shells.Set(p.ID, sh)
	s.t.Cleanup(sh.Close)
	for end := time.Now().Add(5 * time.Second); sh.T.Text() == "" && time.Now().Before(end); {
		time.Sleep(time.Millisecond)
	}
}

// show shows the state as do changes it.
func (s *stage) show(do func(st *State)) {
	s.t.Helper()
	do(&s.st)
	for _, p := range s.st.Panes {
		s.open(p)
	}
	s.again()
}

// again shows the state again, with the terminals there are now.
func (s *stage) again() {
	s.t.Helper()
	if err := s.win.Client().Publish(topic, s.st); err != nil {
		s.t.Fatal(err)
	}
	s.frames(1)
}

// frames draws n frames.
func (s *stage) frames(n int) {
	for range n {
		s.win.Frame(time.Second / 60)
	}
}

// settle draws until nothing moves.
func (s *stage) settle() { s.frames(90) }

// intents takes what the page has sent.
func (s *stage) intents() []gunim.Intent {
	var out []gunim.Intent
	for len(s.win.Client().Intents()) > 0 {
		out = append(out, (<-s.win.Client().Intents()).Intent)
	}
	return out
}

// press presses a key.
func (s *stage) press(k gi.Key, mods gi.Mods) {
	s.win.Input(gi.KeyPress{Key: k, Mods: mods, Time: time.Now()})
	s.frames(2)
}

// typing types text.
func (s *stage) typing(text string) {
	s.win.Input(gi.TextInput{Text: text, Time: time.Now()})
	s.frames(2)
}

// sent checks what a pane's program was typed in all, which may be on its
// way to it.
func (s *stage) sent(id, want string) {
	s.t.Helper()
	for end := time.Now().Add(5 * time.Second); s.typed[id].Sent() != want && time.Now().Before(end); {
		time.Sleep(time.Millisecond)
	}
	if got := s.typed[id].Sent(); got != want {
		s.t.Errorf("%s was typed %q, want %q", id, got, want)
	}
}

// click clicks in the middle of a node.
func (s *stage) click(n gunim.Node) {
	s.t.Helper()
	box, ok := s.u.Bounds(n)
	if !ok {
		s.t.Fatalf("%T is not drawn", n)
	}
	s.clickAt(box.Center())
}

func (s *stage) clickAt(at geom.Point) {
	s.win.Input(gi.PointerDown{Pos: at, Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	s.frames(2)
	s.win.Input(gi.PointerUp{Pos: at, Button: gi.ButtonPrimary, Time: time.Now()})
	s.frames(2)
}

// shot writes what the window shows to the folder KAKEL_SHOTS names.
func (s *stage) shot(name string) {
	s.t.Helper()
	if dir := os.Getenv("KAKEL_SHOTS"); dir != "" {
		gunimtest.WritePNG(s.t, filepath.Join(dir, name+".png"), gunimtest.Pixels(s.t, s.win))
	}
}

// term is a terminal pane.
func termPane(id string) guest.Pane {
	return guest.Pane{ID: id, Title: id, Kind: guest.KindTerminal, Icon: "terminal", Cols: 80, Rows: 24}
}

// inWindow is the state of a guest in a window of terminals, in one
// group.
func inWindow(ids ...string) State {
	st := State{Page: PageWindow, HostName: "Anna", CanType: true, Layout: dock.NewLayout(true, ids...), HostIn: ids[0]}
	for _, id := range ids {
		st.Panes = append(st.Panes, termPane(id))
	}
	return st
}

// inSplit is one of p1 with a tab p2, beside p3 over a pane that is not
// shared.
func inSplit() State {
	st := inWindow("p1", "p2", "p3")
	l := dock.NewLayout(true, "p1", "p2")
	l.Add("p3", dock.Target{At: l.GroupOf("p1").ID, Side: dock.Right})
	l.Add("files", dock.Target{At: "p3", Side: dock.Bottom})
	l.Show("p1")
	l.SetShare(l.Windows[0].Root.ID, 0.6)
	st.Layout = l
	st.Panes = append(st.Panes, guest.Pane{ID: "files", Title: "Files", Kind: guest.KindOther, Icon: "filemanager"})
	return st
}
