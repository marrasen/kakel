package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/themes"
	uiterm "github.com/marrasen/kakel/ui/term"
	"github.com/marrasen/kakel/vt"
)

// said is a session whose program says what a test has it say, and keeps
// what it is sent.
type said struct {
	out  chan []byte
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	got  []byte
	rest []byte
}

func newSaid() *said { return &said{out: make(chan []byte, 64), done: make(chan struct{})} }

func (s *said) say(text string) { s.out <- []byte(text) }

func (s *said) Read(p []byte) (int, error) {
	if len(s.rest) == 0 {
		select {
		case s.rest = <-s.out:
		case <-s.done:
			return 0, io.EOF
		}
	}
	n := copy(p, s.rest)
	s.rest = s.rest[n:]
	return n, nil
}

func (s *said) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, p...)
	return len(p), nil
}

func (s *said) sent() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return string(s.got)
}

func (s *said) Close() error          { s.once.Do(func() { close(s.done) }); return nil }
func (s *said) Resize(int, int) error { return nil }
func (s *said) Wait() error           { return nil }

// webClient is a web client to give a relay: one that does nothing.
func webClient(t *testing.T) func() (zipped, sig []byte, ok bool) {
	t.Helper()
	var wasm bytes.Buffer
	z := gzip.NewWriter(&wasm)
	if _, err := z.Write([]byte("\x00asm\x01\x00\x00\x00")); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	zipped, _, err := relay.MakeBundle(map[string][]byte{
		"index.html": []byte("<!doctype html><title>kakel</title>"), "wasm_exec.js": []byte("// runs it"), "client.wasm.gz": wasm.Bytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return func() ([]byte, []byte, bool) { return zipped, nil, true }
}

// openRelay is a relay that takes any web client, and its URL.
func openRelay(t *testing.T) string {
	t.Helper()
	secret := relay.NewSecret()
	s := relay.NewServer(secret)
	s.AllowUnsigned = true
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	return ts.URL + "/k/" + secret
}

// linkRig is a kakel with two windows, the first of them to share, and a
// terminal of a test's in each.
type linkRig struct {
	t        *testing.T
	a        *app
	one, two *ownWin
	url      string
	// progs are the programs in the panes, by pane.
	progs map[string]*said
	// logged is the Window Log.
	logged *bytes.Buffer
}

// newLinkRig makes one. The first window holds the terminals p1 and p2
// and the file manager p3, the second the terminal p4 and the secrets.
func newLinkRig(t *testing.T) *linkRig {
	t.Helper()
	r := &linkRig{t: t, url: openRelay(t), progs: map[string]*said{}, logged: &bytes.Buffer{}}
	out := &lockedWriter{w: io.MultiWriter(r.logged, testWriter{t})}
	log.SetOutput(out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	a := newApp(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), screen.NewShells())
	a.ctx = t.Context()
	a.settings = mustSettings(t)
	a.links.client = webClient(t)
	a.palette = vt.DefaultPalette()
	for _, th := range themes.Built() {
		made, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		a.themes = append(a.themes, made)
	}
	r.a, r.one = a, a.cur
	a.pickTheme(a.themes[0].Name)
	r.terminal("p1", "first")
	r.terminal("p2", "second")
	a.addPane(Pane{ID: "p3", Title: "/home/anna/PRIVATE-FOLDER", Kind: KindFileManager}, nil, Placement{})
	r.two = a.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	a.front(r.two)
	r.terminal("p4", "ELSEWHERE")
	a.addPane(Pane{ID: "p5", Title: "SECRET-NAME", Kind: KindSecrets}, nil, Placement{})
	a.front(r.one)
	a.publish()
	t.Cleanup(func() {
		a.endLinks(guest.ReasonQuitting)
		r.drain()
		a.waitForLinks()
		for len(a.st.Panes) > 0 {
			a.remove(a.st.Panes[0].ID)
		}
	})
	return r
}

// lockedWriter has one writer at a time.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// testWriter writes to a test's log, while it runs.
type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) {
	if w.t.Context().Err() == nil {
		w.t.Log(strings.TrimSpace(string(p)))
	}
	return len(p), nil
}

// drain does what waits to be done.
func (r *linkRig) drain() {
	for {
		select {
		case f := <-r.a.events:
			f()
		case <-time.After(20 * time.Millisecond):
			return
		}
	}
}

// settle gives what is on its way the time to arrive.
func (r *linkRig) settle() {
	end := time.Now().Add(300 * time.Millisecond)
	r.until("a moment", func() bool { return time.Now().After(end) })
}

// terminal opens a terminal pane with a program of the test's.
func (r *linkRig) terminal(id, title string) {
	prog := newSaid()
	r.progs[id] = prog
	r.a.addPane(Pane{ID: id, Title: title}, screen.Open(prog, r.a.palette, r.a.hooks(id)), Placement{})
}

// until runs the program, publishing after each event as it does, until
// ok.
func (r *linkRig) until(what string, ok func() bool) {
	r.t.Helper()
	defer stuckAfter(what, 40*time.Second)()
	for end := time.Now().Add(20 * time.Second); !ok(); {
		select {
		case f := <-r.a.events:
			f()
			if !r.a.gone {
				r.a.publish()
			}
		case <-time.After(5 * time.Millisecond):
		}
		if time.Now().After(end) {
			r.t.Fatalf("waited for %s: %+v", what, r.a.windowLinks())
		}
	}
}

// do has the program carry out in, as the run loop does.
func (r *linkRig) do(in any) {
	r.a.handle(in)
	r.a.publish()
}

// only is the one share there is.
func (r *linkRig) only() WindowLink {
	r.t.Helper()
	all := r.a.windowLinks()
	if len(all) != 1 {
		r.t.Fatalf("the shares are %+v", all)
	}
	return all[0]
}

// share shares window one, and returns the share once its link can be
// opened.
func (r *linkRig) share(canType bool) WindowLink {
	r.t.Helper()
	n := len(r.a.windowLinks())
	r.do(ShareWindow{Window: r.one.id, Name: "Anna", CanType: canType, Relay: r.url})
	r.until("the link", func() bool {
		all := r.a.windowLinks()
		return len(all) > n && all[len(all)-1].Stands == LinkWaiting
	})
	all := r.a.windowLinks()
	return all[len(all)-1]
}

// visitor is a guest, which keeps everything it is ever told.
type visitor struct {
	r    *linkRig
	link relay.ShareLink
	key  guest.Key
	s    *guest.Session

	mu   sync.Mutex
	told []string
	seen bytes.Buffer
}

// visit opens the link as a guest does, with nothing asked yet.
func (r *linkRig) visit(link string) *visitor {
	r.t.Helper()
	l, err := relay.ParseShareLink(link)
	if err != nil {
		r.t.Fatal(err)
	}
	key, err := guest.NewKey()
	if err != nil {
		r.t.Fatal(err)
	}
	v := &visitor{r: r, link: l, key: key}
	if err := v.dial(); err != nil {
		r.t.Fatal(err)
	}
	return v
}

// dial connects the guest to the host, again if it has been.
func (v *visitor) dial() error {
	secret, err := guest.ParseSecret(v.link.Secret)
	if err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(v.r.t.Context(), 15*time.Second)
		defer cancel()
		conn, err := relay.JoinShare(ctx, v.link, relay.Dialer{})
		if err == nil {
			v.s, err = guest.Dial(ctx, conn, guest.Options{Secret: secret, Key: v.key})
		}
		done <- err
	}()
	var got error
	v.r.until("the guest to connect", func() bool {
		select {
		case got = <-done:
			return true
		default:
			return false
		}
	})
	if got != nil {
		return got
	}
	s := v.s
	v.r.t.Cleanup(func() { _ = s.Close() })
	go func() {
		for {
			select {
			case <-s.Changed():
				v.keep(s)
			case <-s.Done():
				return
			}
		}
	}()
	return nil
}

// keep keeps what the guest is told now.
func (v *visitor) keep(s *guest.Session) {
	raw, err := json.Marshal(s.View().State)
	if err != nil {
		panic(err)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	v.told = append(v.told, string(raw))
}

// everything is all the guest has decoded: each state, and what each
// pane drew.
func (v *visitor) everything() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return strings.Join(v.told, "\n") + v.seen.String()
}

// join asks to come in, and waits for the question.
func (v *visitor) join(id string) {
	v.r.t.Helper()
	done := make(chan error, 1)
	go func() { done <- v.s.Join(v.r.t.Context(), "Bob", "Firefox on Linux") }()
	v.r.until("the question", func() bool {
		l := v.r.a.linkByID(id)
		return l != nil && l.stands == LinkAsking
	})
	if err := <-done; err != nil {
		v.r.t.Fatal(err)
	}
}

// in joins and is let in.
func (v *visitor) in(id string, canType bool) {
	v.r.t.Helper()
	v.join(id)
	v.r.do(AnswerGuest{ID: id, Allow: true, CanType: canType})
	v.r.until("the guest to be in", func() bool { return v.s.View().Phase == guest.PhaseIn && v.r.a.linkByID(id).stands == LinkIn })
}

// state waits for a state of the window that ok is true of.
func (v *visitor) state(what string, ok func(guest.State) bool) guest.State {
	v.r.t.Helper()
	v.r.until(what, func() bool { return ok(v.s.View().State) })
	return v.s.View().State
}

// open opens a pane, and keeps what it draws.
func (v *visitor) open(id string) (*guest.ShownPane, error) {
	v.r.t.Helper()
	type got struct {
		p   *guest.ShownPane
		err error
	}
	done := make(chan got, 1)
	go func() {
		p, err := v.s.Pane(v.r.t.Context(), id)
		done <- got{p, err}
	}()
	var g got
	v.r.until("pane "+id, func() bool {
		select {
		case g = <-done:
			return true
		default:
			return false
		}
	})
	if g.err != nil {
		return nil, g.err
	}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := g.p.Read(buf)
			v.mu.Lock()
			v.seen.Write(buf[:n])
			v.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return g.p, nil
}

// sees waits for the guest to have been shown text.
func (v *visitor) sees(text string) {
	v.r.t.Helper()
	v.r.until("the guest to see "+text, func() bool { return strings.Contains(v.everything(), text) })
}

// paneIDs are the panes of a state.
func paneIDs(st guest.State) []string {
	var out []string
	for _, p := range st.Panes {
		out = append(out, p.ID)
	}
	return out
}

// A guest comes in by a link once it is allowed, and is shown the
// terminals of that window, and nothing of any other pane or window.
func TestAGuestSeesTheWindowSharedAndNothingElse(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	if !strings.HasPrefix(l.Link, strings.SplitN(r.url, "/k/", 2)[0]+"/s/") {
		t.Fatalf("the link is %q", l.Link)
	}
	if strings.Contains(l.Link, "/k/") {
		t.Error("the link has the relay's secret in it")
	}
	v := r.visit(l.Link)
	if got := v.s.View(); got.Phase != guest.PhaseJoin || len(got.State.Panes) > 0 {
		t.Fatalf("before joining it is told %+v", got)
	}
	v.join(l.ID)
	if got := r.only(); got.Guest != "Bob" || got.About != "Firefox on Linux" || got.From != "127.0.0.1" || got.Link != "" {
		t.Errorf("asking, the share reads %+v", got)
	}
	if v.s.View().Phase == guest.PhaseIn || v.everything() != "" && strings.Contains(v.everything(), "first") {
		t.Error("the guest is told of the window before it was let in")
	}
	r.do(AnswerGuest{ID: l.ID, Allow: true, CanType: true})
	st := v.state("the window", func(st guest.State) bool { return len(st.Panes) == 3 })
	if got := paneIDs(st); !slices.Equal(got, []string{"p1", "p2", "p3"}) {
		t.Fatalf("it is shown %v", got)
	}
	if st.HostName != "Anna" || !st.CanType {
		t.Errorf("the state is %+v", st)
	}
	for _, p := range st.Panes {
		if want := p.ID != "p3"; (p.Kind == guest.KindTerminal) != want {
			t.Errorf("%s is a %s", p.ID, p.Kind)
		}
	}
	lay, err := guestlook.ReadLayout(st.Layout)
	if err != nil || !slices.Equal(lay.Items(), []string{"p1", "p2", "p3"}) {
		t.Errorf("the layout holds %v, %v", lay.Items(), err)
	}
	th, err := guestlook.ReadTheme(st.Theme)
	if err != nil || th.Look.Palette != r.a.themes[0].Palette || th.FontSize != defaultFontSize {
		t.Errorf("the theme: %v", err)
	}
	if _, err := v.open("p1"); err != nil {
		t.Fatal(err)
	}
	r.progs["p1"].say("hello from the first")
	v.sees("hello from the first")
	for _, id := range []string{"p3", "p4", "p5", "nope", ""} {
		if p, err := v.open(id); err == nil {
			_ = p.Close()
			t.Errorf("pane %q was opened", id)
		}
	}
	r.progs["p4"].say("ELSEWHERE says this")
	r.do(FontSize{Step: 1})
	v.state("the text's size", func(st guest.State) bool {
		th, err := guestlook.ReadTheme(st.Theme)
		return err == nil && th.FontSize == defaultFontSize+1
	})
	for _, never := range []string{"ELSEWHERE", "SECRET-NAME", "PRIVATE-FOLDER", "p4", "p5", "/k/"} {
		if strings.Contains(v.everything(), never) {
			t.Errorf("the guest was told %q", never)
		}
	}
	for _, never := range []string{l.Link, strings.SplitN(l.Link, "#", 2)[1], strings.SplitN(r.url, "/k/", 2)[1]} {
		if strings.Contains(r.logged.String(), never) {
			t.Errorf("the Window Log has a secret in it: %s", r.logged)
		}
	}
	for _, want := range []string{"asks to join", "was let in", "Bob", "127.0.0.1"} {
		if !strings.Contains(r.logged.String(), want) {
			t.Errorf("the Window Log has no %q", want)
		}
	}
}

// A pane moved out of the shared window is the guest's no more by the
// time it stands in another, and one moved in is.
func TestAPaneThatLeavesTheWindowIsWithdrawn(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	v.state("the window", func(st guest.State) bool { return len(st.Panes) == 3 })
	p1, err := v.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	r.progs["p1"].say("while shared")
	v.sees("while shared")

	r.a.moveToWindow("p1", r.two)
	r.a.publish()
	// At once: the program says this as soon as the pane has moved.
	r.progs["p1"].say("AFTER-IT-LEFT")
	_, _ = p1.Write([]byte("typed-after"))
	st := v.state("the pane to go", func(st guest.State) bool { return len(st.Panes) == 2 })
	if got := paneIDs(st); !slices.Equal(got, []string{"p2", "p3"}) {
		t.Fatalf("it is shown %v", got)
	}
	if _, err := v.open("p1"); !errors.Is(err, guest.ErrNotShared) {
		t.Errorf("opening it again says %v", err)
	}

	// And one that comes is shown.
	r.a.moveToWindow("p4", r.one)
	r.a.publish()
	v.state("the pane moved in", func(st guest.State) bool { return slices.Contains(paneIDs(st), "p4") })
	if _, err := v.open("p4"); err != nil {
		t.Fatal(err)
	}
	r.progs["p4"].say("now shared")
	v.sees("now shared")
	if got := v.everything(); strings.Contains(got, "AFTER-IT-LEFT") {
		t.Error("the guest was shown what the pane said after it left")
	}
	if got := r.progs["p1"].sent(); strings.Contains(got, "typed-after") {
		t.Errorf("the guest typed %q in a pane that left", got)
	}

	// Back, as the same pane: opened anew.
	r.a.moveToWindow("p1", r.one)
	r.a.publish()
	v.state("the pane back", func(st guest.State) bool { return slices.Contains(paneIDs(st), "p1") })
	if _, err := v.open("p1"); err != nil {
		t.Fatal(err)
	}
}

// Torn off into a window of its own, closed, or with its program ended
// and started again, a pane is the guest's as the window it stands in
// is.
func TestWhatIsSharedFollowsThePanes(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	v.state("the window", func(st guest.State) bool { return len(st.Panes) == 3 })
	if _, err := v.open("p2"); err != nil {
		t.Fatal(err)
	}

	// A pane opened here later, and renamed.
	r.a.front(r.one)
	r.terminal("p6", "third")
	r.a.publish()
	v.state("the pane opened later", func(st guest.State) bool { return slices.Contains(paneIDs(st), "p6") })
	r.do(RenamePane{Pane: "p6", Title: "renamed"})
	v.state("its name", func(st guest.State) bool {
		return slices.ContainsFunc(st.Panes, func(p guest.Pane) bool { return p.Title == "renamed" })
	})
	// A command's title is its command line.
	prog := newSaid()
	r.progs["p7"] = prog
	r.a.addPane(Pane{ID: "p7", Title: "mysql -pHUNTER2", Command: true}, screen.Open(prog, r.a.palette, r.a.hooks("p7")), Placement{})
	r.a.publish()
	v.state("the command", func(st guest.State) bool { return slices.Contains(paneIDs(st), "p7") })
	if strings.Contains(v.everything(), "HUNTER2") {
		t.Error("the guest was told a command line")
	}
	// In a split, and focused.
	r.do(MovePane{Pane: "p6", Beside: "p2"})
	r.do(FocusPane{Pane: "p6"})
	v.state("the split", func(st guest.State) bool {
		lay, err := guestlook.ReadLayout(st.Layout)
		return err == nil && lay.Windows[0].Root.Kind == dock.Row && st.HostIn == "p6"
	})
	// With a pane in front that is not shared, its title is not said.
	r.do(FocusPane{Pane: "p3"})
	st := v.state("the files in front", func(st guest.State) bool { return st.HostIn == "p3" })
	if i := slices.IndexFunc(st.Panes, func(p guest.Pane) bool { return p.ID == "p3" }); i < 0 || st.Panes[i].Title != "Files" {
		t.Errorf("the files are told as %+v", st.Panes)
	}

	// Closed.
	r.do(ClosePane{Pane: "p6"})
	v.state("it to close", func(st guest.State) bool { return !slices.Contains(paneIDs(st), "p6") })

	// Torn off.
	three := r.a.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	r.a.moveToWindow("p2", three)
	r.a.publish()
	r.progs["p2"].say("TORN-OFF")
	v.state("it to be torn off", func(st guest.State) bool { return !slices.Contains(paneIDs(st), "p2") })

	// The program ended: the pane is shown, and says so; one started in
	// its place is the guest's too.
	again, err := v.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.progs["p1"].Close()
	v.state("the program to end", func(st guest.State) bool {
		return slices.ContainsFunc(st.Panes, func(p guest.Pane) bool { return p.ID == "p1" && p.Ended })
	})
	next := newSaid()
	if err := r.a.terminal("p1").Restart(next); err != nil {
		t.Fatal(err)
	}
	r.a.setPane("p1", func(p *Pane) { p.Ended = false })
	r.a.publish()
	next.say("the next program")
	v.sees("the next program")
	_ = again.Close()
	if strings.Contains(v.everything(), "TORN-OFF") {
		t.Error("the guest was shown a pane torn off")
	}
}

// Whether the guest may type is the host's to say, at any time.
func TestTheHostSaysWhetherAGuestTypes(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	// Asked for with typing, and let in to watch.
	v.in(l.ID, false)
	if st := v.state("the window", func(st guest.State) bool { return len(st.Panes) == 3 }); st.CanType {
		t.Error("let in to watch, the guest may type")
	}
	p, err := v.open("p1")
	if err != nil {
		t.Fatal(err)
	}
	r.progs["p1"].say("ready")
	v.sees("ready")
	_, _ = p.Write([]byte("watching"))
	r.settle()
	r.do(SetLinkMay{ID: l.ID, CanType: true})
	v.state("may type", func(st guest.State) bool { return st.CanType })
	_, _ = p.Write([]byte("typing"))
	r.until("what it typed", func() bool { return strings.Contains(r.progs["p1"].sent(), "typing") })
	if got := r.progs["p1"].sent(); strings.Contains(got, "watching") {
		t.Errorf("the program was sent %q", got)
	}
	r.until("where it types", func() bool { return r.only().TypedIn == "p1" && !r.only().TypedAt.IsZero() })
	v.s.Focus("p2")
	r.until("where it is", func() bool { return r.only().In == "p2" })

	r.do(SetLinkMay{ID: l.ID})
	v.state("may only watch", func(st guest.State) bool { return !st.CanType })
	_, _ = p.Write([]byte("again"))
	r.settle()
	if got := r.progs["p1"].sent(); strings.Contains(got, "again") {
		t.Errorf("the program was sent %q", got)
	}
}

// A guest that follows the host is in the pane the host is in, as the
// host moves, with nothing said by the guest: a page in a hidden tab draws
// nothing, and says nothing.
func TestAGuestThatFollowsIsInTheHostsPane(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	v.state("the window", func(st guest.State) bool { return len(st.Panes) == 3 })
	v.s.Follow()
	for _, id := range []string{"p2", "p1", "p2"} {
		r.do(FocusPane{Pane: id})
		r.until("the guest in "+id, func() bool { got := r.only(); return got.In == id && got.Follows })
	}
	// A pane of its own holds it.
	v.s.Focus("p1")
	r.until("the guest in p1", func() bool { got := r.only(); return got.In == "p1" && !got.Follows })
	r.do(FocusPane{Pane: "p2"})
	r.settle()
	if got := r.only(); got.In != "p1" {
		t.Errorf("the host moved, and the guest is in %q", got.In)
	}
}

// A guest is told how large the window's panes are laid out, and a cell,
// as the window says, and again as it is resized; another window's
// saying so tells it nothing.
func TestAGuestIsToldHowLargeThePanesAre(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	sized := func(w *ownWin, dock, cell geom.Size) {
		if !r.a.overviewIn(w, StageSized{At: geom.Rc(0, 30, dock.W, dock.H-30), Window: dock, Dock: dock, Cell: cell}) {
			t.Fatal("the window's size was not taken")
		}
	}
	sized(r.one, geom.Sz(1200, 700), geom.Sz(8.5, 17))
	v.state("the panes' size", func(st guest.State) bool {
		return st.Room == guest.Size{W: 1200, H: 700} && st.Cell == guest.Size{W: 8.5, H: 17}
	})
	sized(r.two, geom.Sz(500, 400), geom.Sz(10, 20))
	if told := r.a.linkByID(l.ID).told.Room; told != (guest.Size{W: 1200, H: 700}) {
		t.Errorf("another window resized, and the guest was told %v", told)
	}
	sized(r.one, geom.Sz(900, 600), geom.Sz(8.5, 17))
	st := v.state("the panes resized", func(st guest.State) bool { return st.Room == guest.Size{W: 900, H: 600} })
	if st.Cell != (guest.Size{W: 8.5, H: 17}) {
		t.Errorf("the guest was told a cell is %v", st.Cell)
	}
}

// A guest is told of a theme before the terminals are drawn in it.
func TestAThemeIsToldBeforeTheTerminalsHaveIt(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	var drawn []string
	stop := r.a.terminal("p1").Follow(&themeSpy{stale: func() {
		th, err := guestlook.ReadTheme(r.a.linkByID(l.ID).told.Theme)
		if err != nil {
			t.Error(err)
			return
		}
		drawn = append(drawn, th.Look.Name)
	}})
	defer stop()
	other := r.a.themes[1].Name
	r.do(PreviewTheme{Name: other})
	r.do(PreviewTheme{})
	if want := []string{other, r.a.themes[0].Name}; !slices.Equal(drawn, want) {
		t.Errorf("as the colours changed the guest had been told %v, want %v", drawn, want)
	}
}

// themeSpy hears a terminal's colours change.
type themeSpy struct{ stale func() }

func (*themeSpy) Screen(int, int, []byte) {}
func (*themeSpy) Write([]byte)            {}
func (*themeSpy) Ended()                  {}
func (*themeSpy) Started()                {}
func (s *themeSpy) Stale()                { s.stale() }

var _ uiterm.Follower = (*themeSpy)(nil)

// ended waits for a guest to be told it is over, and why.
func (v *visitor) ended() guest.Reason {
	v.r.t.Helper()
	s := v.s
	v.r.until("the guest to hear it is over", func() bool {
		select {
		case <-s.Done():
			return true
		default:
			return false
		}
	})
	var over *guest.EndedError
	if !errors.As(s.Err(), &over) {
		v.r.t.Fatalf("it ended with %v", s.Err())
	}
	return over.Reason
}

// Each way a share ends is the one the guest is told, and what its card
// keeps.
func TestHowAShareEnds(t *testing.T) {
	for _, c := range []struct {
		name string
		do   func(r *linkRig, l WindowLink, v *visitor)
		want guest.Reason
		gone bool
	}{
		{"stopped", func(r *linkRig, l WindowLink, _ *visitor) { r.do(StopLink{ID: l.ID}) }, guest.ReasonStopped, false},
		{"all stopped", func(r *linkRig, _ WindowLink, _ *visitor) { r.do(StopLinks{Window: r.one.id}) }, guest.ReasonStopped, false},
		{"left", func(_ *linkRig, _ WindowLink, v *visitor) { v.s.Leave() }, guest.ReasonLeft, false},
		{"window closed", func(r *linkRig, _ WindowLink, _ *visitor) {
			r.a.front(r.one)
			r.do(CloseWindow{})
			if len(r.a.st.Asks) != 1 {
				r.t.Fatal("a window with a guest closed with nothing asked")
			}
			r.do(AskAnswered{ID: r.a.st.Asks[0].ID, Yes: true})
		}, guest.ReasonWindowClosed, true},
		{"window gone", func(r *linkRig, _ WindowLink, _ *visitor) { r.a.windowClosed(r.one) }, guest.ReasonWindowClosed, true},
		{"its panes closed", func(r *linkRig, _ WindowLink, _ *visitor) {
			for _, id := range []string{"p1", "p2", "p3"} {
				r.do(ClosePane{Pane: id})
			}
			r.a.leaveEmpty()
		}, guest.ReasonWindowClosed, true},
		{"exit", func(r *linkRig, _ WindowLink, _ *visitor) { r.a.leave() }, guest.ReasonQuitting, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newLinkRig(t)
			l := r.share(true)
			v := r.visit(l.Link)
			v.in(l.ID, true)
			c.do(r, l, v)
			switch {
			case c.want == guest.ReasonLeft:
				r.until("the share to end", func() bool { return r.only().Stands == LinkEnded })
			default:
				if got := v.ended(); got != c.want {
					t.Errorf("the guest is told %q, want %q", got, c.want)
				}
			}
			if c.gone {
				if got := r.a.windowLinks(); len(got) != 0 {
					t.Errorf("of a window closed there are the shares %+v", got)
				}
			} else if got := r.only(); got.Live() || got.Over == "" || got.Link != "" {
				t.Errorf("it stands at %+v", got)
			}
			if err := v.dial(); err == nil {
				t.Error("the guest came back to a share that is over")
			}
			r.a.clearLinks(r.one.id)
			if got := r.a.windowLinks(); len(got) != 0 {
				t.Errorf("cleared, there are %+v", got)
			}
		})
	}
}

// Refused, a guest is told so, and the link is spent.
func TestAGuestRefused(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	v.join(l.ID)
	r.do(AnswerGuest{ID: l.ID, CanType: true})
	if got := v.ended(); got != guest.ReasonRefused {
		t.Errorf("it is told %q", got)
	}
	r.until("the share to be over", func() bool { return r.only().Stands == LinkEnded })
	if strings.Contains(v.everything(), "first") {
		t.Error("a guest refused was told of the window")
	}
	// Answered again, too late, it changes nothing.
	r.do(AnswerGuest{ID: l.ID, Allow: true})
	if got := r.only(); got.Live() {
		t.Errorf("it stands at %+v", got)
	}
}

// A link nobody opens, and a question nobody answers, end by themselves.
func TestAShareEndsByItself(t *testing.T) {
	r := newLinkRig(t)
	r.a.links.times = guest.HostConfig{LinkFor: 300 * time.Millisecond}
	l := r.share(true)
	if l.Until.IsZero() {
		t.Error("it does not say when the link expires")
	}
	r.until("the link to expire", func() bool { return r.only().Stands == LinkEnded })
	r.a.clearLinks(r.one.id)

	r.a.links.times = guest.HostConfig{AskFor: 300 * time.Millisecond}
	l = r.share(true)
	v := r.visit(l.Link)
	v.join(l.ID)
	if got := v.ended(); got != guest.ReasonUnanswered {
		t.Errorf("it is told %q", got)
	}
	r.until("the question to be over", func() bool { return r.only().Stands == LinkEnded })
}

// A link is opened once: whoever comes with it after the first is told
// it is used.
func TestALinkWorksOnce(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	other := r.visitErr(l.Link)
	if !errors.Is(other, guest.ErrUsed) {
		t.Errorf("a second guest is told %v", other)
	}
	if err := r.a.copyLink(l.ID); err == nil {
		t.Error("a link that is used was copied")
	}
}

// visitErr is what a guest who opens the link is told at once.
func (r *linkRig) visitErr(link string) error {
	r.t.Helper()
	l, err := relay.ParseShareLink(link)
	if err != nil {
		r.t.Fatal(err)
	}
	key, err := guest.NewKey()
	if err != nil {
		r.t.Fatal(err)
	}
	return (&visitor{r: r, link: l, key: key}).dial()
}

// Several shares, two of one window, go their own ways.
func TestSeveralSharesAtOnce(t *testing.T) {
	r := newLinkRig(t)
	first, second := r.share(true), r.share(false)
	r.do(ShareWindow{Window: r.two.id, Name: "Anna", CanType: true, Relay: r.url})
	r.until("the third", func() bool { return len(r.a.windowLinks()) == 3 && r.a.windowLinks()[2].Stands == LinkWaiting })
	third := r.a.windowLinks()[2]
	if first.Link == second.Link || first.Link == third.Link {
		t.Fatal("two shares have one link")
	}
	a, b, c := r.visit(first.Link), r.visit(second.Link), r.visit(third.Link)
	a.in(first.ID, true)
	b.in(second.ID, false)
	c.in(third.ID, true)
	st := c.state("the other window", func(st guest.State) bool { return len(st.Panes) == 2 })
	if got := paneIDs(st); !slices.Equal(got, []string{"p4", "p5"}) {
		t.Errorf("the third is shown %v", got)
	}
	if strings.Contains(a.everything(), "ELSEWHERE") || strings.Contains(b.everything(), "ELSEWHERE") || strings.Contains(c.everything(), "first") {
		t.Error("a guest was told of a window that is not its own")
	}
	if n := r.a.sharedWindows(); n != 2 {
		t.Errorf("%d windows are shared", n)
	}
	if got := r.a.whatIsOpen(); !slices.Contains(got, "2 shared windows") {
		t.Errorf("exiting says still open are %v", got)
	}
	r.do(StopLink{ID: first.ID})
	if got := a.ended(); got != guest.ReasonStopped {
		t.Errorf("the first is told %q", got)
	}
	r.do(RenamePane{Pane: "p1", Title: "still here"})
	b.state("the second to go on", func(st guest.State) bool {
		return slices.ContainsFunc(st.Panes, func(p guest.Pane) bool { return p.Title == "still here" })
	})
	r.do(StopLinks{})
	if b.ended() != guest.ReasonStopped || c.ended() != guest.ReasonStopped {
		t.Error("stopping them all left one")
	}
}

// A kakel with no web client in it makes no link, and says why, and a
// relay that is not one, or too old, is said in its words.
func TestAWindowThatCannotBeShared(t *testing.T) {
	r := newLinkRig(t)
	r.a.links.client = func() ([]byte, []byte, bool) { return nil, nil, false }
	if !r.a.linkSetup().NoClient {
		t.Error("a build with no web client offers to share")
	}
	if err := r.a.shareWindow(ShareWindow{Window: r.one.id, Relay: r.url}); !errors.Is(err, errNoClient) {
		t.Errorf("sharing without one says %v", err)
	}
	r.a.links.client = webClient(t)
	for _, url := range []string{"", "relay.example.com", "https://relay.example.com/s/x"} {
		if err := r.a.shareWindow(ShareWindow{Window: r.one.id, Relay: url}); err == nil {
			t.Errorf("shared through %q", url)
		}
	}
	if err := r.a.shareWindow(ShareWindow{Window: 99, Relay: r.url}); err == nil {
		t.Error("shared a window there is none of")
	}
	if got := r.a.windowLinks(); len(got) != 0 {
		t.Fatalf("the shares are %+v", got)
	}
	failed := func(url, want string) {
		t.Helper()
		r.do(ShareWindow{Window: r.one.id, Relay: url})
		r.until("it to fail", func() bool { return r.only().Stands == LinkEnded })
		if got := r.only().Trouble; !strings.Contains(got, want) {
			t.Errorf("it says %q, want %q in it", got, want)
		}
		last := r.a.st.Notices[len(r.a.st.Notices)-1]
		if last.Title != couldntShare || !strings.Contains(last.Body, want) || !strings.Contains(r.logged.String(), last.Body) {
			t.Errorf("the notice is %+v", last)
		}
		r.a.clearLinks(r.one.id)
	}
	// A relay from before windows were shared: it knows no such door.
	secret := relay.NewSecret()
	real := relay.NewServer(secret)
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/about") {
			http.NotFound(w, req)
			return
		}
		real.ServeHTTP(w, req)
	}))
	defer old.Close()
	failed(old.URL+"/k/"+secret, "This relay needs a newer kakel to share windows.")
	// One that takes signed web clients alone, as every relay does at
	// first: in its own words.
	strict := httptest.NewServer(relay.NewServer(secret))
	defer strict.Close()
	failed(strict.URL+"/k/"+secret, "released kakel")
}

// A relay that is lost is reached again, the share goes on, and its guest
// comes back.
func TestAShareOutlivesItsRelayBeingLost(t *testing.T) {
	secret := relay.NewSecret()
	s := relay.NewServer(secret)
	s.AllowUnsigned = true
	inner := httptest.NewServer(s)
	defer inner.Close()
	door := newGate(t, inner.Listener.Addr().String())
	r := newLinkRig(t)
	r.url = "http://" + door.ln.Addr().String() + "/k/" + secret
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	door.shut(true)
	r.until("the relay to be lost", func() bool { return r.only().Trouble != "" && r.only().Stands == LinkAway })
	// Changed while nobody is there to hear.
	r.do(ClosePane{Pane: "p2"})
	door.shut(false)
	r.until("the relay to be back", func() bool { return r.only().Trouble == "" })
	if err := v.dial(); err != nil {
		t.Fatal(err)
	}
	st := v.state("the window as it is now", func(st guest.State) bool { return len(st.Panes) == 2 })
	if slices.Contains(paneIDs(st), "p2") {
		t.Errorf("it is shown %v", paneIDs(st))
	}
	r.until("the guest to be back", func() bool { return r.only().Stands == LinkIn })
	var shown int
	for _, n := range r.a.st.Notices {
		if n.Title == "Couldn't reach the relay" {
			shown++
		}
	}
	if shown > 1 {
		t.Errorf("one failure was shown %d times", shown)
	}
	// What was said of it goes as the relay is back.
	last := -1
	for i, n := range r.a.st.Notices {
		if n.Key == "relay:"+l.ID {
			last = i
		}
	}
	if last < 0 || r.a.st.Notices[last].Kind != NoticeWorked {
		t.Error("with the relay back, the user is still told that it is lost")
	}
}

// gate passes connections on to somewhere else, while it is open.
type gate struct {
	ln     net.Listener
	mu     sync.Mutex
	closed bool
	conns  []net.Conn
}

func newGate(t *testing.T, to string) *gate {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	g := &gate{ln: ln}
	t.Cleanup(func() { _ = ln.Close(); g.shut(true) })
	go func() {
		for {
			near, err := ln.Accept()
			if err != nil {
				return
			}
			far, err := net.Dial("tcp", to)
			if err != nil || !g.add(near, far) {
				_ = near.Close()
				if far != nil {
					_ = far.Close()
				}
				continue
			}
			go func() { _, _ = io.Copy(far, near); _ = far.Close() }()
			go func() { _, _ = io.Copy(near, far); _ = near.Close() }()
		}
	}()
	return g
}

// add takes on a connection, when the gate is open.
func (g *gate) add(conns ...net.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.closed {
		g.conns = append(g.conns, conns...)
	}
	return !g.closed
}

// shut closes the gate, and cuts what passes, or opens it.
func (g *gate) shut(on bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = on
	if on {
		for _, c := range g.conns {
			_ = c.Close()
		}
		g.conns = nil
	}
}

// What a window was last shared with is offered for the next, and before
// that the relay remote access uses, or the last one connected through.
func TestWhatTheNextShareIsOffered(t *testing.T) {
	r := newLinkRig(t)
	s := r.a.settings
	if got := r.a.linkSetup(); got.Name == "" || got.Watch || got.Relay != "" {
		t.Errorf("at first it is %+v", got)
	}
	if err := s.PutRelayLast("https://last.example.com/k/x"); err != nil {
		t.Fatal(err)
	}
	if got := r.a.linkSetup().Relay; got != "https://last.example.com/k/x" {
		t.Errorf("the relay is %q", got)
	}
	if err := s.PutRelay(settings.Relay{URL: "https://serving.example.com/k/x", Via: "s1"}); err != nil {
		t.Fatal(err)
	}
	if got := r.a.linkSetup(); got.Relay != "https://serving.example.com/k/x" || got.Via != "s1" {
		t.Errorf("the relay is %q through %q", got.Relay, got.Via)
	}
	r.share(false)
	if got := r.a.linkSetup(); got.Relay != r.url || got.Via != "" || got.Name != "Anna" || !got.Watch {
		t.Errorf("after one it is %+v", got)
	}
	if got := s.Relay(); got.URL != "https://serving.example.com/k/x" {
		t.Errorf("remote access goes through %q now", got.URL)
	}
	if err := r.a.setLinkSetup(SetLinkSetup{Name: "A", Relay: "nonsense"}); err == nil {
		t.Error("a relay that is none was kept")
	}
}

// A guest who asks is told of where the user is, when that is another
// window than the one shared.
func TestAGuestWhoAsksElsewhereIsSaidWhereTheUserIs(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	r.a.front(r.two)
	before := r.one.pings.Calls
	v := r.visit(l.Link)
	v.join(l.ID)
	if r.one.pings.Calls != before+1 {
		t.Error("the window shared does not call for the user")
	}
	last := r.a.st.Notices[len(r.a.st.Notices)-1]
	if last.win != r.two.id || last.On == nil {
		t.Errorf("the window in front is told %+v", last)
	}
	r.do(last.On)
	if r.a.cur != r.one {
		t.Error("its button does not go to the window")
	}
}

// Closing a window says what of its sharing goes with it, and a share
// that ends so is said.
func TestClosingASharedWindowSaysWhatEnds(t *testing.T) {
	for _, c := range []struct {
		name   string
		join   bool
		notice string
	}{{"a guest asks", true, "Bob"}, {"a link waits", false, "link"}} {
		t.Run(c.name, func(t *testing.T) {
			r := newLinkRig(t)
			// One pane: nothing else to ask about.
			r.do(ClosePane{Pane: "p2"})
			r.do(ClosePane{Pane: "p3"})
			l := r.share(true)
			if c.join {
				r.visit(l.Link).join(l.ID)
			}
			r.a.front(r.one)
			r.do(CloseWindow{})
			if r.one.gone || len(r.a.st.Asks) != 1 {
				t.Fatalf("the window is gone: %v, asked %+v", r.one.gone, r.a.st.Asks)
			}
			r.do(AskAnswered{ID: r.a.st.Asks[0].ID, Yes: true})
			r.until("the window to close", func() bool { return r.one.gone })
			if !slices.ContainsFunc(r.a.st.Notices, func(n Notice) bool { return strings.Contains(n.Title, c.notice) && strings.Contains(n.Body, "closed") }) {
				t.Errorf("the notices are %+v", r.a.st.Notices)
			}
		})
	}
}

// joinedNotices are the titles of what was said of terminals joining a
// shared window.
func (r *linkRig) joinedNotices() []string {
	var out []string
	for _, n := range r.a.st.Notices {
		if strings.HasPrefix(n.Key, "joined:") {
			out = append(out, n.Title)
		}
	}
	return out
}

// A terminal that joins a window with a guest in it is said, unless the
// user moved it there themselves.
func TestATerminalThatJoinsASharedWindowIsSaid(t *testing.T) {
	r := newLinkRig(t)
	l := r.share(true)
	r.a.front(r.one)
	r.terminal("p6", "before")
	r.a.publish()
	v := r.visit(l.Link)
	v.in(l.ID, true)
	if got := r.joinedNotices(); len(got) != 0 {
		t.Fatalf("with nobody in it was said %q", got)
	}
	r.terminal("p7", "late")
	r.a.publish()
	r.a.publish()
	if got := r.joinedNotices(); len(got) != 1 || !strings.Contains(got[0], "late") || !strings.Contains(got[0], "Bob") {
		t.Errorf("it was said %q", got)
	}
	// Moved by the user, and one that is no terminal.
	r.do(MovePane{Pane: "p4", Beside: "p1"})
	r.a.addPane(Pane{ID: "p8", Title: "jobs", Kind: KindJobs}, nil, Placement{})
	r.a.publish()
	if got := r.joinedNotices(); len(got) != 1 {
		t.Errorf("it was said %q", got)
	}
	// Moved back and brought by kakel itself.
	r.a.moveToWindow("p4", r.two)
	r.a.publish()
	r.a.moveToWindow("p4", r.one)
	r.a.publish()
	if got := r.joinedNotices(); len(got) != 2 {
		t.Errorf("it was said %q", got)
	}
}

// What is asked for in one window lands there, however late, with the
// user gone to another.
func TestALatePaneLandsWhereItWasAskedFor(t *testing.T) {
	r := newLinkRig(t)
	for _, c := range []struct {
		name string
		at   func() Placement
	}{
		{"asked in the window", func() Placement { return r.a.asked(Placement{}) }},
		{"a group of it", func() Placement { return r.a.asked(Placement{Group: r.a.layout.GroupOf("p4").ID}) }},
		{"a pane gone since", func() Placement { return r.a.asked(Placement{Beside: "gone"}) }},
	} {
		r.a.front(r.two)
		at := c.at()
		r.a.front(r.one)
		focus := r.a.st.Focus
		id := "late-" + c.name
		r.a.addPane(Pane{ID: id, Title: "late"}, nil, at)
		if got := r.a.ownerOf(id); got != r.two {
			t.Errorf("%s: it landed in window %+v", c.name, got)
		}
		if r.a.cur != r.one || r.a.st.Focus != focus || r.a.focusIn(r.two) != id {
			t.Errorf("%s: the user was taken from %q to %q", c.name, focus, r.a.st.Focus)
		}
	}
	// With that window gone, in the one in front.
	r.a.front(r.two)
	at := r.a.asked(Placement{})
	r.a.front(r.one)
	r.two.gone = true
	r.a.addPane(Pane{ID: "orphan", Title: "late"}, nil, at)
	if got := r.a.ownerOf("orphan"); got != r.one {
		t.Errorf("with its window gone it landed in %+v", got)
	}
	r.two.gone = false
}

// The command line of a command pane on another kakel is told to no guest
// of this one's window, nor written in this Window Log: not by one that
// says what its panes are, and not by one too old to.
func TestACommandLineOnAnotherKakelIsNotTold(t *testing.T) {
	for _, older := range []bool{false, true} {
		t.Run(fmt.Sprintf("older=%v", older), func(t *testing.T) {
			far, _ := agentApp(t)
			dir := t.TempDir()
			far.serving.hostKey, far.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
			far.serving.older = older
			for id, p := range map[string]Pane{"cmd": {Title: "mysql -u root -pHUNTER2", Command: true}, "sh": {Title: "PLAIN-SHELL"}} {
				p.ID = id
				far.addPane(p, screen.Open(newSaid(), vt.DefaultPalette(), far.hooks(id)), Placement{})
			}
			far.publish()
			b, keyFile := clientOf(t, far)
			b.links.client = webClient(t)
			r := &linkRig{t: t, a: b, one: b.cur, url: openRelay(t), logged: &bytes.Buffer{}}
			log.SetOutput(&lockedWriter{w: r.logged})
			t.Cleanup(func() { log.SetOutput(os.Stderr) })
			b.handle(ConnectWindow{Addr: far.st.Serving.Addr, KeyFile: keyFile, Only: true})
			pumpBoth(t, far, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
			b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
			pumpBoth(t, far, b, "its panes", func() bool { return len(b.st.Windows) == 1 && len(b.st.Windows[0].Open) >= 2 })
			name := b.st.Windows[0].Name
			b.handle(AttachWindow{Window: name, ID: "cmd"})
			b.handle(AttachWindow{Window: name, ID: "sh"})
			terminals := func() (n int) {
				for _, p := range b.st.Panes {
					if b.terminal(p.ID) != nil {
						n++
					}
				}
				return n
			}
			pumpBoth(t, far, b, "both", func() bool { return terminals() == 2 })
			stop := make(chan struct{})
			var running sync.WaitGroup
			running.Go(func() {
				for {
					select {
					case f := <-far.events:
						f()
					case <-stop:
						return
					}
				}
			})
			defer func() { close(stop); running.Wait() }()
			l := r.share(true)
			v := r.visit(l.Link)
			v.in(l.ID, true)
			v.state("both panes", func(st guest.State) bool { return len(st.Panes) >= 2 })
			if !older {
				// A pane through another kakel may have its title a moment
				// after it is there.
				v.state("the shell's title", func(st guest.State) bool {
					return slices.ContainsFunc(st.Panes, func(p guest.Pane) bool { return p.Title == "PLAIN-SHELL" })
				})
			}
			got := v.everything()
			if strings.Contains(got, "HUNTER2") {
				t.Errorf("the guest is told the command line: %s", got)
			}
			if strings.Contains(got, "PLAIN-SHELL") == older {
				t.Errorf("a shell there is told by its title: %v", !older)
			}
			r.do(StopLinks{})
			b.waitForLinks()
			for _, p := range b.st.Panes {
				b.handle(ClosePane{Pane: p.ID})
			}
			if strings.Contains(r.logged.String(), "HUNTER2") {
				t.Errorf("the Window Log has the command line: %s", r.logged)
			}
		})
	}
}

// A terminal through another kakel is shared as any other, at the size it
// has here, and what the guest types reaches the far program.
func TestATerminalThroughAnotherKakelIsShared(t *testing.T) {
	far, _ := agentApp(t)
	dir := t.TempDir()
	far.serving.hostKey, far.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	b, keyFile := clientOf(t, far)
	b.links.client = webClient(t)
	b.handle(ConnectWindow{Addr: far.st.Serving.Addr, KeyFile: keyFile})
	pumpBoth(t, far, b, "the question about the host key", func() bool { return len(b.st.Asks) > 0 })
	b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
	pumpBoth(t, far, b, "a terminal on it", func() bool { return oneShell(b) })
	pane := b.st.Panes[0]

	// The far kakel runs on a goroutine's time of its own here.
	r := &linkRig{t: t, a: b, one: b.cur, url: openRelay(t), logged: &bytes.Buffer{}}
	stop := make(chan struct{})
	var running sync.WaitGroup
	running.Go(func() {
		for {
			select {
			case f := <-far.events:
				f()
			case <-stop:
				return
			}
		}
	})
	defer func() { close(stop); running.Wait() }()
	l := r.share(true)
	v := r.visit(l.Link)
	v.in(l.ID, true)
	st := v.state("the pane", func(st guest.State) bool { return len(st.Panes) == 1 })
	if p := st.Panes[0]; p.Kind != guest.KindTerminal || p.Icon != "window" {
		t.Errorf("it is %+v", p)
	}
	shown, err := v.open(pane.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(shown, "echo typed-by-the-guest\r"); err != nil {
		t.Fatal(err)
	}
	// Whatever shell that is: what echo printed, or the line as it echoed.
	v.sees("typed-by-the-guest")
	// Another size here is the guest's, as a screen of it.
	b.terminal(pane.ID).Hold(91, 23)
	b.publish()
	st = v.state("its size", func(st guest.State) bool { return len(st.Panes) == 1 && st.Panes[0].Cols == 91 })
	if st.Panes[0].Rows != 23 {
		t.Errorf("it is %+v", st.Panes[0])
	}
	r.do(StopLinks{})
	if got := v.ended(); got != guest.ReasonStopped {
		t.Errorf("it is told %q", got)
	}
	b.waitForLinks()
}

// So does what takes a connection, or another kakel, to open: connected
// to, opened there and attached to, each after the user has gone to
// another window.
func TestWhatAnotherKakelOpensLandsWhereItWasAskedFor(t *testing.T) {
	far, _ := agentApp(t)
	dir := t.TempDir()
	far.serving.hostKey, far.serving.allowed = filepath.Join(dir, "host_key"), filepath.Join(dir, "authorized_keys")
	far.publish()
	b, keyFile := clientOf(t, far)
	shared := b.cur
	private := b.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	b.addPane(Pane{ID: "kept", Title: "kept", Kind: KindFileManager}, nil, Placement{})
	// asked asks for something in the private window, and goes to the
	// other at once.
	asked := func(what string, in any, n int) {
		t.Helper()
		b.front(private)
		b.handle(in)
		b.front(shared)
		pumpBoth(t, far, b, what, func() bool {
			if len(b.st.Asks) > 0 {
				b.handle(AskAnswered{ID: b.st.Asks[0].ID, Yes: true})
				b.front(shared)
			}
			got := 0
			for _, p := range b.st.Panes {
				if b.terminal(p.ID) != nil {
					got++
				}
			}
			return got == n
		})
		for _, p := range b.st.Panes {
			if b.terminal(p.ID) != nil && b.ownerOf(p.ID) != private {
				t.Errorf("%s: %q landed in the other window", what, p.Title)
			}
		}
		if b.cur != shared {
			t.Errorf("%s: it took the user along", what)
			b.front(shared)
		}
	}
	// With no pane of its log to stand beside.
	asked("the terminal it connected for", ConnectWindow{Addr: far.st.Serving.Addr, KeyFile: keyFile, InPlace: true}, 1)
	name := b.st.Windows[0].Name
	asked("a terminal opened there", OpenOn{Machine: name}, 2)
	asked("a command run there", RunCommand{Machine: name, Line: "true"}, 3)
	open := ""
	pumpBoth(t, far, b, "what it has open", func() bool {
		for _, o := range b.st.Windows[0].Open {
			open = o.ID
		}
		return open != ""
	})
	asked("a pane attached to", AttachWindow{Window: name, ID: open}, 4)
}
