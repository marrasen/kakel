package guestview

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/guest"
)

// turns shows each frame of a change, for a person to look at, and checks
// that the two pages are never seen together, and that neither is there
// at once.
func turns(s *stage, name string, do func(st *State)) {
	s.t.Helper()
	before := s.page.pages.Page()
	s.show(func(st *State) { do(st) })
	after := s.page.pages.Page()
	if before == after {
		s.t.Fatalf("%s: the page is the same", name)
	}
	going, coming := 0, 0
	for i := range 60 {
		if s.u.Drawn(before) {
			going++
		}
		if s.u.Drawn(after) {
			coming++
		}
		if i%4 == 0 && i < 40 {
			s.shot(fmt.Sprintf("%s-%02d", name, i))
		}
		s.frames(1)
	}
	if going < 3 {
		s.t.Errorf("%s: the page before went in %d frames", name, going)
	}
	if coming == 0 {
		s.t.Errorf("%s: the next never came", name)
	}
	s.settle()
	s.shot(name + "-done")
}

// Every page gives way to the next as it fades, whichever comes after
// which.
func TestThePagesTurn(t *testing.T) {
	s := newStage(t, geom.Sz(900, 600), State{Page: PageJoin, Name: "Bo"})
	s.shot("turn-join")
	turns(s, "turn-waiting", func(st *State) { st.Page = PageWaiting })
	turns(s, "turn-window", func(st *State) {
		th, own := st.Theme, st.Own
		*st = inSplit()
		st.Theme, st.Own = th, own
	})
	s.settle()
	turns(s, "turn-over", func(st *State) { st.Page, st.Over = PageOver, overOf(guest.ReasonReplaced, "Anna") })
	turns(s, "turn-back", func(st *State) { st.Page, st.Lost = PageWaiting, true })
	turns(s, "turn-waiting-again", func(st *State) { st.Lost = false })
	turns(s, "turn-refused", func(st *State) { st.Page, st.Over = PageOver, overOf(guest.ReasonRefused, "") })
}

// Enter and the button both join, as whatever name is there.
func TestJoining(t *testing.T) {
	s := newStage(t, geom.Sz(390, 700), State{Page: PageJoin})
	join := partOf[*widget.Button](s, s.page.card.body)
	trouble := partOf[*widget.Fold](s, s.page.card.body)
	// Nobody joins with no name.
	s.press(gi.KeyEnter, 0)
	s.settle()
	if in := s.intents(); len(in) != 0 || !trouble.Open() {
		t.Errorf("with no name it asked for %v, and says why: %v", in, trouble.Open())
	}
	s.typing("Bo ")
	s.settle()
	if trouble.Open() {
		t.Error("it still says what was missing")
	}
	s.press(gi.KeyEnter, 0)
	if in := s.intents(); len(in) != 1 || in[0] != (Join{"Bo"}) {
		t.Errorf("Enter asked for %v", in)
	}
	s.show(func(st *State) { st.Busy = true })
	s.settle()
	s.shot("join-busy")
	s.click(join)
	s.press(gi.KeyEnter, 0)
	if in := s.intents(); len(in) != 0 {
		t.Errorf("while it joins it asked for %v", in)
	}
	s.show(func(st *State) {
		st.Busy, st.Trouble = false, "Couldn't reach the host. Check the network, then retry."
	})
	s.settle()
	s.shot("join-trouble")
	s.click(join)
	if in := s.intents(); len(in) != 1 || in[0] != (Join{"Bo"}) {
		t.Errorf("the button asked for %v", in)
	}
}

// partOf is the first part of a card of a type.
func partOf[N gunim.Node](s *stage, b *cardBody) N {
	s.t.Helper()
	for _, it := range b.items {
		if n, ok := it.node.(N); ok {
			return n
		}
	}
	var none N
	s.t.Fatalf("no %T", none)
	return none
}

// The pages that end in something to do ask for it, with the keyboard
// too, and those that wait can be left.
func TestWhatAPageOffers(t *testing.T) {
	s := newStage(t, geom.Sz(600, 500), State{Page: PageOver, Over: overOf(guest.ReasonReplaced, "")})
	s.settle()
	s.press(gi.KeyEnter, 0)
	if in := s.intents(); len(in) != 1 || in[0] != (Do{ActHere}) {
		t.Errorf("Enter asked for %v", in)
	}
	s.show(func(st *State) { st.Page = PageWaiting })
	s.settle()
	s.click(partOf[*widget.Button](s, s.page.card.body))
	if in := s.intents(); len(in) != 1 || in[0] != (Leave{}) {
		t.Errorf("Cancel asked for %v", in)
	}
	for _, o := range []Over{overBroken, overGone(true), overUnreached, overOf(guest.ReasonUsed, "")} {
		s.show(func(st *State) { st.Page, st.Over = PageOver, o })
		s.settle()
	}
}

// A card fits a page however small, and what does not scrolls.
func TestACardOnASmallPage(t *testing.T) {
	s := newStage(t, geom.Sz(320, 200), State{Page: PageJoin})
	join := partOf[*widget.Button](s, s.page.card.body)
	box, _ := s.u.Bounds(join)
	if box.Min.X < 0 || box.Max.X > 320 {
		t.Errorf("Join stands at %v", box)
	}
	s.win.Input(gi.Scroll{Pos: geom.Pt(160, 100), Delta: geom.Pt(0, -600), Notches: geom.Pt(0, -15)})
	s.settle()
	if box, _ = s.u.Bounds(join); box.Max.Y > 200 {
		t.Errorf("scrolled down, Join stands at %v", box)
	}
	// A phone's keyboard takes room too.
	s.win.Offscreen().Resize(geom.Sz(390, 700))
	s.win.Offscreen().SetKeyboardCover(300)
	s.settle()
	s.shot("join-keyboard")
	if box, _ = s.u.Bounds(join); box.Max.Y > 400 {
		t.Errorf("over the keyboard, Join stands at %v", box)
	}
}

// What is typed reaches the pane the guest is in, and the page the
// browser's keys while it is there.
func TestTypingInTheWindow(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	s.typing("ls")
	s.press(gi.KeyEnter, 0)
	s.sent("p1", "ls\r")
	in := s.intents()
	if !slices.Contains(in, gunim.Intent(In{"p1"})) {
		t.Errorf("it said %v", in)
	}
	if !slices.ContainsFunc(in, func(i gunim.Intent) bool {
		sh, ok := i.(Shown)
		return ok && slices.Equal(sh.Panes, []string{"p1", "p3", "files"})
	}) {
		t.Errorf("of what is on the screen it said %v", in)
	}
	off := s.win.Offscreen()
	if !off.KeysClaimed() || !off.CloseAsks() || off.Title() != "p1 — kakel" {
		t.Errorf("the keys are claimed: %v, a close asks: %v, the title is %q", off.KeysClaimed(), off.CloseAsks(), off.Title())
	}
	// Another pane clicked.
	s.click(s.page.win.panes["p3"])
	s.typing("x")
	s.sent("p3", "x")
	if in := s.intents(); !slices.Contains(in, gunim.Intent(In{"p3"})) {
		t.Errorf("it said %v", in)
	}
	s.show(func(st *State) { st.Page, st.Over = PageOver, overBroken })
	s.settle()
	if off.CloseAsks() || off.Title() != "kakel" {
		t.Errorf("at the end a close asks: %v, the title is %q", off.CloseAsks(), off.Title())
	}
}

// A guest who may only watch types nothing, and is told why; copying and
// scrolling work.
func TestWatching(t *testing.T) {
	st := inWindow("p1")
	st.CanType = false
	s := newStage(t, geom.Sz(800, 600), st)
	s.settle()
	b := s.page.win.bar
	hinted := func() bool {
		on := b.swing.Active() && s.page.win.panes["p1"].warned.Target() == 1
		for i := range 12 {
			if on {
				s.shot(fmt.Sprintf("hint-%02d", i))
			}
			s.frames(4)
		}
		s.settle()
		if v := s.page.win.panes["p1"].warned.Value(); v > 0.01 || b.swing.Active() {
			t.Errorf("the hint stays: the edge is lit %v", v)
		}
		return on
	}
	s.press(gi.KeyLeftShift, gi.ModShift)
	s.press(gi.KeyPageUp, gi.ModShift)
	if hinted() {
		t.Error("a key that types nothing is answered with a shake")
	}
	for what, do := range map[string]func(){
		"a letter": func() { s.typing("x") },
		"Enter":    func() { s.press(gi.KeyEnter, 0) },
		"a paste": func() {
			s.u.SetClipboard("two\nlines\n")
			s.press(gi.KeyV, gi.ModControl|gi.ModShift)
		},
	} {
		do()
		if s.page.dialog != nil {
			t.Errorf("%s asks something", what)
		}
		if !hinted() {
			t.Errorf("%s is met with silence", what)
		}
	}
	s.sent("p1", "")
	s.shot("watching")
	sh := s.shells.Get("p1")
	sh.T.SelectAll()
	s.press(gi.KeyC, gi.ModControl|gi.ModShift)
	if got := s.u.Clipboard(); !strings.Contains(got, "this is p1") {
		t.Errorf("copied is %q", got)
	}
	if s.page.win.panes["p1"].term.TakesText() {
		t.Error("a phone's keyboard comes up for nothing")
	}
	s.show(func(st *State) { st.CanType = true })
	s.settle()
	s.typing("x")
	s.sent("p1", "x")
	if !s.u.FocusTakesText() {
		t.Error("once it may type, a phone's keyboard does not come up")
	}
}

// The panes follow the host's until the guest picks one, and then the
// guest's stay as the host's move.
func TestFollowingTheHost(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	d := s.page.win.dock
	front := func(show, in string) {
		s.show(func(st *State) {
			st.Layout = st.Layout.Clone()
			st.Layout.Show(show)
			st.HostIn = in
		})
		s.settle()
	}
	front("p2", "p2")
	if got := d.Shown(); !slices.Equal(got, []string{"p2", "p3", "files"}) || d.Active() != "p2" {
		t.Errorf("after the host, it shows %v and is in %s", got, d.Active())
	}
	s.typing("a")
	s.sent("p2", "a")
	// Having typed, the guest stays where it is: its next key goes
	// nowhere else.
	front("p1", "p1")
	s.typing("b")
	s.sent("p2", "ab")
	if d.Active() != "p2" {
		t.Errorf("the guest is in %s", d.Active())
	}
	if got := d.Shown(); !slices.Equal(got, []string{"p2", "p3", "files"}) {
		t.Errorf("the guest's tabs went: %v", got)
	}
	// A tab the host opens comes, and one it closes goes.
	s.show(func(st *State) {
		st.Panes = append(st.Panes, termPane("p4"))
		st.Layout = st.Layout.Clone()
		st.Layout.Add("p4", dock.Target{At: st.Layout.GroupOf("p1").ID})
	})
	s.settle()
	if a := d.Arrangement(); a.Node("p4") == nil || !slices.Equal(d.Shown(), []string{"p2", "p3", "files"}) {
		t.Errorf("with p4 it shows %v", d.Shown())
	}
	s.show(func(st *State) {
		st.Panes = slices.DeleteFunc(slices.Clone(st.Panes), func(p guest.Pane) bool { return p.ID == "p2" })
		st.Layout = st.Layout.Clone()
		st.Layout.Mend(func(id string) bool { return id != "p2" })
	})
	s.settle()
	if slices.Contains(d.Shown(), "p2") || s.page.win.panes["p2"] != nil {
		t.Errorf("without p2 it shows %v", d.Shown())
	}
	// A tab picked is the guest's too.
	at, ok := d.TabRect("p1")
	if !ok {
		t.Fatal("p1 has no tab")
	}
	box, _ := s.u.Bounds(d)
	s.clickAt(at.Center().Add(box.Min))
	s.settle()
	if !slices.Contains(d.Shown(), "p1") || d.Active() != "p1" {
		t.Errorf("after its tab was picked it shows %v, and is in %s", d.Shown(), d.Active())
	}
}

// A guest that picked a pane of its own has a way back to following the
// host, which comes and goes as it moves; the bar and the browser's tab
// are called after the pane the guest is in.
func TestFollowingTheHostAgain(t *testing.T) {
	for name, size := range map[string]geom.Size{"wide": geom.Sz(1000, 700), "narrow": geom.Sz(390, 700)} {
		t.Run(name, func(t *testing.T) {
			s := newStage(t, size, inSplit())
			s.settle()
			w := s.page.win
			b, d := w.bar, w.dock
			way, other := b.follow, b.followIcon
			if size.W < 600 {
				way, other = other, way
			}
			titled := func(want string) {
				t.Helper()
				label, _ := b.title.Page().(*widget.Label)
				if label == nil || label.Text != want || s.win.Offscreen().Title() != want+" — kakel" {
					t.Errorf("in %s, the bar says %v and the tab %q", d.Active(), label, s.win.Offscreen().Title())
				}
			}
			// moves draws the way back as it comes or goes, and reports
			// in how many frames it stood between.
			moves := func() int {
				between, full := 0, float32(0)
				for range 60 {
					s.frames(1)
					box, _ := s.u.Bounds(way)
					if wide := box.Size().W; wide > 1 && (full == 0 || wide != full) {
						between++
					}
					if !way.Open() {
						continue
					}
					full = max(full, box.Size().W)
				}
				return between
			}
			host := func(id string) {
				s.show(func(st *State) {
					st.Layout = st.Layout.Clone()
					st.Layout.Show(id)
					st.HostIn = id
				})
			}
			if way.Open() || other.Open() {
				t.Error("following, there is a way back")
			}
			titled("p1")
			// The title fades from one to the next.
			was := b.title.Page()
			host("p3")
			going := 0
			for range 30 {
				if s.u.Drawn(was) {
					going++
				}
				s.frames(1)
			}
			if going < 3 {
				t.Errorf("the title before went in %d frames", going)
			}
			s.settle()
			titled("p3")
			s.intents()

			// Typed in, the pane is the guest's own.
			s.typing("x")
			s.sent("p3", "x")
			if in := s.intents(); !slices.Contains(in, gunim.Intent(Follows{false})) {
				t.Errorf("typing, it said %v", in)
			}
			if n := moves(); !way.Open() || other.Open() || n < 3 {
				t.Errorf("the way back is there: %v, and came in %d frames", way.Open(), n)
			}
			host("p1")
			s.settle()
			if d.Active() != "p3" {
				t.Errorf("the host moved, and the guest is in %s", d.Active())
			}
			titled("p3")

			s.click(way)
			if in := s.intents(); !slices.Contains(in, gunim.Intent(Follows{true})) {
				t.Errorf("following again, it said %v", in)
			}
			if n := moves(); way.Open() || n < 3 {
				t.Errorf("following again, the way back is there: %v, and went in %d frames", way.Open(), n)
			}
			s.settle()
			if d.Active() != "p1" || !slices.Contains(d.Shown(), "p1") {
				t.Errorf("following again, it shows %v and is in %s", d.Shown(), d.Active())
			}
			titled("p1")
			host("p2")
			s.settle()
			if d.Active() != "p2" {
				t.Errorf("following again, the host moved and the guest is in %s", d.Active())
			}
			titled("p2")
			s.shot("follow-again-" + name)
		})
	}
}

// The keys that go through tabs and panes are kakel's own.
func TestTheKeysOfTabsAndPanes(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	d := s.page.win.dock
	s.press(gi.KeyPageDown, gi.ModControl)
	if d.Active() != "p2" {
		t.Errorf("Ctrl+Page Down is in %s", d.Active())
	}
	s.press(gi.KeyTab, gi.ModControl)
	s.settle()
	if d.Active() != "p3" {
		t.Errorf("Ctrl+Tab is in %s", d.Active())
	}
	s.typing("x")
	s.sent("p3", "x")
}

// On a narrow page the panes stand one at a time, as tabs, and go back to
// the host's places with the room.
func TestANarrowPageShowsOnePane(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	d := s.page.win.dock
	s.click(s.page.win.panes["p3"])
	s.settle()
	s.win.Offscreen().Resize(geom.Sz(390, 700))
	var widths []float32
	for i := range 50 {
		s.frames(1)
		if i%5 == 0 {
			s.shot(fmt.Sprintf("narrow-%02d", i))
		}
		r, _ := d.Rect("p3")
		widths = append(widths, r.Size().W)
	}
	s.settle()
	if got := d.Shown(); !slices.Equal(got, []string{"p3"}) || widths[len(widths)-1] < 380 {
		t.Errorf("it shows %v, %v wide", got, widths)
	}
	if a := d.Arrangement(); len(a.Items()) != 4 {
		t.Errorf("its tabs are %v", a.Items())
	}
	s.typing("x")
	s.sent("p3", "x")
	if b, ok := s.u.Bounds(s.page.win.bar.leave); ok && b.Size().W > 1 {
		t.Errorf("Leave's word has the room %v", b)
	}
	s.win.Offscreen().Resize(geom.Sz(1000, 700))
	s.settle()
	if got := d.Shown(); !slices.Equal(got, []string{"p1", "p3", "files"}) || d.Active() != "p3" {
		t.Errorf("with the room it shows %v, and is in %s", got, d.Active())
	}
}

// While the connection is lost the panes stay under a veil, nothing is
// typed, and there is a way out; then it lifts.
func TestTheVeil(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	v := s.page.win.veil
	s.show(func(st *State) { st.Lost = true })
	on := []float32{v.on.Value()}
	for i := range 30 {
		s.frames(1)
		on = append(on, v.on.Value())
		if i%5 == 0 {
			s.shot(fmt.Sprintf("veil-%02d", i))
		}
	}
	if on[1] > 0.9 || on[3] <= on[1] || on[len(on)-1] < 0.9 {
		t.Errorf("the veil came as %v", on)
	}
	s.settle()
	s.shot("veil")
	s.typing("x")
	s.press(gi.KeyEnter, 0)
	s.sent("p1", "")
	if s.page.win.bar.swing.Active() {
		t.Error("it is hinted at that the guest may only watch")
	}
	s.show(func(st *State) { st.Why = "Anna is not connected." })
	s.settle()
	s.shot("veil-why")
	s.intents()
	// Out of it, from the keyboard.
	s.press(gi.KeyTab, 0)
	for range 8 {
		if b, ok := s.u.Focused().(*widget.Button); ok && b.Label == "Leave" && !b.Ghost {
			break
		}
		s.press(gi.KeyTab, 0)
	}
	s.press(gi.KeySpace, 0)
	s.win.Input(gi.KeyRelease{Key: gi.KeySpace, Time: time.Now()})
	s.settle()
	if s.page.dialog == nil {
		t.Fatalf("Leave did not ask; the keyboard is in %T", s.u.Focused())
	}
	s.press(gi.KeyEscape, 0)
	s.settle()
	s.show(func(st *State) { st.Lost, st.Why = false, "" })
	s.settle()
	s.click(s.page.win.panes["p1"])
	s.typing("y")
	s.sent("p1", "y")
}

// Leaving asks first, with Enter on the safe answer.
func TestLeavingAsks(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	s.intents()
	s.click(s.page.win.bar.leave)
	s.settle()
	s.shot("leave")
	s.press(gi.KeyEnter, 0)
	s.settle()
	if in := s.intents(); slices.Contains(in, gunim.Intent(Leave{})) || s.page.dialog != nil {
		t.Errorf("Enter asked for %v", in)
	}
	s.typing("x")
	s.sent("p1", "x")
	s.click(s.page.win.bar.leave)
	s.settle()
	s.click(s.page.dialog.OK())
	s.settle()
	if in := s.intents(); !slices.Contains(in, gunim.Intent(Leave{})) {
		t.Errorf("Leave asked for %v", in)
	}
}

// A paste of more than one line is asked about, and one line goes in.
func TestPasting(t *testing.T) {
	s := newStage(t, geom.Sz(900, 700), inWindow("p1"))
	s.settle()
	sent := func(want string) { t.Helper(); s.sent("p1", want) }
	s.u.SetClipboard("one line")
	s.press(gi.KeyV, gi.ModControl|gi.ModShift)
	sent("one line")
	s.u.SetClipboard("a\nb\n")
	s.press(gi.KeyInsert, gi.ModShift)
	s.settle()
	if s.page.dialog == nil {
		t.Fatal("two lines were not asked about")
	}
	s.shot("paste")
	sent("one line")
	s.press(gi.KeyEscape, 0)
	s.settle()
	sent("one line")
	s.press(gi.KeyV, gi.ModSuper)
	s.settle()
	s.press(gi.KeyEnter, 0)
	s.settle()
	sent("one linea\rb\r")
	// The keyboard is back in the pane.
	s.typing("x")
	sent("one linea\rb\rx")
	// What ends the window ends the question.
	s.press(gi.KeyV, gi.ModSuper)
	s.settle()
	s.show(func(st *State) { st.Page, st.Over = PageOver, overBroken })
	s.settle()
	if s.page.dialog != nil {
		t.Error("the question is still there")
	}
}

// A pane whose program has ended dims and says so, one that is another's
// kind says it is not shared, and one that becomes a terminal shows it.
func TestWhatAPaneIs(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	v := s.page.win.panes["p1"]
	s.show(func(st *State) { st.Panes = slices.Clone(st.Panes); st.Panes[0].Ended = true })
	s.frames(3)
	if d := v.dim.Value(); d <= 0 || d >= 0.9 {
		t.Errorf("three frames on it has dimmed %v", d)
	}
	s.settle()
	s.shot("ended")
	if v.ended.Hidden() || v.dim.Value() < 0.9 {
		t.Errorf("it says so: %v, and dimmed %v", !v.ended.Hidden(), v.dim.Value())
	}
	s.show(func(st *State) { st.Panes = slices.Clone(st.Panes); st.Panes[0].Ended = false })
	s.settle()
	if !v.ended.Hidden() || v.dim.Value() > 0.01 {
		t.Error("started again, it is still dim")
	}
	other := s.page.win.panes["files"]
	if other.term != nil {
		t.Fatal("a pane that is not shared has a terminal")
	}
	s.show(func(st *State) {
		st.Panes = slices.Clone(st.Panes)
		st.Panes[3] = termPane("files")
	})
	s.settle()
	s.settle()
	if other.term == nil {
		t.Error("one that became a terminal shows none")
	}
}

// A terminal that is not held stands as what it is, and takes nothing
// typed for another; one that is yet to come shows as it does, and one
// let go of in a tab behind is let go of there too.
func TestATerminalThatIsNotHeld(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit())
	s.settle()
	v := s.page.win.panes["p3"]
	was := v.term
	s.show(func(st *State) { st.Held = map[string]Held{"p3": HeldLarge} })
	if !s.u.Drawn(was) {
		t.Error("it is gone at once")
	}
	s.settle()
	s.shot("held")
	if v.term != nil || s.u.Drawn(was) {
		t.Error("it still shows")
	}
	s.press(gi.KeyTab, gi.ModControl)
	s.settle()
	if got := s.page.win.dock.Active(); got != "p3" {
		t.Fatalf("the guest is in %s", got)
	}
	s.typing("x")
	time.Sleep(20 * time.Millisecond)
	s.sent("p1", "")
	s.show(func(st *State) { st.Held = nil })
	s.settle()
	if v.term == nil {
		t.Fatal("once there is room it does not show")
	}
	s.typing("y")
	s.sent("p3", "y")

	// In front, and then behind.
	s.press(gi.KeyTab, gi.ModControl|gi.ModShift)
	s.settle()
	s.press(gi.KeyPageDown, gi.ModControl)
	s.settle()
	s.press(gi.KeyPageUp, gi.ModControl)
	s.settle()
	behind := s.page.win.panes["p2"]
	if behind == nil || behind.term == nil {
		t.Fatal("p2 was never shown")
	}
	sh := s.shells.Get("p2")
	s.shells.Set("p2", nil)
	s.again()
	s.settle()
	if behind.term != nil || behind.face.sh != nil {
		t.Error("a tab behind still has the terminal that was let go of")
	}
	s.shells.Set("p2", sh)
	s.again()
	s.settle()
	if behind.term == nil {
		t.Error("it does not take another")
	}
}

// A bell lights its pane, which fades, and a theme glides over all of it.
func TestABellAndATheme(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	v := s.page.win.panes["p1"]
	s.page.ring("p1", s.u)
	var most float32
	for range 90 {
		s.frames(1)
		most = max(most, v.rung.Value())
	}
	if most < 0.5 || v.rung.Value() > 0.01 {
		t.Errorf("the bell lit it %v, and leaves it %v", most, v.rung.Value())
	}
	s.show(func(st *State) { st.Theme, st.Own, st.Themes = themeAt(t, 1), themeAt(t, 1).Plain, 1 })
	was, to := themeAt(t, 0).Palette.BG, themeAt(t, 1).Palette.BG
	bg := widget.Background.Get(s.u.Theme())
	if bg.R == to.R {
		t.Errorf("the theme is there at once, from %v", was)
	}
	for i := range 20 {
		if i%4 == 0 {
			s.shot(fmt.Sprintf("theme-%02d", i))
		}
		s.frames(1)
	}
	s.settle()
	if bg = widget.Background.Get(s.u.Theme()); bg.R != to.R || bg.G != to.G {
		t.Errorf("the theme's ground is %v, want %v", bg, to)
	}
}

// A finger has a row of the keys its keyboard has not, where it may type.
func TestAFingersKeys(t *testing.T) {
	s := newStage(t, geom.Sz(390, 700), inWindow("p1"))
	s.settle()
	row := s.page.win.keys
	if row.fold.Open() {
		t.Fatal("the row is there with no finger")
	}
	box, _ := s.u.Bounds(s.page.win.panes["p1"])
	s.win.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1, Touch: true, Time: time.Now()})
	s.frames(2)
	s.win.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary, Touch: true, Time: time.Now()})
	s.settle()
	if !row.fold.Open() {
		t.Fatal("a finger has no row of keys")
	}
	s.shot("finger")
	tap := func(n gunim.Node) {
		t.Helper()
		box, ok := s.u.Bounds(n)
		if !ok {
			t.Fatalf("%T is not drawn", n)
		}
		s.win.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1, Touch: true, Time: time.Now()})
		s.frames(2)
		s.win.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary, Touch: true, Time: time.Now()})
		s.frames(2)
	}
	tap(row.keys[0])
	tap(row.ctrl)
	s.typing("c")
	s.typing("c")
	s.sent("p1", "\x1b\x03c")
	s.show(func(st *State) { st.CanType = false })
	s.settle()
	if row.fold.Open() {
		t.Error("the row is there with nothing to type")
	}
}

// The keyboard goes with a finger to the pane it touches, as the dock's
// mark does. Held, the finger selects, and the menu comes to copy that as
// it lifts.
func TestAFingerTapsAndHolds(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inSplit(), func(c *Config) { c.Touch = true })
	s.win.TimeByFrames(true)
	s.settle()
	w := s.page.win
	finger := func(id string, dx, dy float32, do func(at geom.Point)) {
		t.Helper()
		v := w.panes[id]
		box, ok := s.u.Bounds(v.term)
		if !ok {
			t.Fatalf("%s is not drawn", id)
		}
		at, cell := v.term.Drawn()
		p := box.Min.Add(at).Add(geom.Pt(1.5*cell.W, 0.5*cell.H))
		s.win.Input(gi.PointerDown{Pos: p, Button: gi.ButtonPrimary, Clicks: 1, Touch: true, Time: time.Now()})
		s.frames(1)
		do(p)
		s.win.Input(gi.PointerUp{Pos: p.Add(geom.Pt(dx, dy)), Button: gi.ButtonPrimary, Touch: true, Time: time.Now()})
		s.settle()
	}
	still := func(geom.Point) {}
	finger("p3", 0, 0, still)
	if w.dock.Active() != "p3" || !w.panes["p3"].term.Focused() {
		t.Fatalf("after a tap it is in %s", w.dock.Active())
	}
	finger("p1", 0, 60, func(p geom.Point) {
		for i := range 8 {
			s.win.Input(gi.PointerMove{Pos: p.Add(geom.Pt(0, float32(i)*8)), Touch: true, Time: time.Now()})
			s.frames(1)
		}
	})
	if w.dock.Active() != "p1" || !w.panes["p1"].term.Focused() {
		t.Fatalf("after a scroll in p1 it is in %s", w.dock.Active())
	}
	finger("p1", 0, 0, func(geom.Point) { s.frames(40) })
	if got := w.panes["p1"].term.Shell().T.SelectionText(); got != "this" {
		t.Errorf("a finger held selects %q", got)
	}
	s.shot("finger-held")
	s.press(gi.KeyDown, 0)
	s.press(gi.KeyEnter, 0)
	s.settle()
	if got, _ := s.u.ReadClipboard(); got != "this" {
		t.Errorf("the menu's first item copied %q", got)
	}
}

// The end of a line goes to no other pane as the one it was typed in goes
// away: a split or a tab in front of it, or the last there was.
func TestALineEndsWithItsPane(t *testing.T) {
	without := func(gone string, all State) func(st *State) {
		return func(st *State) {
			st.Panes = slices.DeleteFunc(slices.Clone(all.Panes), func(p guest.Pane) bool { return p.ID == gone })
			st.Layout = all.Layout.Clone()
			st.Layout.Remove(gone)
			st.HostIn = "p2"
		}
	}
	for name, c := range map[string]struct {
		st   State
		pane string
	}{"a tab": {inWindow("p1", "p2"), "p1"}, "a split": {inSplit(), "p3"}} {
		t.Run(name, func(t *testing.T) {
			s := newStage(t, geom.Sz(1000, 600), c.st)
			s.settle()
			s.click(s.page.win.panes[c.pane])
			s.typing("rm -rf ./bu")
			s.sent(c.pane, "rm -rf ./bu")
			s.show(without(c.pane, c.st))
			s.typing("ild; sudo ")
			// However slowly it goes on, as long as it does.
			for range 5 {
				s.frames(30)
				s.typing("x")
			}
			s.typing("reboot")
			s.press(gi.KeyEnter, 0)
			s.frames(30)
			for _, id := range []string{"p1", "p2"} {
				if id != c.pane {
					s.sent(id, "")
				}
			}
			s.frames(60)
			at := s.page.win.dock.Active()
			s.typing("ls")
			s.sent(at, "ls")
		})
	}
	// With none left, the next one to come.
	s := newStage(t, geom.Sz(1000, 600), inWindow("p1"))
	s.settle()
	s.click(s.page.win.panes["p1"])
	s.typing("rm -rf ./bu")
	s.show(func(st *State) { st.Panes, st.Layout, st.HostIn = nil, dock.NewLayout(true), "" })
	s.show(func(st *State) {
		next := inWindow("p2")
		st.Panes, st.Layout, st.HostIn = next.Panes, next.Layout, "p2"
	})
	s.frames(2)
	s.typing("ild; sudo reboot")
	s.press(gi.KeyEnter, 0)
	s.sent("p2", "")
}

// A pane that goes away beside the one typed in takes nothing with it.
func TestAnotherPaneThatGoesStopsNoTyping(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 600), inSplit())
	s.settle()
	s.click(s.page.win.panes["p3"])
	s.typing("a")
	s.show(func(st *State) {
		st.Panes = slices.DeleteFunc(slices.Clone(st.Panes), func(p guest.Pane) bool { return p.ID == "p1" })
		st.Layout = st.Layout.Clone()
		st.Layout.Remove("p1")
		st.HostIn = "p2"
	})
	s.typing("b")
	s.sent("p3", "ab")
}

// A key pressed with the keyboard nowhere, as after a click on the bar,
// goes to the pane the guest is in.
func TestAKeyWithTheKeyboardNowhere(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	s.u.Focus(nil)
	s.frames(2)
	if s.u.Focused() != nil {
		t.Skip("the keyboard goes back by itself")
	}
	s.typing("x")
	s.typing("y")
	s.sent("p1", "xy")
}

// A copy the browser kept off the clipboard is said, as is a paste it
// would not give.
func TestWhatTheClipboardDoesNot(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	s.page.failed("Not copied", errors.New("web: NotAllowedError"), s.u)
	s.settle()
	if s.page.toasts.Len() != 1 {
		t.Error("nothing is said")
	}
	s.shot("not-copied")
}

// With nothing to animate, a page is there at once.
func TestAStillPage(t *testing.T) {
	th := Still(themeAt(t, 0))
	s := newStage(t, geom.Sz(800, 600), State{Page: PageJoin})
	s.u.UseTheme(th.Theme)
	defer anim.SetScale(1)
	s.frames(60)
	s.show(func(st *State) { st.Page = PageWaiting })
	s.frames(3)
	if box, ok := s.u.Bounds(s.page.card.title); !ok || !s.u.Drawn(s.page.card.title) {
		t.Errorf("three frames on, what it says is at %v, drawn: %v", box, ok)
	}
}

// Full screen is asked for with the button, and the button follows it.
func TestFillingTheScreen(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	b := s.page.win.bar
	s.click(b.fill)
	s.settle()
	if !s.win.Offscreen().FullScreen() || b.fill.Open() || !b.unfill.Open() {
		t.Errorf("it fills the screen: %v, with its button: %v", s.win.Offscreen().FullScreen(), b.unfill.Open())
	}
	s.shot("filled")
	s.click(b.unfill)
	s.settle()
	if s.win.Offscreen().FullScreen() || !b.fill.Open() || b.unfill.Open() {
		t.Error("it still fills the screen")
	}
	// The user left by themselves.
	s.click(b.fill)
	s.settle()
	s.win.Offscreen().SetFullScreen(false)
	s.show(func(*State) {})
	s.settle()
	if !b.fill.Open() || b.unfill.Open() {
		t.Error("the button stays as it was")
	}
}

// What is typed in a pane that is not open yet waits for it, all of it and
// in order: in one that is yet to have a terminal, with the keyboard going
// on to it, and in one whose terminal is there.
func TestWhatIsTypedBeforeAPaneIsOpenWaitsForIt(t *testing.T) {
	s := newStage(t, geom.Sz(1000, 700), inWindow("p1", "p2", "p3"))
	s.settle()
	sh := s.shells.Get("p2")
	s.shells.Set("p2", nil)
	shut := func(ids ...string) {
		s.st.Shut = map[string]bool{}
		for _, id := range ids {
			s.st.Shut[id] = true
		}
		s.again()
	}
	shut("p3")
	s.settle()
	s.press(gi.KeyPageDown, gi.ModControl)
	s.typing("rm -rf ")
	s.typing("build")
	if !s.u.FocusTakesText() || !s.win.Offscreen().KeysClaimed() {
		t.Error("a pane that is yet to show has a phone's keyboard go away, or the browser's keys")
	}
	s.frames(20)
	s.u.SetClipboard("/x")
	s.press(gi.KeyV, gi.ModSuper)
	s.shells.Set("p2", sh)
	shut("p2", "p3")
	s.typing("/y")
	s.settle()
	s.sent("p2", "")
	shut("p3")
	s.press(gi.KeyEnter, 0)
	s.sent("p2", "rm -rf build/x/y\r")
	if s.page.toasts.Len() != 0 {
		t.Error("something is said of it")
	}
	// One kept, whose pane was closed while it was off the screen.
	s.press(gi.KeyPageDown, gi.ModControl)
	s.typing("ls")
	s.press(gi.KeyEnter, 0)
	s.settle()
	s.sent("p3", "")
	shut()
	s.sent("p3", "ls\r")
	s.sent("p1", "")
}

// What waits is lost whole, and said, where the connection goes, the guest
// may type no more, the pane has no room or goes, it is too much, or the
// pane is too long in coming; and what is typed on is for nobody until a
// pause.
func TestWhatIsTypedIsSentWholeOrNotAtAll(t *testing.T) {
	long := strings.Repeat("x", mostEarlyKeys)
	for name, lose := range map[string]func(s *stage){
		"the connection goes": func(s *stage) { s.show(func(st *State) { st.Lost = true }) },
		"it goes and is back": func(s *stage) { s.show(func(st *State) { st.Conn++ }) },
		"no more typing":      func(s *stage) { s.show(func(st *State) { st.CanType = false }) },
		"something was lost":  func(s *stage) { s.show(func(st *State) { st.Untyped = map[string]int{"p1": 1} }) },
		"lost and then ready": func(s *stage) {
			s.show(func(st *State) { st.Untyped, st.Shut = map[string]int{"p1": 1}, nil })
		},
		"too much":  func(s *stage) { s.typing(long) },
		"no room":   func(s *stage) { s.show(func(st *State) { st.Held = map[string]Held{"p1": HeldLarge} }) },
		"too long":  func(s *stage) { s.win.Frame(earlyFor + time.Second) },
		"it closes": func(s *stage) { s.show(func(st *State) { st.Panes = st.Panes[1:]; st.Layout = inWindow("p2").Layout }) },
	} {
		t.Run(name, func(t *testing.T) {
			st := inWindow("p1", "p2")
			st.Shut = map[string]bool{"p1": true}
			s := newStage(t, geom.Sz(800, 600), st)
			s.settle()
			s.typing("rm -rf ")
			lose(s)
			if s.page.toasts.Len() == 0 {
				t.Error("nothing is said")
			}
			s.typing("build")
			s.show(func(st *State) { st.Lost, st.CanType, st.Shut, st.Held = false, true, nil, nil })
			s.typing("/x")
			s.press(gi.KeyEnter, 0)
			if name == "it closes" {
				return
			}
			s.frames(60)
			s.typing("y")
			s.sent("p1", "y")
		})
	}
}

// A paste asked about is typed as it is answered, or it is said why not.
func TestAPasteAnsweredTooLate(t *testing.T) {
	for name, change := range map[string]func(st *State){
		"lost":     func(st *State) { st.Lost = true },
		"watching": func(st *State) { st.CanType = false },
		"gone":     func(st *State) { st.Panes = st.Panes[1:]; st.Layout = inWindow("p2").Layout },
	} {
		t.Run(name, func(t *testing.T) {
			s := newStage(t, geom.Sz(900, 700), inWindow("p1", "p2"))
			s.settle()
			s.u.SetClipboard("a\nb\n")
			s.press(gi.KeyV, gi.ModSuper)
			s.settle()
			s.show(change)
			s.settle()
			s.press(gi.KeyEnter, 0)
			s.settle()
			s.sent("p1", "")
			if s.page.toasts.Len() == 0 {
				t.Error("nothing is said")
			}
		})
	}
}

// Nothing a host says of its theme hides what the page says itself.
func TestAHostsThemeLeavesThePageItself(t *testing.T) {
	edits := `{"motion.scale": 1e9, "kakel.page.bar.height": 0, "text.size": 0, "toast.room": 3e38, "kakel.page.toast.room": 3e38,
		"menubar.fill": "#8a93a6", "kakel.faint": "#8a93a6", "ink": "#8a93a6", "dialog.fill": "#8a93a6", "button.height": 1e9, "dialog.width": -1e9}`
	raw, err := json.Marshal(map[string]any{"theme": themeAt(t, 0).Source, "edits": json.RawMessage(edits), "fontSize": 15})
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"edited": string(raw),
		"unread": strings.ReplaceAll(strings.ReplaceAll(string(raw), themeAt(t, 0).Source.FG, "#101010"), themeAt(t, 0).Source.BG, "#101010"),
	} {
		t.Run(name, func(t *testing.T) {
			quick(t)
			v := in("p1")
			v.State.Theme = []byte(raw)
			c := newCourse(t, true, nil, newFake(v))
			c.until("themed", func() bool { return c.state().Themes == 1 })
			th := c.state()
			s := newStage(t, geom.Sz(900, 600), inWindow("p1"))
			s.settle()
			s.show(func(st *State) { st.Theme, st.Own, st.Themes = th.Theme, th.Own, 1 })
			s.settle()
			b := s.page.win.bar
			live := s.u.Theme()
			if got := barHeight.Get(live); got != barHeight.Default() {
				t.Errorf("the bar is %v tall", got)
			}
			for what, pair := range map[string][2]color.NRGBA{
				"the bar's title":  {widget.Ink.Get(live), widget.MenubarFill.Get(live)},
				"who shares it":    {quietInk.Get(live), widget.MenubarFill.Get(live)},
				"a card's words":   {quietInk.Get(live), widget.Background.Get(live)},
				"a dialog's words": {widget.Ink.Get(live), widget.DialogFill.Get(live)},
			} {
				rgba := func(c color.NRGBA) color.RGBA { return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff} }
				if got := grid.Contrast(rgba(pair[0]), rgba(pair[1])); got < 4.4 {
					t.Errorf("%s contrast %v", what, got)
				}
			}
			s.shot("hostile-" + name)
			// What is said, and asked, comes and goes as ever.
			s.page.failed(notTyped, errors.New("x"), s.u)
			s.click(b.leave)
			s.settle()
			s.shot("hostile-dialog-" + name)
			if box, ok := s.u.Bounds(s.page.dialog.OK()); !ok || box.Size().H < 20 || box.Size().H > 60 || box.Max.Y > 600 {
				t.Errorf("the dialog's button stands at %v", box)
			}
			s.press(gi.KeyEscape, 0)
			s.show(func(st *State) { st.Lost = true })
			s.settle()
			s.shot("hostile-veil-" + name)
			if s.page.win.veil.on.Value() < 0.9 {
				t.Error("the veil does not come")
			}
			s.show(func(st *State) { st.Page, st.Over = PageOver, overBroken })
			s.settle()
		})
	}
}

// Who shares the window has room, whatever the window is called, and the
// way out too.
func TestALongTitleLeavesRoomForWhoShares(t *testing.T) {
	for _, w := range []float32{320, 390, 700, 1200} {
		st := inWindow("p1")
		st.Panes[0].Title, st.HostName, st.CanType = strings.Repeat("Shared by you ", 40), "Anna", false
		s := newStage(t, geom.Sz(w, 600), st)
		s.settle()
		b := s.page.win.bar
		box, ok := s.u.Bounds(b.by)
		if !ok || box.Size().W < 70 || box.Max.X > w {
			t.Errorf("%v wide, who shares it stands at %v", w, box)
		}
		title, _ := s.u.Bounds(b.title)
		if title.Max.Y > box.Min.Y+1 {
			t.Errorf("the title at %v lies over %v", title, box)
		}
		out := b.leave
		if w < 600 {
			out = b.leaveIcon
		}
		if box, _ := s.u.Bounds(out); box.Size().W < 20 || box.Max.X > w {
			t.Errorf("%v wide, the way out stands at %v", w, box)
		}
		s.shot(fmt.Sprint("long-title-", w))
	}
}

// Full screen, and the keys it takes, end with the window.
func TestFullScreenEndsWithTheWindow(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	s.click(s.page.win.bar.fill)
	s.settle()
	if !s.win.Offscreen().FullScreen() {
		t.Fatal("it does not fill the screen")
	}
	s.show(func(st *State) { st.Page, st.Over = PageOver, overBroken })
	s.settle()
	if s.win.Offscreen().FullScreen() {
		t.Error("at the end it still fills the screen")
	}
}

// On a narrow page, and on a phone on its side, the one pane shown follows
// the host.
func TestANarrowPageFollowsTheHost(t *testing.T) {
	for name, size := range map[string]geom.Size{"narrow": geom.Sz(390, 700), "on its side": geom.Sz(844, 390)} {
		t.Run(name, func(t *testing.T) {
			s := newStage(t, size, inSplit(), func(c *Config) { c.Touch = true })
			s.settle()
			d := s.page.win.dock
			for _, id := range []string{"p3", "p2", "p1"} {
				s.show(func(st *State) {
					st.Layout = st.Layout.Clone()
					st.Layout.Show(id)
					st.HostIn = id
				})
				s.settle()
				if got := d.Shown(); !slices.Equal(got, []string{id}) || d.Active() != id {
					t.Errorf("with the host in %s it shows %v, and is in %s", id, got, d.Active())
				}
			}
			s.shot("follow-" + strings.ReplaceAll(name, " ", "-"))
			s.typing("x")
			s.sent("p1", "x")
			s.show(func(st *State) { st.HostIn = "p3" })
			s.settle()
			if d.Active() != "p1" {
				t.Errorf("having typed, the guest is in %s", d.Active())
			}
		})
	}
}

// However often a bell rings, the pane is lit once at a time.
func TestABellThatRingsAndRings(t *testing.T) {
	s := newStage(t, geom.Sz(800, 600), inWindow("p1"))
	s.settle()
	for range 1000 {
		s.page.ring("p1", s.u)
	}
	s.frames(2)
	if v := s.page.win.panes["p1"]; !v.ringing {
		t.Error("it does not ring")
	}
	s.settle()
	if v := s.page.win.panes["p1"]; v.ringing || v.rung.Value() > 0.01 {
		t.Error("it rings on")
	}
}

// While the connection is lost, the bar says nothing of what the guest may
// do, and the way out is the veil's alone, which makes way for a dialog.
func TestWhileTheConnectionIsLost(t *testing.T) {
	s := newStage(t, geom.Sz(900, 600), inWindow("p1"))
	s.settle()
	w := s.page.win
	s.show(func(st *State) { st.Lost = true })
	if !w.bar.leave.Open() && !s.u.Drawn(w.bar.leave) {
		t.Error("the way out is gone at once")
	}
	s.settle()
	if !w.bar.types.Hidden() || !w.bar.watch.Hidden() || w.bar.leave.Open() || w.bar.leaveIcon.Open() {
		t.Error("the bar says what the guest may do, or has a way out")
	}
	s.page.askLeave(s.u)
	s.settle()
	s.shot("veil-asking")
	if w.veil.way.Value() < 0.9 {
		t.Error("the veil stands under the dialog")
	}
	s.press(gi.KeyEscape, 0)
	s.settle()
	if w.veil.way.Value() > 0.1 {
		t.Error("the veil does not come back")
	}
	s.show(func(st *State) { st.Lost = false })
	s.settle()
	if w.bar.types.Hidden() || !w.bar.leave.Open() {
		t.Error("the bar does not come back")
	}
}

// The page after a dialog comes once the dialog has gone.
func TestThePageAfterADialogWaitsForItToGo(t *testing.T) {
	s := newStage(t, geom.Sz(900, 600), inWindow("p1"))
	s.settle()
	s.page.askLeave(s.u)
	s.settle()
	d := s.page.dialog
	win := s.page.pages.Page()
	s.show(func(st *State) { st.Page, st.Over = PageOver, overBroken })
	seen := 0
	for i := range 60 {
		if s.u.Drawn(d) {
			seen++
			if s.page.pages.Page() != win {
				t.Fatal("the next page is there with the dialog")
			}
		}
		if i%3 == 0 && i < 30 {
			s.shot(fmt.Sprintf("after-dialog-%02d", i))
		}
		s.frames(1)
	}
	if seen < 2 || s.page.pages.Page() == win || s.page.card == nil {
		t.Errorf("the dialog went in %d frames, and the next page came: %v", seen, s.page.card != nil)
	}
	// Another state meanwhile is the one that comes.
	s.show(func(st *State) { st.Page = PageWindow })
	s.settle()
	s.page.askLeave(s.u)
	s.settle()
	s.show(func(st *State) { st.Page, st.Over = PageOver, overBroken })
	s.show(func(st *State) { st.Page = PageWindow })
	s.settle()
	if s.page.pages.Page() != win {
		t.Error("the page that was not to come came")
	}
	s.typing("x")
	s.sent("p1", "x")
}

// A key held for the next letter is typed as an input method begins to
// compose one, which is no beginning of a word.
func TestAHeldKeyWaitsForNoWord(t *testing.T) {
	s := newStage(t, geom.Sz(390, 700), inWindow("p1"), func(c *Config) { c.Touch = true })
	s.settle()
	row := s.page.win.keys
	s.click(row.ctrl) // A mouse's click folds the row away, and holds the key all the same.
	v := s.page.win.panes["p1"]
	if !v.term.ctrl {
		t.Fatal("Ctrl is not held")
	}
	s.win.Input(gi.Composing{Text: "c", Selected: [2]int{1, 1}})
	s.frames(2)
	s.sent("p1", "\x03")
	s.win.Input(gi.Composing{Text: "cd", Selected: [2]int{2, 2}})
	s.frames(2)
	if got := v.term.Composing(); got != "d" {
		t.Errorf("it composes %q", got)
	}
	s.win.Input(gi.Composing{})
	s.typing("cd")
	s.sent("p1", "\x03d")
	s.typing("cd")
	s.sent("p1", "\x03dcd")
	v.term.alt = true
	s.typing("x")
	v.term.alt = true
	s.typing("1")
	s.sent("p1", "\x03dcd\x1bx\x1b1")
}

// What a finger touches is large enough for it, and all the keys have
// room on a phone.
func TestAFingerHasRoom(t *testing.T) {
	s := newStage(t, geom.Sz(360, 640), inWindow("p1", "p2"), func(c *Config) { c.Touch = true })
	s.settle()
	s.shot("touch-keys")
	w := s.page.win
	all := []gunim.Node{w.keys.ctrl, w.keys.rest, w.bar.leaveIcon, w.bar.fill}
	for _, k := range w.keys.keys[:6] {
		all = append(all, k)
	}
	check := func(all []gunim.Node) {
		t.Helper()
		for _, n := range all {
			box, ok := s.u.Bounds(n)
			if !ok || box.Size().H < touchLeast || box.Size().W < 42 || box.Min.X < 0 || box.Max.X > 360 {
				t.Errorf("%T stands at %v", n, box)
			}
		}
	}
	check(all)
	if r, ok := w.dock.TabRect("p1"); !ok || r.Size().H < touchLeast-8 {
		t.Errorf("a tab is %v", r)
	}
	tap := func(n gunim.Node) {
		box, _ := s.u.Bounds(n)
		s.win.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1, Touch: true, Time: time.Now()})
		s.frames(2)
		s.win.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary, Touch: true, Time: time.Now()})
		s.settle()
	}
	if w.keys.more.Open() {
		t.Error("the rest of the keys are there at once")
	}
	tap(w.keys.rest)
	s.shot("touch-keys-more")
	check([]gunim.Node{w.keys.alt, w.keys.keys[6], w.keys.keys[9]})
	for _, k := range w.keys.keys[6:] {
		tap(k)
	}
	s.sent("p1", "\x1b[H\x1b[F\x1b[5~\x1b[6~")
	s.show(func(st *State) { st.Page, st.Over = PageOver, overOf(guest.ReasonReplaced, "") })
	s.settle()
	s.shot("touch-over")
	check([]gunim.Node{partOf[*widget.Button](s, s.page.card.body)})
	s.show(func(st *State) { st.Page = PageJoin })
	s.settle()
	s.shot("touch-join")
}

// The keys a menu says are the ones there are: a Mac's on a Mac, and none
// for a finger.
func TestAMenusKeys(t *testing.T) {
	p := &page{}
	other, _ := p.hints()
	p.cfg.Mac = true
	mac, _ := p.hints()
	p.touch = true
	if none, _ := p.hints(); none != "" || mac == other || mac == "" {
		t.Errorf("they are %q, %q and %q", other, mac, none)
	}
}

// The keyboard is on what there is to do, with its ring where it came by
// the keys. Where a page waits it is on nothing: what there is to do there
// is to cancel.
func TestTheKeyboardOnAPagesButton(t *testing.T) {
	s := newStage(t, geom.Sz(600, 500), State{Page: PageJoin, Name: "Bo"})
	s.settle()
	s.press(gi.KeyTab, 0)
	s.show(func(st *State) { st.Page = PageWaiting })
	s.settle()
	s.intents()
	s.press(gi.KeyEnter, 0)
	if in := s.intents(); len(in) != 0 {
		t.Errorf("an Enter too many, as it waits, asked for %v", in)
	}
	s.press(gi.KeyTab, 0)
	s.settle()
	s.shot("waiting-ring")
	stop := partOf[*widget.Button](s, s.page.card.body)
	if s.u.Focused() != gunim.Node(stop) {
		t.Errorf("after Tab, as it waits, the keyboard is on %T", s.u.Focused())
	}
	s.show(func(st *State) { st.Page, st.Over = PageOver, overUnreached })
	s.settle()
	s.shot("over-ring")
	if retry := partOf[*widget.Button](s, s.page.card.body); s.u.Focused() != gunim.Node(retry) {
		t.Errorf("at the end the keyboard is on %T", s.u.Focused())
	}
}
