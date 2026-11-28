package guestview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/termview"
)

// fakePane is a pane with a screen to begin with and nothing after.
type fakePane struct {
	id string
	mu sync.Mutex
	// screens are to come, and typed is what was typed.
	screens chan *session.Screen
	typed   []byte
	over    chan struct{}
	closed  bool
}

func newFakePane(id, draw string) *fakePane { return newPaneOf(id, 40, 10, draw) }

func newPaneOf(id string, cols, rows int, draw string) *fakePane {
	p := &fakePane{id: id, screens: make(chan *session.Screen, 4), over: make(chan struct{})}
	p.screens <- &session.Screen{Cols: cols, Rows: rows, Draw: []byte(draw)}
	return p
}

func (p *fakePane) ReadScreen([]byte) (int, *session.Screen, error) {
	select {
	case s := <-p.screens:
		return 0, s, nil
	case <-p.over:
		return 0, nil, io.EOF
	}
}
func (p *fakePane) Read([]byte) (int, error) { <-p.over; return 0, io.EOF }
func (p *fakePane) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.typed = append(p.typed, b...)
	return len(b), nil
}
func (p *fakePane) Resize(int, int) error { return nil }
func (p *fakePane) Wait() error           { <-p.over; return nil }
func (p *fakePane) Mirrors() bool         { return true }
func (p *fakePane) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.over)
	}
	return nil
}
func (p *fakePane) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

// fakeSession is a host that says what a test has it say.
type fakeSession struct {
	mu      sync.Mutex
	view    guest.View
	changed chan struct{}
	done    chan struct{}
	err     error
	// joinErr is what Join answers, and named what it was called with.
	joinErr error
	named   string
	onJoin  func()
	panes   []*fakePane
	paneErr error
	onPane  func()
	// sizes are the panes of another size than 40 by 10.
	sizes map[string][2]int
	// focused are where the guest said it is, following for the host's.
	focused []string
	left    bool
}

// following is the guest saying it follows the host, as focused has it.
const following = "(following)"

func newFake(v guest.View) *fakeSession {
	return &fakeSession{view: v, changed: make(chan struct{}, 1), done: make(chan struct{})}
}

func (s *fakeSession) View() guest.View         { s.mu.Lock(); defer s.mu.Unlock(); return s.view }
func (s *fakeSession) Changed() <-chan struct{} { return s.changed }
func (s *fakeSession) Done() <-chan struct{}    { return s.done }
func (s *fakeSession) Err() error               { s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *fakeSession) Join(_ context.Context, name, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.named = name
	if s.onJoin != nil {
		s.onJoin()
	}
	if s.joinErr == nil {
		s.view.Phase = guest.PhaseWaiting
	}
	return s.joinErr
}
func (s *fakeSession) Pane(_ context.Context, id string) (session.ScreenReader, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onPane != nil {
		s.onPane()
	}
	if s.paneErr != nil {
		return nil, s.paneErr
	}
	size, ok := s.sizes[id]
	if !ok {
		size = [2]int{40, 10}
	}
	p := newPaneOf(id, size[0], size[1], "screen of "+id)
	s.panes = append(s.panes, p)
	return p, nil
}
func (s *fakeSession) Follow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focused = append(s.focused, following)
}
func (s *fakeSession) Focus(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.focused = append(s.focused, id)
}
func (s *fakeSession) Leave() {
	s.mu.Lock()
	s.left = true
	s.mu.Unlock()
	s.end(&guest.EndedError{Reason: guest.ReasonLeft})
}
func (s *fakeSession) Close() error { s.end(guest.ErrLost); return nil }

// end ends the connection, once.
func (s *fakeSession) end(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
		close(s.done)
		for _, p := range s.panes {
			_ = p.Close()
		}
	}
}

// say has the host say v.
func (s *fakeSession) say(v guest.View) {
	s.mu.Lock()
	s.view = v
	s.mu.Unlock()
	select {
	case s.changed <- struct{}{}:
	default:
	}
}

// opened counts the panes opened and not closed, by ID.
func (s *fakeSession) opened() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, p := range s.panes {
		if !p.isClosed() {
			out = append(out, p.id)
		}
	}
	slices.Sort(out)
	return out
}

// fakeKeep is a tab's storage.
type fakeKeep struct {
	mu     sync.Mutex
	key    guest.Key
	has    bool
	joined bool
}

func (k *fakeKeep) Load() (guest.Key, bool) { k.mu.Lock(); defer k.mu.Unlock(); return k.key, k.has }
func (k *fakeKeep) Save(key guest.Key)      { k.mu.Lock(); defer k.mu.Unlock(); k.key, k.has = key, true }
func (k *fakeKeep) Joined()                 { k.mu.Lock(); defer k.mu.Unlock(); k.joined = true }
func (k *fakeKeep) Clear()                  { k.mu.Lock(); defer k.mu.Unlock(); k.has = false }
func (k *fakeKeep) kept() bool              { _, ok := k.Load(); return ok }

// course is a flow with a host that is dialled as a test says.
type course struct {
	t    *testing.T
	f    *flow
	keep *fakeKeep
	wake chan struct{}

	mu sync.Mutex
	// answers are what the next dials come to, the last over and over,
	// and keys what they were dialled with.
	answers []any
	keys    []guest.Key
	last    State
	names   []string
}

// quick has everything take next to no time, for a test.
func quick(t *testing.T) {
	was := paces
	t.Cleanup(func() { paces = was })
	ms := time.Millisecond
	paces = pacing{tries: []time.Duration{ms, 2 * ms, 4 * ms}, joins: 3, blocked: 50 * ms, wakes: 5 * ms, stuck: 20 * ms, giveUp: time.Hour,
		held: time.Hour, dials: 1000, each: ms, hidden: 60 * ms, kept: 100 * ms, again: 5 * ms, againMost: 20 * ms, bells: 5 * ms, room: mostHeld}
}

// newCourse runs a flow whose dials come to answers: an error, or a
// session.
func newCourse(t *testing.T, kept bool, change func(*Config), answers ...any) *course {
	t.Helper()
	c := &course{t: t, keep: &fakeKeep{}, wake: make(chan struct{}, 1), answers: answers}
	if kept {
		key, err := guest.NewKey()
		if err != nil {
			t.Fatal(err)
		}
		c.keep.Save(key)
	}
	cfg := Config{
		Dial: func(_ context.Context, key guest.Key) (Session, error) {
			c.mu.Lock()
			defer c.mu.Unlock()
			c.keys = append(c.keys, key)
			a := c.answers[0]
			if len(c.answers) > 1 {
				c.answers = c.answers[1:]
			}
			if err, ok := a.(error); ok {
				return nil, err
			}
			return a.(Session), nil
		},
		Keep: c.keep, Wake: c.wake, Theme: themeAt(t, 0), About: "Chrome on Linux",
		SaveName: func(n string) { c.mu.Lock(); defer c.mu.Unlock(); c.names = append(c.names, n) },
		OnState:  func(st State) { c.mu.Lock(); defer c.mu.Unlock(); c.last = st },
	}
	if change != nil {
		change(&cfg)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.f = newFlow(ctx, gunimtest.New(t, geom.Sz(100, 100), nil), cfg)
	c.f.begin()
	done := make(chan struct{})
	go func() { defer close(done); _ = c.f.run(); c.f.close() }()
	t.Cleanup(func() { cancel(); <-done })
	return c
}

func (c *course) state() State { c.mu.Lock(); defer c.mu.Unlock(); return c.last }
func (c *course) dials() int   { c.mu.Lock(); defer c.mu.Unlock(); return len(c.keys) }

// then says what the next dials come to.
func (c *course) then(answers ...any) { c.mu.Lock(); defer c.mu.Unlock(); c.answers = answers }

// do has the guest do something.
func (c *course) do(in any) { c.f.post(func() { c.f.intent(in) }) }

// until waits until ok.
func (c *course) until(what string, ok func() bool) {
	c.t.Helper()
	for end := time.Now().Add(10 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			st := c.state()
			st.Theme, st.Own = look.Themed{}, theme.Theme{}
			c.t.Fatalf("not %s; the state: %+v", what, st)
		}
	}
}

// on waits for a page.
func (c *course) on(p Page) {
	c.t.Helper()
	c.until(fmt.Sprint("on page ", p), func() bool { return c.state().Page == p })
}

// in is a window of terminals, as a host says it.
func in(ids ...string) guest.View {
	st := guest.State{HostName: "Anna", CanType: true}
	l := dock.NewLayout(true, ids...)
	st.Layout, _ = guestlook.Layout(l.Windows[0].Root, func(string) bool { return true })
	for _, id := range ids {
		st.Panes = append(st.Panes, guest.Pane{ID: id, Title: id, Kind: guest.KindTerminal, Cols: 40, Rows: 10})
	}
	if len(ids) > 0 {
		st.HostIn = ids[0]
	}
	return guest.View{Phase: guest.PhaseIn, State: st}
}

// joinsFirst is a host nobody has joined yet.
func joinsFirst() *fakeSession { return newFake(guest.View{Phase: guest.PhaseJoin}) }

// Nothing is dialled before Join, the key is kept before the host has it,
// and the address lets go of its secret once the host has been asked.
func TestAGuestJoinsOnceItAsks(t *testing.T) {
	quick(t)
	s := joinsFirst()
	c := newCourse(t, false, nil, s)
	s.onJoin = func() {
		if !c.keep.kept() {
			t.Error("the host had the key before the tab did")
		}
	}
	time.Sleep(20 * time.Millisecond)
	if n := c.dials(); n != 0 {
		t.Fatalf("before Join the host was dialled %d times", n)
	}
	c.do(Join{"  "})
	c.do(Join{" Bo "})
	c.do(Join{"Bo again"})
	c.on(PageWaiting)
	if c.dials() != 1 || s.named != "Bo" || !c.keep.joined {
		t.Errorf("it dialled %d times, joined as %q, and dropped the secret: %v", c.dials(), s.named, c.keep.joined)
	}
	if !slices.Equal(c.names, []string{"Bo"}) {
		t.Errorf("the names saved are %q", c.names)
	}
	s.say(in("p1"))
	c.on(PageWindow)
	if st := c.state(); st.HostName != "Anna" || !st.CanType || len(st.Panes) != 1 {
		t.Errorf("the window is %+v", st)
	}
}

// A Join that does not get there is tried again by itself, and then the
// guest is told, and may try once more.
func TestAJoinThatDoesNotGetThereIsTriedAgain(t *testing.T) {
	quick(t)
	c := newCourse(t, false, nil, errors.New("no network"))
	c.do(Join{"Bo"})
	c.until("told", func() bool { return c.state().Trouble != "" })
	if st := c.state(); st.Page != PageJoin || st.Busy || c.dials() != paces.joins {
		t.Errorf("after %d tries it shows %+v", c.dials(), st)
	}
	if c.keep.kept() {
		t.Error("the tab kept a key the host never had")
	}
	// Once, where the relay says there were too many.
	c.then(relay.RefusedTooMany)
	n := c.dials()
	c.do(Join{"Bo"})
	c.until("told again", func() bool { return c.dials() > n && c.state().Trouble != "" && !c.state().Busy })
	if got := c.dials() - n; got != 1 {
		t.Errorf("turned away for trying too often, it tried %d times", got)
	}
	c.then(joinsFirst())
	c.do(Join{"Bo"})
	c.on(PageWaiting)
	if c.keys[0] != c.keys[len(c.keys)-1] {
		t.Error("each try is another guest")
	}
}

// A connection lost as the guest joins leaves the join page as it was.
func TestAJoinThatIsLostIsTriedAgain(t *testing.T) {
	quick(t)
	s := joinsFirst()
	s.joinErr = guest.ErrLost
	c := newCourse(t, false, nil, s, joinsFirst())
	c.do(Join{"Bo"})
	c.on(PageWaiting)
	if c.dials() != 2 {
		t.Errorf("it dialled %d times", c.dials())
	}
}

// A tab that joined comes back by itself, as the same guest, and is asked
// nothing.
func TestAReloadComesBack(t *testing.T) {
	quick(t)
	s := newFake(in("p1"))
	c := newCourse(t, true, nil, s)
	if st := c.state(); st.Page != PageWaiting || !st.Lost {
		t.Errorf("it begins on %+v", st)
	}
	c.on(PageWindow)
	if s.named != "" || c.keys[0] != c.keep.key || !c.keep.joined {
		t.Errorf("it joined as %q, with another key: %v", s.named, c.keys[0] != c.keep.key)
	}
}

// A tab with a key the host never had joins as any other.
func TestAKeyTheHostNeverHadJoins(t *testing.T) {
	quick(t)
	c := newCourse(t, true, nil, joinsFirst(), joinsFirst())
	c.on(PageJoin)
	c.do(Join{"Bo"})
	c.on(PageWaiting)
	if c.state().Lost {
		t.Error("it says the connection is lost")
	}
}

// Each end there is no coming back from shows its page, and nothing more
// is tried. What the tab kept goes, where it is of no more use.
func TestHowAShareEnds(t *testing.T) {
	quick(t)
	for _, c := range []struct {
		name  string
		err   error
		act   Act
		keeps bool
	}{
		{"refused", guest.ErrRefused, ActNone, false},
		{"used", guest.ErrUsed, ActNone, false},
		{"expired", guest.ErrExpired, ActNone, false},
		{"stopped", &guest.EndedError{Reason: guest.ReasonStopped}, ActNone, false},
		{"window closed", &guest.EndedError{Reason: guest.ReasonWindowClosed}, ActNone, false},
		{"quitting", &guest.EndedError{Reason: guest.ReasonQuitting}, ActNone, false},
		{"away", &guest.EndedError{Reason: guest.ReasonAway}, ActNone, false},
		{"unanswered", &guest.EndedError{Reason: guest.ReasonUnanswered}, ActNone, false},
		{"another reason", &guest.EndedError{Reason: "whatever"}, ActNone, false},
		{"replaced", guest.ErrReplaced, ActHere, true},
		{"denied", guest.ErrDenied, ActNone, false},
		{"another host", fmt.Errorf("dial: %w", guest.ErrWrongHost), ActNone, true},
		{"gone from the relay", fmt.Errorf("join: %w", relay.RefusedGone), ActNone, false},
	} {
		for _, when := range []string{"dialled", "in"} {
			t.Run(c.name+" "+when, func(t *testing.T) {
				s := newFake(in("p1"))
				first := any(s)
				if when == "dialled" {
					first = c.err
				}
				cr := newCourse(t, true, nil, first, errors.New("dialled again"))
				if when == "in" {
					cr.on(PageWindow)
					s.end(c.err)
				}
				cr.on(PageOver)
				time.Sleep(15 * time.Millisecond)
				st := cr.state()
				if st.Over.Title == "" || st.Over.Act != c.act || (st.Over.Word != "") != (c.act != ActNone) {
					t.Errorf("it says %+v", st.Over)
				}
				if cr.keep.kept() != c.keeps {
					t.Errorf("the tab kept it: %v", cr.keep.kept())
				}
				if cr.dials() != 1 {
					t.Errorf("it dialled %d times", cr.dials())
				}
			})
		}
	}
}

// The host hears whether the guest follows it, and the pane it picked
// once it does not: as it comes in, as it changes, and as it comes back.
// While it follows, the panes it is in say nothing to the host.
func TestTheHostHearsWhetherTheGuestFollows(t *testing.T) {
	quick(t)
	s, back := newFake(in("p1", "p2")), newFake(in("p1", "p2"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	said := func(f *fakeSession, what string, want ...string) {
		t.Helper()
		c.until(what, func() bool {
			f.mu.Lock()
			defer f.mu.Unlock()
			return slices.Equal(f.focused, want)
		})
	}
	said(s, "following, as it came in", following)
	c.do(In{"p2"})
	c.do(In{"p1"})
	c.do(Follows{false})
	said(s, "in p1", following, "p1")
	c.do(In{"p2"})
	said(s, "in p2", following, "p1", "p2")
	c.do(Follows{true})
	said(s, "following again", following, "p1", "p2", following)
	c.then(back)
	s.end(guest.ErrLost)
	c.until("back", func() bool { return c.dials() == 2 && !c.state().Lost })
	said(back, "following, as it came back", following)
}

// A link that is not whole says so, and reaches nobody.
func TestABrokenLinkSaysSo(t *testing.T) {
	quick(t)
	c := newCourse(t, true, func(cfg *Config) { cfg.Broken = true }, errors.New("dialled"))
	c.do(Join{"Bo"})
	time.Sleep(10 * time.Millisecond)
	if st := c.state(); st.Page != PageOver || c.dials() != 0 {
		t.Errorf("it shows %+v, having dialled %d times", st, c.dials())
	}
}

// A lost connection is made again with the window kept meanwhile, the
// panes on the screen are opened again, and the host hears where the
// guest is.
func TestALostConnectionIsMadeAgain(t *testing.T) {
	quick(t)
	s, back := newFake(in("p1", "p2")), newFake(in("p1", "p2"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p1"}})
	c.do(In{"p1"})
	c.do(Follows{false})
	c.until("p1 open", func() bool { return slices.Equal(s.opened(), []string{"p1"}) })
	c.until("its terminal", func() bool { return c.f.shells.Get("p1") != nil })
	sh := c.f.shells.Get("p1")
	c.until("its screen", func() bool { return strings.Contains(sh.T.Text(), "screen of p1") })
	c.then(errors.New("no network"), errors.New("no network"), relay.RefusedAway, back)
	s.end(guest.ErrLost)
	c.until("lost", func() bool { return c.state().Lost })
	if st := c.state(); st.Page != PageWindow || len(st.Panes) != 2 {
		t.Errorf("while it is lost it shows %+v", st)
	}
	// Typed meanwhile, it is for nobody, and that is said.
	if !c.state().Shut["p1"] {
		t.Error("while it is lost it may be typed in")
	}
	sh.T.Send([]byte("lost"))
	c.until("back", func() bool { return !c.state().Lost })
	c.until("typed in again", func() bool { return !c.state().Shut["p1"] })
	if st := c.state(); st.Untyped["p1"] != 1 || st.Conn != 2 {
		t.Errorf("what was typed meanwhile was for nobody %d times, over connection %d", st.Untyped["p1"], st.Conn)
	}
	c.until("p1 open again", func() bool { return slices.Equal(back.opened(), []string{"p1"}) })
	if c.f.shells.Get("p1") != sh {
		t.Error("the terminal is another, with another history")
	}
	c.until("told where the guest is", func() bool {
		back.mu.Lock()
		defer back.mu.Unlock()
		return slices.Equal(back.focused, []string{"p1"})
	})
	sh.T.Send([]byte("x"))
	typed := func() string {
		back.panes[0].mu.Lock()
		defer back.panes[0].mu.Unlock()
		return string(back.panes[0].typed)
	}
	c.until("typed", func() bool { return typed() != "" })
	if got := typed(); got != "x" {
		t.Errorf("the pane was typed %q", got)
	}
}

// Why the host is not reached is said once it has been tried, and a host
// that is not on the relay is one to say.
func TestWhyAConnectionIsNotBack(t *testing.T) {
	quick(t)
	s := newFake(in("p1"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.then(relay.RefusedAway)
	s.end(guest.ErrLost)
	c.until("said why", func() bool { return c.state().Why != "" })
	c.then(errors.New("no network"))
	c.until("no more", func() bool { return c.state().Why == "" })
}

// A wake has the next try come now, and not again and again.
func TestAWakeTriesNow(t *testing.T) {
	quick(t)
	paces.tries = []time.Duration{time.Hour}
	paces.wakes = time.Second
	// A dial on its way is never stuck here, however slow the machine.
	paces.stuck = time.Hour
	s := newFake(in("p1"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.then(errors.New("no network"))
	s.end(guest.ErrLost)
	c.until("lost", func() bool { return c.state().Lost })
	time.Sleep(paces.wakes + 50*time.Millisecond)
	n := c.begun()
	// Each wake comes once the dial before it is over, when a try is due again.
	for range 20 {
		c.wake <- struct{}{}
		c.begun()
	}
	if got := c.begun() - n; got != 1 {
		t.Errorf("twenty wakes at once tried %d times", got)
	}
	// With the connection there, a wake is nothing.
	c.then(newFake(in("p1")))
	time.Sleep(paces.wakes + 50*time.Millisecond)
	c.wake <- struct{}{}
	c.until("back", func() bool { return !c.state().Lost })
	n = c.begun()
	time.Sleep(paces.wakes + 50*time.Millisecond)
	c.wake <- struct{}{}
	if c.begun() != n {
		t.Error("a wake dialled over a connection there is")
	}
}

// Lost for too long, the page gives up, and tries once more when asked.
func TestLostForTooLong(t *testing.T) {
	quick(t)
	paces.giveUp = 30 * time.Millisecond
	s := newFake(in("p1"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.then(errors.New("no network"))
	s.end(guest.ErrLost)
	c.on(PageOver)
	if st := c.state(); st.Over.Act != ActRetry || !c.keep.kept() {
		t.Errorf("it says %+v, and the tab kept its key: %v", st.Over, c.keep.kept())
	}
	c.do(Do{ActHere})
	c.then(newFake(in("p1")))
	c.do(Do{ActRetry})
	c.until("back", func() bool { st := c.state(); return st.Page == PageWindow && !st.Lost })
}

// Opened in another tab, the window is here again when asked for.
func TestUseHere(t *testing.T) {
	quick(t)
	s, back := newFake(in("p1")), newFake(in("p1"))
	c := newCourse(t, true, nil, s, back)
	c.on(PageWindow)
	s.end(guest.ErrReplaced)
	c.on(PageOver)
	c.do(Do{ActHere})
	c.until("here", func() bool { st := c.state(); return st.Page == PageWindow && !st.Lost })
}

// A wake has a dial that has had its time give way to another, and lets a
// new one be.
func TestAWakeLetsGoOfADeadDial(t *testing.T) {
	quick(t)
	s := newFake(in("p1"))
	var hung atomic.Int32
	c := newCourse(t, true, func(cfg *Config) {
		dial := cfg.Dial
		cfg.Dial = func(ctx context.Context, key guest.Key) (Session, error) {
			got, err := dial(ctx, key)
			if err != nil {
				// Dead: nothing comes until it is given up.
				hung.Add(1)
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return got, nil
		}
	}, s)
	c.on(PageWindow)
	c.then(errors.New("dead"))
	s.end(guest.ErrLost)
	c.until("dialled", func() bool { return hung.Load() == 1 })
	c.wake <- struct{}{}
	time.Sleep(5 * time.Millisecond)
	if n := hung.Load(); n != 1 {
		t.Errorf("a wake at once dialled %d times", n)
	}
	c.then(newFake(in("p1")))
	time.Sleep(paces.stuck)
	c.wake <- struct{}{}
	c.until("back", func() bool { return !c.state().Lost })
	n := c.dials()
	time.Sleep(20 * time.Millisecond)
	if c.dials() != n {
		t.Error("over a connection there is, it dialled on")
	}
}

// Called off while it waits for the host, the page says so, and not that
// the guest was in.
func TestCancelling(t *testing.T) {
	quick(t)
	s := joinsFirst()
	c := newCourse(t, false, nil, s)
	c.do(Join{"Bo"})
	c.on(PageWaiting)
	c.do(Leave{})
	c.on(PageOver)
	if got := c.state().Over; got != overCancelled {
		t.Errorf("it says %+v", got)
	}
	c.until("told", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.left })
}

// A reload of a tab that has been to the end says so, and reaches nobody.
func TestAReloadAfterTheEnd(t *testing.T) {
	quick(t)
	c := newCourse(t, false, func(cfg *Config) { cfg.Broken, cfg.Ended = true, true }, errors.New("dialled"))
	if st := c.state(); st.Page != PageOver || st.Over == overBroken || c.dials() != 0 {
		t.Errorf("it says %+v", st.Over)
	}
}

// A pane that is never to be opened is asked for ever less often.
func TestAPaneThatDoesNotOpenIsAskedForLess(t *testing.T) {
	quick(t)
	paces.again, paces.againMost = 5*time.Millisecond, 100*time.Millisecond
	s := newFake(in("p1"))
	var asked atomic.Int32
	s.paneErr, s.onPane = guest.ErrNotShared, func() { asked.Add(1) }
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p1"}})
	time.Sleep(500 * time.Millisecond)
	if n := asked.Load(); n < 3 || n > 12 {
		t.Errorf("it was asked for %d times", n)
	}
	// Opened and cut off over and over, too.
	s.mu.Lock()
	s.paneErr = nil
	s.onPane = func() {
		asked.Add(1)
		if n := len(s.panes); n > 0 {
			_ = s.panes[n-1].Close()
		}
	}
	s.mu.Unlock()
	s.say(in("p1"))
	c.until("open", func() bool { return len(s.opened()) == 1 })
	asked.Store(0)
	for end := time.Now().Add(500 * time.Millisecond); time.Now().Before(end); time.Sleep(time.Millisecond) {
		s.mu.Lock()
		for _, p := range s.panes {
			_ = p.Close()
		}
		s.mu.Unlock()
	}
	if n := asked.Load(); n > 12 {
		t.Errorf("cut off over and over, it was asked for %d times", n)
	}
}

// However often a program rings its bell, the page hears of it now and
// then, and waits for none of it.
func TestABellIsHeardNowAndThen(t *testing.T) {
	quick(t)
	paces.bells = time.Hour
	s := newFake(in("p1", "p2"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p1"}})
	c.until("shown", func() bool { return c.f.shells.Get("p1") != nil })
	was := runtime.NumGoroutine()
	for range 100_000 {
		c.f.ring(c.f.panes["p1"])
	}
	if n := runtime.NumGoroutine() - was; n > 10 {
		t.Errorf("a hundred thousand bells wait in %d goroutines", n)
	}
}

// Terminals kept for panes off the screen are few, the last ones shown.
func TestFewTerminalsAreKept(t *testing.T) {
	quick(t)
	paces.hidden, paces.kept = 0, time.Hour
	ids := names(mostKept + 10)
	s := newFake(in(ids...))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	for _, id := range ids {
		c.do(Shown{[]string{id}})
		c.until(id+" shown", func() bool { return c.f.shells.Get(id) != nil })
		time.Sleep(time.Millisecond)
	}
	c.do(Shown{nil})
	var got []string
	c.until("few kept", func() bool { got, _ = c.made(); return len(got) == mostKept })
	if slices.Contains(got, ids[0]) || !slices.Contains(got, ids[len(ids)-1]) {
		t.Errorf("kept are %v", got)
	}
}

// Leaving tells the host, where there is a connection to, and nothing is
// kept.
func TestLeaving(t *testing.T) {
	quick(t)
	s := newFake(in("p1"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Leave{})
	c.on(PageOver)
	c.until("told", func() bool { s.mu.Lock(); defer s.mu.Unlock(); return s.left })
	if c.keep.kept() || c.dials() != 1 {
		t.Errorf("the tab kept its key: %v, and dialled %d times", c.keep.kept(), c.dials())
	}

	away := newCourse(t, true, nil, errors.New("no network"))
	away.do(Leave{})
	away.on(PageOver)
	n := away.begun()
	time.Sleep(20 * time.Millisecond)
	if got := away.begun(); away.keep.kept() || got != n {
		t.Errorf("having left with no connection, it dials on: %d after %d", got, n)
	}
}

// Only the panes on the screen are open, and one off it for a while is
// closed.
func TestOnlyThePanesShownAreOpen(t *testing.T) {
	quick(t)
	s := newFake(in("p1", "p2", "p3"))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	time.Sleep(10 * time.Millisecond)
	if got := s.opened(); len(got) != 0 {
		t.Fatalf("with none on the screen %v are open", got)
	}
	c.do(Shown{[]string{"p1"}})
	c.until("p1 open", func() bool { return slices.Equal(s.opened(), []string{"p1"}) })
	c.do(Shown{[]string{"p2"}})
	c.until("p2 open too", func() bool { return slices.Equal(s.opened(), []string{"p1", "p2"}) })
	c.until("p1 closed", func() bool { return slices.Equal(s.opened(), []string{"p2"}) })
	c.do(Shown{[]string{"p1"}})
	c.until("p1 open again", func() bool { return slices.Contains(s.opened(), "p1") })
	// Gone from the window, it goes here.
	s.say(in("p2", "p3"))
	c.until("p1 gone", func() bool { return c.f.shells.Get("p1") == nil && !slices.Contains(s.opened(), "p1") })
}

// A pane cut off that still stands in the window is opened again, and so
// is one that could not be.
func TestAPaneCutOffIsOpenedAgain(t *testing.T) {
	quick(t)
	s := newFake(in("p1"))
	s.paneErr = guest.ErrNotShared
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p1"}})
	time.Sleep(20 * time.Millisecond)
	s.mu.Lock()
	s.paneErr = nil
	s.mu.Unlock()
	c.until("open", func() bool { return len(s.opened()) == 1 })
	_ = s.panes[0].Close()
	c.until("open again", func() bool { s.mu.Lock(); n := len(s.panes); s.mu.Unlock(); return n == 2 && len(s.opened()) == 1 })
}

// However fast tabs are gone through, panes are opened at a pace the host
// takes, and no more at once than it takes.
func TestPanesAreOpenedAtAPace(t *testing.T) {
	quick(t)
	paces.hidden = time.Hour
	var ids []string
	for i := range 90 {
		ids = append(ids, fmt.Sprint("p", i))
	}
	s := newFake(in(ids...))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	began := time.Now()
	for _, id := range ids {
		c.do(Shown{[]string{id}})
		time.Sleep(time.Millisecond)
	}
	c.until("the last one open", func() bool { return slices.Contains(s.opened(), ids[len(ids)-1]) })
	s.mu.Lock()
	n := len(s.panes)
	s.mu.Unlock()
	if most := opensBurst + int(time.Since(began)/opensEach) + 1; n > most {
		t.Errorf("%d panes were opened, over %d", n, most)
	}
	if got := len(s.opened()); got > mostOpen {
		t.Errorf("%d are open at once", got)
	}
}

// A host that shows no layout, as of a window too large to say, has its
// panes in one row of tabs, and one that shows a pane it does not list
// shows the rest.
func TestAWindowAsFarAsItIsSaid(t *testing.T) {
	quick(t)
	v := in("p1", "p2")
	v.State.Layout = nil
	s := newFake(v)
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	if l := c.state().Layout; !slices.Equal(l.Items(), []string{"p1", "p2"}) {
		t.Errorf("with no layout it shows %v", l.Items())
	}
	v = in("p1", "p2", "p3")
	v.State.Panes = v.State.Panes[:2]
	s.say(v)
	c.until("without p3", func() bool {
		l := c.state().Layout
		return slices.Equal(l.Items(), []string{"p1", "p2"}) && len(l.Windows[0].Root.Kids) == 2
	})
	v.State.Theme = []byte(`{"theme":{}}`)
	was := c.state().Theme.Name
	s.say(v)
	made, err := guestlook.Theme{Look: themeAt(t, 1), FontSize: 15}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if st := c.state(); st.Theme.Name != was || st.Themes != 0 {
		t.Errorf("a theme that could not be read is %q", st.Theme.Name)
	}
	v.State.Theme = made
	s.say(v)
	c.until("the host's theme", func() bool { st := c.state(); return st.Theme.Name == themeAt(t, 1).Name && st.Themes == 1 })
}

// What is typed while no pane is open is for nobody, which is heard of as
// the next is there, and a pane begins with its first screen: over another
// connection, with what waits to be sent dropped.
func TestAFeedGoesOnWithTheNextPane(t *testing.T) {
	f := newFeed()
	var ready []bool
	f.onReady = func(untyped bool) { ready = append(ready, untyped) }
	// A mouse moves over a pane that is not open, and that is no matter.
	_, _ = f.Write([]byte("\x1b[<35;1;1M"))
	b := make([]byte, 10)
	for i, fresh := range []bool{true, false, true} {
		if i == 1 {
			f.detach()
			if n, err := f.Write([]byte("nobody")); n != 6 || err != nil {
				t.Errorf("with no pane a write is %d, %v", n, err)
			}
		}
		p := newFakePane("p", "x")
		first, err := firstScreen(p)
		if err != nil {
			t.Fatal(err)
		}
		f.attach(p, first, fresh)
		if _, scr, err := f.ReadScreen(b); err != nil || scr != first || scr.Started != fresh {
			t.Errorf("a pane begins with %+v, %v", scr, err)
		}
		if len(ready) != i {
			t.Errorf("it is ready before its first screen is drawn: %v", ready)
		}
		p.screens <- &session.Screen{Cols: 5, Rows: 5}
		if _, scr, _ := f.ReadScreen(b); scr.Started {
			t.Error("the next screen drops what was typed too")
		}
		_, _ = f.Write([]byte("to p"))
		if got := string(p.typed); got != "to p" {
			t.Errorf("the pane was typed %q", got)
		}
		_ = p.Close()
	}
	if !slices.Equal(ready, []bool{false, true, false}) {
		t.Errorf("as each was ready, something was for nobody: %v", ready)
	}
	ended := make(chan bool, 1)
	f.onEnd = func(tooLarge bool) { ended <- tooLarge }
	read := make(chan error)
	p, late := newFakePane("p", "x"), newFakePane("late", "x")
	first, _ := firstScreen(p)
	f.attach(p, first, false)
	_ = p.Close()
	go func() {
		for {
			if _, _, err := f.ReadScreen(b); err != nil {
				read <- err
				return
			}
		}
	}()
	if <-ended {
		t.Error("a pane that ended was too large")
	}
	// One whose screen does not fit ends too, with nothing shown of it.
	f.fits = func(cols, rows int) bool { return cols*rows <= 400 }
	big := newFakePane("big", "x")
	big.screens <- &session.Screen{Cols: 100, Rows: 100}
	first, _ = firstScreen(big)
	f.attach(big, first, false)
	if !<-ended || !big.isClosed() {
		t.Error("a screen that does not fit is shown")
	}
	_ = f.Close()
	if err := <-read; err != io.EOF {
		t.Errorf("a closed feed reads %v", err)
	}
	if _, err := f.Write([]byte("x")); err == nil {
		t.Error("a closed feed is typed in")
	}
	f.attach(late, nil, false)
	if !late.isClosed() {
		t.Error("a pane that came too late is open")
	}
	_ = f.Wait()
}

// sized is a window of panes of one size, as a host lists them, whatever
// they are once opened.
func sized(cols, rows int, ids ...string) guest.View {
	v := in(ids...)
	for i := range v.State.Panes {
		v.State.Panes[i].Cols, v.State.Panes[i].Rows = cols, rows
	}
	return v
}

// names are n panes' IDs.
func names(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprint("p", i)
	}
	return out
}

// begun is how many dials the flow has begun, once it has taken every wake sent and has no dial on its way.
func (c *course) begun() int {
	type at struct {
		dial     int
		dialling bool
	}
	for {
		if len(c.wake) > 0 {
			runtime.Gosched()
			continue
		}
		got := make(chan at)
		c.f.post(func() { got <- at{c.f.dial, c.f.drop != nil} })
		if a := <-got; !a.dialling {
			return a.dial
		}
		runtime.Gosched()
	}
}

// made are the panes with a terminal, and taken what they all take.
func (c *course) made() (ids []string, taken int) {
	done := make(chan struct{})
	c.f.post(func() {
		defer close(done)
		for id, p := range c.f.panes {
			if p.sh != nil {
				ids = append(ids, id)
			}
		}
		taken = c.f.budget.most - c.f.budget.free("")
	})
	<-done
	slices.Sort(ids)
	return ids, taken
}

// A pane has a terminal from when it is opened, however many the host
// lists, and for a while after it was closed. The sizes are the host's to
// say, and none is believed.
func TestATerminalIsMadeAsItsPaneIsOpened(t *testing.T) {
	quick(t)
	s := newFake(sized(1000, 500, names(256)...))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	if ids, taken := c.made(); len(ids) != 0 || taken != 0 {
		t.Fatalf("with none opened, %d have terminals, which take %d bytes", len(ids), taken)
	}
	// Larger than is held at all, going by what is listed.
	c.do(Shown{[]string{"p3"}})
	c.until("too large", func() bool { return c.state().Held["p3"] == HeldLarge })
	if n := len(s.opened()); n != 0 {
		t.Errorf("one too large is opened")
	}
	s.say(sized(40, 10, names(256)...))
	c.until("shown", func() bool { return c.f.shells.Get("p3") != nil && len(c.state().Held) == 0 })
	if ids, _ := c.made(); !slices.Equal(ids, []string{"p3"}) {
		t.Errorf("%v have terminals", ids)
	}
	// Off the screen, it closes, and its terminal goes a while later.
	c.do(Shown{nil})
	c.until("closed", func() bool { return len(s.opened()) == 0 })
	c.until("gone", func() bool { ids, taken := c.made(); return len(ids) == 0 && taken == 0 && c.f.shells.Get("p3") == nil })
}

// The terminals take no more than there is for them, whatever sizes they
// turn out to have and come to have, and those on the screen go before
// the others.
func TestThePageHoldsNoMoreThanItMay(t *testing.T) {
	quick(t)
	// Room for five screens, and some.
	const cols, rows = 100, 50
	paces.room = 5*cols*rows*cellBytes + 100<<10
	paces.hidden, paces.kept = time.Hour, time.Hour
	ids := names(12)
	s := newFake(sized(10, 10, ids...))
	s.sizes = map[string][2]int{}
	for _, id := range ids {
		s.sizes[id] = [2]int{cols, rows}
	}
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	within := func(what string) {
		t.Helper()
		if made, taken := c.made(); taken > paces.room || len(made)*cols*rows*cellBytes > paces.room {
			t.Errorf("%s: %d terminals take %d bytes of %d", what, len(made), taken, paces.room)
		}
	}
	// One after the other, as tabs are gone through: the last is shown.
	for _, id := range ids {
		c.do(Shown{[]string{id}})
		c.until(id+" shown", func() bool { return c.f.shells.Get(id) != nil })
		within(id)
	}
	// All at once: as many as there is room for, and the one the guest is
	// in among them.
	c.do(In{"p11"})
	c.do(Shown{ids})
	c.until("five shown", func() bool { made, _ := c.made(); return len(made) == 5 && len(c.state().Held) == 7 })
	within("all")
	if c.state().Held["p11"] != 0 {
		t.Error("the one the guest is in is not shown")
	}
	c.do(In{"p9"})
	c.until("p9 shown", func() bool { return c.f.shells.Get("p9") != nil && c.state().Held["p9"] == 0 })
	within("another")
	// One grows on the screen, past what there is.
	s.mu.Lock()
	for _, p := range s.panes {
		if p.id == "p9" && !p.isClosed() {
			p.screens <- &session.Screen{Cols: 1000, Rows: 500}
		}
	}
	s.sizes["p9"] = [2]int{1000, 500}
	s.mu.Unlock()
	c.until("too large", func() bool { return c.state().Held["p9"] == HeldLarge })
	c.until("the rest shown", func() bool { made, _ := c.made(); return len(made) == 5 })
	within("grown")
}

// One that has room takes it in steps: history and images give way to the
// screens of others, and what is listed for those to come.
func TestHistoryGivesWayToScreens(t *testing.T) {
	const cols, rows = 100, 40
	screen := cols * rows * cellBytes
	if r := roomWith(cols, rows, 0); r.lines != 0 || r.images != 0 || r.bytes() != screen {
		t.Errorf("with nothing besides its screen a terminal has %+v", r)
	}
	for _, share := range []int{1, 100 << 10, 1 << 20, 10 << 20, 100 << 20} {
		if r := roomWith(cols, rows, share); r.spare() > share || r.lines > mostLines || r.images > mostImages {
			t.Errorf("with %d bytes besides its screen it has %+v, which is %d", share, r, r.spare())
		}
	}
	quick(t)
	paces.room = 3*screen + 8<<20
	s := newFake(sized(cols, rows, "p0", "p1", "p2"))
	s.sizes = map[string][2]int{"p0": {cols, rows}, "p1": {cols, rows}, "p2": {cols, rows}}
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	c.do(Shown{[]string{"p0"}})
	c.until("p0 shown", func() bool { return c.f.shells.Get("p0") != nil })
	if r := c.f.budget.of("p0"); r.lines < leastLines || r.images == 0 {
		t.Errorf("alone it has %+v", r)
	}
	c.do(Shown{[]string{"p0", "p1", "p2"}})
	c.until("all shown", func() bool { made, _ := c.made(); return len(made) == 3 && len(c.state().Held) == 0 })
}

// What the terminals take is what they are counted as, or less.
func TestATerminalTakesWhatItIsCountedAs(t *testing.T) {
	heap := func() int {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return int(m.HeapAlloc)
	}
	// About 20 MB.
	r := room{cols: 200, rows: 100, lines: 400, images: 0}
	was := heap()
	p := newPaneOf("p", r.cols, r.rows, "")
	sh := screen.Open(p, themeAt(t, 0).Palette, screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}, Scrollback: r.lines, Images: -1})
	defer sh.Close()
	for end := time.Now().Add(10 * time.Second); sh.T.Size().Cols != r.cols && time.Now().Before(end); time.Sleep(time.Millisecond) {
		sh.Drawn(func(*grid.Grid) {})
	}
	tm := termview.New(sh, shortcuts())
	// Each cell its own letter and colours, and more lines than are kept.
	line := strings.Repeat("\x1b[31;42mé\x1b[1;35;47mx", r.cols/2)
	for range r.lines + lineSlack + r.rows {
		sh.T.Say(line)
	}
	tm.Sync()
	if got := heap() - was; got > r.bytes() {
		t.Errorf("it takes %d bytes, and is counted as %d", got, r.bytes())
	} else {
		t.Logf("it takes %d bytes of the %d it is counted as", got, r.bytes())
	}
	runtime.KeepAlive(tm)
}

// More terminals on the screen than are opened at once leave the first of
// them open, with the one the guest is in before the others.
func TestMoreOnTheScreenThanAreOpened(t *testing.T) {
	quick(t)
	ids := names(mostOpen + 1)
	s := newFake(in(ids...))
	c := newCourse(t, true, nil, s)
	c.on(PageWindow)
	last := ids[mostOpen]
	c.do(Shown{ids})
	c.until("as many as may be open", func() bool { return len(s.opened()) == mostOpen })
	if got := c.state().Held; len(got) != 1 || got[last] != HeldMany {
		t.Errorf("held are %v", got)
	}
	c.do(In{last})
	c.until("the one the guest is in, open", func() bool { got := s.opened(); return len(got) == mostOpen && slices.Contains(got, last) })
}

// However a connection goes, the host is dialled no more often than a
// relay lets one address join.
func TestDialsKeepToAPace(t *testing.T) {
	ms := time.Millisecond
	// paced is a page whose times are a hundredth of what they are.
	paced := func(t *testing.T) {
		quick(t)
		paces.tries = []time.Duration{3 * ms, 10 * ms, 20 * ms, 40 * ms, 60 * ms}
		paces.held, paces.dials, paces.each = 300*ms, 5, 50*ms
		paces.wakes, paces.blocked = 30*ms, 300*ms
	}
	// within reports how many dials there were over a while, against what
	// a relay of as many at once, and one each so often, lets through.
	within := func(t *testing.T, c *course, began time.Time) {
		t.Helper()
		// Wait for the first dial, whose goroutine a busy machine may run late.
		c.until("dialled", func() bool { return c.dials() > 0 })
		took := time.Since(began)
		if n, most := c.dials(), 10+int(took/(40*ms)); n > most {
			t.Errorf("it dialled %d times in %v, and a relay takes %d", n, took, most)
		}
	}
	const while = 500
	// cut answers each dial with a connection that soon goes.
	cut := func(err error) func(*Config) {
		return func(cfg *Config) {
			dial := cfg.Dial
			cfg.Dial = func(ctx context.Context, key guest.Key) (Session, error) {
				_, _ = dial(ctx, key)
				s := newFake(in("p1"))
				time.AfterFunc(5*ms, func() { s.end(err) })
				return s, nil
			}
		}
	}
	for _, c := range []struct {
		name   string
		change func(*Config)
		answer any
		// poke is done over and over meanwhile.
		poke func(c *course)
	}{
		{"one that goes as soon as it is made", cut(guest.ErrLost), guest.ErrLost, nil},
		{"lost", nil, guest.ErrLost, nil},
		{"the host away", nil, relay.RefusedAway, nil},
		{"busy", nil, relay.RefusedBusy, nil},
		{"too many", nil, relay.RefusedTooMany, nil},
		{"woken over and over", nil, guest.ErrLost, func(c *course) {
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}},
		{"woken over and over, with too many", nil, relay.RefusedTooMany, func(c *course) {
			select {
			case c.wake <- struct{}{}:
			default:
			}
		}},
		{"two tabs that take it from each other", cut(guest.ErrReplaced), guest.ErrReplaced, func(c *course) { c.do(Do{ActHere}) }},
	} {
		t.Run(c.name, func(t *testing.T) {
			paced(t)
			began := time.Now()
			cr := newCourse(t, true, c.change, c.answer)
			for time.Since(began) < while*ms {
				if c.poke != nil {
					c.poke(cr)
				}
				time.Sleep(2 * ms)
			}
			within(t, cr, began)
			if c.name == "too many" && cr.dials() > 2+while/300 {
				t.Errorf("told there were too many, it dialled %d times", cr.dials())
			}
		})
	}
	// One that goes each time is made ever less often, whatever the pace.
	t.Run("one that goes each time", func(t *testing.T) {
		paced(t)
		paces.dials = 1000
		c := newCourse(t, true, cut(guest.ErrLost), guest.ErrLost)
		time.Sleep(while * ms)
		if n := c.dials(); n > 5+while/60+2 {
			t.Errorf("it dialled %d times", n)
		}
	})
	// One that held for a good while is made again at once.
	t.Run("one that held", func(t *testing.T) {
		paced(t)
		paces.held = 20 * ms
		// What the fifth try would wait, had it not started over.
		paces.tries[4] = time.Minute
		s := newFake(in("p1"))
		c := newCourse(t, true, nil, errors.New("no network"), errors.New("no network"), errors.New("no network"), errors.New("no network"), s)
		c.on(PageWindow)
		time.Sleep(50 * ms)
		c.then(newFake(in("p1")))
		n := c.dials()
		s.end(guest.ErrLost)
		c.until("back", func() bool { return c.dials() > n && !c.state().Lost })
	})
}

func TestAboutNamesTheBrowserAndTheSystem(t *testing.T) {
	for agent, want := range map[string]string{
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36":                                   "Chrome on Linux",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0":           "Edge on Windows",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10.15; rv:143.0) Gecko/20100101 Firefox/143.0":                                                    "Firefox on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1": "Safari on iPhone",
		"Mozilla/5.0 (Linux; Android 15; Pixel 9) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Mobile Safari/537.36":                   "Chrome on Android",
		"curl/8.0": "",
	} {
		if got := About(agent); got != want {
			t.Errorf("%s is %q, want %q", agent, got, want)
		}
	}
}
