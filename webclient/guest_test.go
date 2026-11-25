//go:build !js

package main_test

import (
	"context"
	"errors"
	"fmt"
	"image"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/internal/clientbuild"
	"github.com/marrasen/kakel/internal/webtest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/themes"
	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
	"github.com/marrasen/kakel/webclient/demo"
)

// These run all of it: a relay with the client in it, a host with a
// window of terminals, and a guest's browser, which opens the link.

// relayed is the relay the tests share, once the client is in it.
var relayed struct {
	once   sync.Once
	server *httptest.Server
	url    relay.URL
	bundle string
	err    error
}

// startRelay runs a relay, and gives it the client the tests built.
func startRelay(t *testing.T) {
	t.Helper()
	start()
	switch {
	case errors.Is(shared.err, webtest.ErrNoChrome):
		t.Skipf("the guest's page is not tested here: %v", shared.err)
	case shared.err != nil:
		t.Fatal(shared.err)
	}
	relayed.once.Do(func() {
		relayed.err = func() error {
			files := map[string][]byte{}
			all, err := os.ReadDir(shared.dir)
			if err != nil {
				return err
			}
			for _, f := range all {
				if files[f.Name()], err = os.ReadFile(filepath.Join(shared.dir, f.Name())); err != nil {
					return err
				}
			}
			packed, err := clientbuild.Packed(files)
			if err != nil {
				return err
			}
			zipped, _, err := relay.MakeBundle(packed)
			if err != nil {
				return err
			}
			secret := relay.NewSecret()
			s := relay.NewServer(secret)
			s.AllowUnsigned = true
			relayed.server = httptest.NewServer(s)
			if relayed.url, err = relay.ParseURL(relayed.server.URL + "/k/" + secret); err != nil {
				return err
			}
			relayed.bundle = relay.BundleHash(zipped)
			return relay.Client{Relay: relayed.url}.PutBundle(context.Background(), zipped, "")
		}()
	})
	if relayed.err != nil {
		t.Fatal(relayed.err)
	}
}

// host is a host sharing a window through the relay.
type host struct {
	t    *testing.T
	host *guest.Host
	link relay.ShareLink
	// asks has the guests who ask, and answer what they are told.
	asks   chan guest.Request
	answer chan bool

	mu    sync.Mutex
	conns []net.Conn
	// away says nobody is let through.
	away     bool
	panes    map[string]*uiterm.Terminal
	programs map[string]*demo.Session
	st       guest.State
	last     guest.Status
}

// accepts keeps what a listener accepts, to cut it.
type accepts struct {
	net.Listener
	h *host
}

func (a accepts) Accept() (net.Conn, error) {
	for {
		c, err := a.Listener.Accept()
		if err != nil {
			return nil, err
		}
		a.h.mu.Lock()
		away := a.h.away
		if !away {
			a.h.conns = append(a.h.conns, c)
		}
		a.h.mu.Unlock()
		if !away {
			return c, nil
		}
		_ = c.Close()
	}
}

// newHost shares a window of terminals named by their panes' IDs.
func newHost(t *testing.T, ids ...string) *host {
	t.Helper()
	startRelay(t)
	h := &host{t: t, asks: make(chan guest.Request, 4), answer: make(chan bool, 4),
		panes: map[string]*uiterm.Terminal{}, programs: map[string]*demo.Session{}}
	for _, id := range ids {
		h.add(id, 80, 24)
	}
	var err error
	h.host, err = guest.NewHost(guest.HostConfig{
		Ask: func(ctx context.Context, r guest.Request) bool {
			h.asks <- r
			select {
			case ok := <-h.answer:
				return ok
			case <-ctx.Done():
				return false
			}
		},
		Open: func(id string) (guest.PaneSource, error) {
			h.mu.Lock()
			defer h.mu.Unlock()
			if p, ok := h.panes[id]; ok {
				return p, nil
			}
			return nil, guest.ErrNotShared
		},
		OnChange: func(st guest.Status) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.last = st
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := relay.NewShareID()
	if h.link, err = relay.NewShareLink(relayed.url, id, h.host.Secret().String()); err != nil {
		t.Fatal(err)
	}
	up := make(chan struct{})
	var once sync.Once
	ln, err := relay.ListenShare(relay.ShareConfig{Relay: relayed.url, ID: id, Token: relay.NewToken(), Bundle: relayed.bundle,
		OnState: func(err error) {
			if err == nil {
				once.Do(func() { close(up) })
			}
		}})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = h.host.Serve(accepts{ln, h}) }()
	t.Cleanup(func() {
		h.host.End(guest.ReasonStopped)
		_ = ln.Close()
		for _, p := range h.panes {
			_ = p.Close()
		}
	})
	select {
	case <-up:
	case <-time.After(longest):
		t.Fatal("the relay never had the share")
	}
	h.st = guest.State{HostName: "Anna", Title: "web", CanType: true, Theme: themeOf(t, 0), HostIn: ids[0]}
	h.publish(ids...)
	return h
}

// themeOf is one of kakel's themes as a host sends it.
func themeOf(t *testing.T, i int) []byte {
	t.Helper()
	made, err := look.Of(themes.Built()[i])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := guestlook.Theme{Look: made, FontSize: 15}.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// add starts a program in a pane.
func (h *host) add(id string, cols, rows int) {
	h.t.Helper()
	p := demo.New(func() {})
	pal, err := themes.Built()[0].Palette()
	if err != nil {
		h.t.Fatal(err)
	}
	term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: cols, Rows: rows}, Palette: &pal})
	if err != nil {
		h.t.Fatal(err)
	}
	h.mu.Lock()
	h.panes[id], h.programs[id] = term, p
	h.mu.Unlock()
}

// publish shows the window with ids in one group, or as l has them.
func (h *host) publish(ids ...string) {
	h.t.Helper()
	l := dock.NewLayout(true, ids...)
	h.publishAs(l)
}

func (h *host) publishAs(l dock.Layout) {
	h.t.Helper()
	root := l.Windows[0].Root
	h.mu.Lock()
	defer h.mu.Unlock()
	h.st.Panes = nil
	for _, id := range root.Items() {
		if p, ok := h.panes[id]; ok {
			s := p.Size()
			h.st.Panes = append(h.st.Panes, guest.Pane{ID: id, Title: id, Kind: guest.KindTerminal, Icon: "terminal", Cols: s.Cols, Rows: s.Rows})
		} else {
			h.st.Panes = append(h.st.Panes, guest.Pane{ID: id, Title: "Files", Kind: guest.KindOther, Icon: "filemanager"})
		}
	}
	raw, err := guestlook.Layout(root, func(string) bool { return true })
	if err != nil {
		h.t.Fatal(err)
	}
	h.st.Layout = raw
	if err := h.host.Publish(h.st); err != nil {
		h.t.Fatal(err)
	}
}

// change changes what is published besides the panes.
func (h *host) change(do func(st *guest.State)) {
	h.t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	do(&h.st)
	if err := h.host.Publish(h.st); err != nil {
		h.t.Fatal(err)
	}
}

// cut cuts every connection there has been, and lets no other through
// until back is called.
func (h *host) cut() (back func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.away = true
	for _, c := range h.conns {
		_ = c.Close()
	}
	h.conns = nil
	return func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.away = false
	}
}

func (h *host) status() guest.Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// allow lets the next guest who asks in, and is who that is.
func (h *host) allow(ok bool) guest.Request {
	h.t.Helper()
	select {
	case r := <-h.asks:
		h.answer <- ok
		return r
	case <-time.After(longest):
		h.t.Fatal("nobody asked")
	}
	return guest.Request{}
}

// awaitTyped waits until a pane's program has been sent want in all.
func (h *host) awaitTyped(id, want string) {
	h.t.Helper()
	await(h.t, func() string {
		if got := string(h.programs[id].Sent()); got != want {
			return fmt.Sprintf("%s was typed %q, want %q", id, got, want)
		}
		return ""
	})
}

// before runs in the guest's page ahead of its own code: what its policy
// stops is kept, and what asks to fill the screen.
const watchedGuest = before + `window.stopped = []; addEventListener("securitypolicyviolation", e => stopped.push(e.violatedDirective + " " + e.blockedURI));
window.filled = 0; { const was = Element.prototype.requestFullscreen; Element.prototype.requestFullscreen = function(...a) { filled++; return was.apply(this, a) } }`

// open opens the link in a page of its own.
func (h *host) open(width, height int, scale float64) *webtest.Page {
	h.t.Helper()
	shared.browser.Allow(h.t, relayed.server.URL, "clipboardReadWrite", "clipboardSanitizedWrite")
	p := shared.browser.Open(h.t, h.link.String(), width, height, scale, watchedGuest)
	h.t.Cleanup(func() {
		h.t.Helper()
		if h.t.Failed() {
			var st any
			_ = p.TryEval(`kakelGuest.state()`, &st)
			h.t.Logf("the page's state: %v, its console: %q", st, p.Console())
		}
		p.Close()
	})
	return p
}

// sound checks that the page had no errors, that its policy stopped
// nothing, and that the link's secret is nowhere to be seen.
func (h *host) sound(p *webtest.Page, gone bool) {
	h.t.Helper()
	secret := h.link.Secret
	var stopped []string
	p.Eval(`stopped`, &stopped)
	if bad := p.Errors(); len(bad)+len(stopped) > 0 {
		h.t.Errorf("its errors: %q, and what its policy stopped: %q", bad, stopped)
	}
	var said, asked, at string
	p.Eval(`document.title + document.documentElement.outerHTML`, &said)
	p.Eval(`performance.getEntriesByType("resource").map(e => e.name).join(" ")`, &asked)
	p.Eval(`location.href`, &at)
	where := append(p.Console(), said, asked)
	if gone {
		where = append(where, at)
	}
	for _, w := range where {
		if strings.Contains(w, secret) {
			h.t.Errorf("the link's secret is in %q", w)
		}
	}
}

// on waits for a page of the guest's, by its number.
func on(p *webtest.Page, what string, page int) {
	p.Await(what, fmt.Sprintf(`window.kakelGuest && kakelGuest.state().page === %d`, page), longest)
}

// steady waits until nothing has moved on the canvas for a moment.
func steady() { time.Sleep(700 * time.Millisecond) }

// join joins as name, and is let in.
func (h *host) join(p *webtest.Page, name string) {
	h.t.Helper()
	on(p, "the join page", 0)
	steady()
	p.Type(name + "\r")
	h.allow(true)
	on(p, "the window", 2)
}

// shows waits until a pane's screen holds want.
func shows(p *webtest.Page, id, want string) {
	p.Await(fmt.Sprintf("%q in %s", want, id), fmt.Sprintf(`(kakelGuest.screen(%q) || "").includes(%q)`, id, want), longest)
}

// typable waits until a terminal has the keyboard.
func typable(p *webtest.Page) {
	p.Await("the keyboard in a terminal", `document.activeElement.tagName === "TEXTAREA"`, longest)
}

func reload(p *webtest.Page) { p.Call("Page.reload", map[string]any{}, nil) }

// split is a window of p1 with a tab p2, beside p3 over a pane that is
// not shared.
func split() dock.Layout {
	l := dock.NewLayout(true, "p1", "p2")
	l.Add("p3", dock.Target{At: l.GroupOf("p1").ID, Side: dock.Right})
	l.Add("files", dock.Target{At: "p3", Side: dock.Bottom})
	l.Show("p1")
	l.SetShare(l.Windows[0].Root.ID, 0.6)
	return l
}

// placeOfPart is where the part of the page a screen reader calls name is.
func placeOfPart(t *testing.T, p *webtest.Page, name string) (at []float64) {
	t.Helper()
	await(t, func() string {
		if err := p.TryEval(fmt.Sprintf(`kakelGuest.at(%q)`, name), &at); err != nil || len(at) != 2 {
			return fmt.Sprintf("nothing is called %q (%v)", name, err)
		}
		return ""
	})
	return at
}

// press clicks the part of the page a screen reader calls name.
func press(t *testing.T, p *webtest.Page, name string) {
	t.Helper()
	if at := placeOfPart(t, p, name); len(at) == 2 {
		p.Click(at[0], at[1], "left", 1)
	}
}

// A guest opens the link, gives a name, is let in, sees what the programs
// draw and types in them; the host changes the window meanwhile, the
// connection is lost and the page reloaded, until the host stops.
func TestAGuestInAWindow(t *testing.T) {
	h := newHost(t, "p1", "p2", "p3")
	p := h.open(1200, 800, 1)
	on(p, "the join page", 0)
	p.Await("the page faded in", `getComputedStyle(document.querySelector("#gunim")).opacity == 1`, longest)
	keep(t, p, "guest-join")
	// Nothing has reached the host before Join.
	if st := h.status(); st.Phase != guest.PhaseJoin || st.Guest.Name != "" {
		t.Fatalf("before Join the share is at %+v", st)
	}
	p.Type("Bo\r")
	on(p, "the wait", 1)
	steady()
	keep(t, p, "guest-waiting")
	// Reloaded while it waits, it waits on, and is not asked about twice.
	reload(p)
	on(p, "the wait again", 1)
	p.Await("the connection", `!kakelGuest.state().lost`, longest)
	if r := h.allow(true); r.Name != "Bo" || !strings.Contains(r.About, "Chrome on ") {
		t.Errorf("it asked as %q, with %q", r.Name, r.About)
	}
	on(p, "the window", 2)
	shows(p, "p1", "~$")
	typable(p)
	p.Type("hello")
	h.awaitTyped("p1", "hello")
	shows(p, "p1", "~$ hello")
	await(t, func() string {
		if st := h.status(); st.Focus != "p1" || st.TypedIn != "p1" {
			return fmt.Sprintf("the guest is in %q and typed in %q", st.Focus, st.TypedIn)
		}
		return ""
	})
	var title string
	p.Eval(`document.title`, &title)
	if title != "web — kakel" {
		t.Errorf("the tab is titled %q", title)
	}
	h.sound(p, true)

	// The host's layout, and a pane opened in it.
	h.publishAs(split())
	shows(p, "p3", "~$")
	steady()
	keep(t, p, "guest-split")

	// Only watching.
	h.change(func(st *guest.State) { st.CanType = false })
	p.Await("watching", `kakelGuest.state().canType === false`, longest)
	p.Type("xyz")
	keep(t, p, "guest-watch")
	h.change(func(st *guest.State) { st.CanType = true })
	p.Await("typing again", `kakelGuest.state().canType === true`, longest)
	// What comes right after keys that went to nobody goes to nobody too,
	// and a slow machine hears those late.
	time.Sleep(3 * time.Second)
	p.Type("!")
	h.awaitTyped("p1", "hello!")

	// A paste of two lines is asked about.
	p.Eval(`navigator.clipboard.writeText("a\nb")`, nil)
	p.Press(webtest.Key{Name: "V", Code: "KeyV", Number: 'V', Mods: webtest.Ctrl | webtest.Shift})
	placeOfPart(t, p, "Text to paste")
	steady()
	keep(t, p, "guest-paste")
	h.awaitTyped("p1", "hello!")
	p.Type("\r")
	h.awaitTyped("p1", "hello!a\rb")

	// Full screen is asked of the browser.
	press(t, p, "Full Screen")
	p.Await("a page that fills the screen", `filled === 1`, longest)
	p.Eval(`document.exitFullscreen && document.fullscreenElement ? document.exitFullscreen() : 0`, nil)
	p.Await("the page as it was", `!document.fullscreenElement`, longest)

	// The connection is lost, and made again by itself.
	back := h.cut()
	p.Await("a veil", `kakelGuest.state().lost`, longest)
	keep(t, p, "guest-lost")
	back()
	p.Await("the connection back", `!kakelGuest.state().lost && kakelGuest.state().page === 2`, longest)
	h.programs["p1"].Print("after the cut")
	shows(p, "p1", "after the cut")
	p.Type("z")
	h.awaitTyped("p1", "hello!a\rbz")

	// Every size, with the host's themes.
	for _, c := range []struct {
		width, height int
		scale         float64
		theme         int
	}{{800, 600, 1, 1}, {390, 844, 2, 2}, {1200, 800, 1, 0}} {
		h.change(func(st *guest.State) { st.Theme = themeOf(t, c.theme) })
		p.Size(c.width, c.height, c.scale)
		p.Await("the theme", fmt.Sprintf(`kakelGuest.state().theme === %q`, themes.Built()[c.theme].Name), longest)
		if os.Getenv("KAKEL_SHOTS") != "" {
			steady()
			steady()
			steady()
			keep(t, p, fmt.Sprintf("guest-%dx%d", c.width, c.height))
		}
	}
	h.sound(p, true)

	// A finger: a tap on the terminal brings up a phone's keyboard, and a
	// row of the keys it has not. Both halves of a tap come at once, as a
	// machine slow to hand them over makes it a finger held down.
	p.Await("the canvas at its size", `document.querySelector("#gunim").width === 1200`, longest)
	steady()
	tap := func(x, y float64) {
		p.Eval(fmt.Sprintf(`{ const c = document.querySelector("#gunim"), e = (kind, held) => c.dispatchEvent(new PointerEvent(kind, {pointerId: 9, pointerType: "touch", isPrimary: true, clientX: %v, clientY: %v, button: 0, buttons: held, bubbles: true, cancelable: true})); e("pointerdown", 1); e("pointerup", 0); 0 }`, x, y), nil)
	}
	p.Eval(`document.activeElement.blur(), 0`, nil)
	tap(300, 500)
	p.Await("a field for a phone's keyboard", `document.activeElement.tagName === "TEXTAREA"`, longest)
	placeOfPart(t, p, "Esc")
	steady()
	steady()
	if at := placeOfPart(t, p, "Esc"); len(at) == 2 {
		tap(at[0], at[1])
	}
	h.awaitTyped("p1", "hello!a\rbz\x1b")

	// Somebody else with the link is told it is used, and the guest stays.
	other := h.open(800, 600, 1)
	on(other, "the other's join page", 0)
	other.Type("Eve\r")
	on(other, "the other's end", 3)
	steady()
	keep(t, other, "guest-used")
	h.sound(other, false)
	other.Close()

	// A reload comes back, with nothing in the address to come back with.
	// It is asked about first, as closing the page is.
	reload(p)
	p.Await("the window, reloaded", `window.kakelGuest && kakelGuest.state().page === 2 && !kakelGuest.state().lost`, longest)
	if p.Asked() != 1 {
		t.Errorf("the browser asked %d times before the page was reloaded", p.Asked())
	}
	shows(p, "p1", "after the cut")
	typable(p)
	p.Type("w")
	h.awaitTyped("p1", "hello!a\rbz\x1bw")
	if r := h.status().Guest; r.Name != "Bo" {
		t.Errorf("it came back as %q", r.Name)
	}
	h.sound(p, true)

	// The same tab opened twice: the window is in the one opened last,
	// and back in the first when asked for there.
	p.Eval(`window.twin = window.open(location.href), 0`, nil)
	on(p, "the first tab's end", 3)
	press(t, p, "Use Here")
	p.Await("the window here", `kakelGuest.state().page === 2 && !kakelGuest.state().lost`, longest)
	p.Eval(`twin.close(), 0`, nil)

	// The host stops, and nothing is kept.
	h.host.End(guest.ReasonStopped)
	on(p, "the end", 3)
	steady()
	keep(t, p, "guest-stopped")
	var stored string
	p.Eval(`JSON.stringify(Object.values(sessionStorage))`, &stored)
	if strings.Contains(stored, "key") || strings.Contains(stored, "secret") {
		t.Errorf("the tab still keeps %s", stored)
	}
	var over, again string
	p.Eval(`kakelGuest.state().over`, &over)
	// Reloaded, it is told from a link that was cut short.
	reload(p)
	on(p, "the end, reloaded", 3)
	steady()
	keep(t, p, "guest-reloaded-after")
	p.Eval(`kakelGuest.state().over`, &again)
	if again == "" || strings.Contains(again, "incomplete") {
		t.Errorf("having said %q, after a reload it says %q", over, again)
	}
	h.sound(p, true)
}

// A phone's keyboard composes word by word: the word shows where the
// cursor is as it grows, the program has it once it is done, and the
// keyboard's own delete, which names no key, is a Backspace.
func TestAGuestComposes(t *testing.T) {
	h := newHost(t, "p1")
	p := h.open(800, 500, 1)
	h.join(p, "Bo")
	shows(p, "p1", "~$")
	typable(p)
	steady()
	// Still, but for the cursor, which blinks.
	const cursor = 30
	before := p.Shot()
	await(t, func() string {
		was := before
		before = p.Shot()
		if at := changed(was, before); at.Dx() >= cursor {
			return fmt.Sprintf("the page still moves in %v", at)
		}
		return ""
	})
	compose := func(text string) {
		n := len([]rune(text))
		p.Call("Input.imeSetComposition", map[string]any{"text": text, "selectionStart": n, "selectionEnd": n}, nil)
	}
	for _, text := range []string{"h", "he", "hel", "hello"} {
		compose(text)
	}
	await(t, func() string {
		if at := changed(before, p.Shot()); at.Dx() < cursor {
			return fmt.Sprintf("what is composed shows in %v", at)
		}
		return ""
	})
	keep(t, p, "guest-composing")
	h.awaitTyped("p1", "")
	p.Call("Input.insertText", map[string]any{"text": "hello"}, nil)
	h.awaitTyped("p1", "hello")
	p.Type(" ")
	compose("wo")
	compose("word")
	p.Call("Input.insertText", map[string]any{"text": "word"}, nil)
	h.awaitTyped("p1", "hello word")
	shows(p, "p1", "~$ hello word")
	// One given up.
	compose("no")
	compose("")
	p.Eval(`document.activeElement.dispatchEvent(new InputEvent("beforeinput", {inputType: "deleteContentBackward", bubbles: true, cancelable: true})), 0`, nil)
	h.awaitTyped("p1", "hello word\x7f")
	p.Press(webtest.Key{Name: "Backspace", Code: "Backspace", Number: 8})
	h.awaitTyped("p1", "hello word\x7f\x7f")
	var off string
	p.Eval(`document.activeElement.getAttribute("autocorrect")`, &off)
	if off != "off" {
		t.Errorf("a phone's keyboard corrects what is typed: %q", off)
	}
	h.sound(p, true)
}

// changed is the part of two pictures that differs.
func changed(a, b image.Image) (at image.Rectangle) {
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if a.At(x, y) != b.At(x, y) {
				at = at.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return at
}

// Links that let nobody in, each with its page, light and dark.
func TestLinksThatLetNobodyIn(t *testing.T) {
	h := newHost(t, "p1")
	scheme := func(p *webtest.Page, name string) {
		p.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": name}}}, nil)
	}
	// Cut short, and whole again once pasted whole.
	p := shared.browser.Open(t, strings.TrimSuffix(h.link.String(), h.link.Secret[10:]), 390, 844, 2, watchedGuest)
	defer p.Close()
	on(p, "a link cut short", 3)
	steady()
	keep(t, p, "guest-broken")
	scheme(p, "dark")
	p.Eval(`location.hash = `+strconv.Quote(h.link.Secret), nil)
	p.Await("the join page, once it is whole", `window.kakelGuest && kakelGuest.state().page === 0`, longest)
	steady()
	keep(t, p, "guest-join-dark")
	p.Type("Bo\r")
	h.allow(false)
	on(p, "refused", 3)
	steady()
	keep(t, p, "guest-refused")
	h.sound(p, true)

	// Another host's: what answers is not the one the link is for.
	one, two := newHost(t, "p1"), newHost(t, "p1")
	l, err := relay.NewShareLink(relayed.url, one.link.ID, two.link.Secret)
	if err != nil {
		t.Fatal(err)
	}
	one.link = l
	p = one.open(800, 600, 1)
	on(p, "the join page", 0)
	p.Type("Bo\r")
	on(p, "not verified", 3)
	var over string
	p.Eval(`kakelGuest.state().over`, &over)
	if !strings.Contains(over, "verified") {
		t.Errorf("it says %q", over)
	}
	select {
	case r := <-one.asks:
		t.Errorf("%q asked a host that was not the link's", r.Name)
	default:
	}
}

// A flood of output comes to its end through the relay, with the page
// drawing and the keys heard on the way. KAKEL_FLOOD is how many
// megabytes of it there are, in place of a quarter of one.
func TestAFloodThroughTheRelay(t *testing.T) {
	mb, err := strconv.ParseFloat(os.Getenv("KAKEL_FLOOD"), 64)
	if err != nil || mb <= 0 {
		mb = 0.25
	}
	h := newHost(t, "p1", "p2")
	p := h.open(1000, 700, 1)
	h.join(p, "Bo")
	shows(p, "p1", "~$")
	l := dock.NewLayout(true, "p1")
	l.Add("p2", dock.Target{At: l.GroupOf("p1").ID, Side: dock.Right})
	h.publishAs(l)
	shows(p, "p2", "~$")
	steady()
	// Where what is typed goes.
	typable(p)
	press(t, p, "p2")
	await(t, func() string {
		if at := h.status().Focus; at != "p2" {
			return fmt.Sprintf("the guest is in %q", at)
		}
		return ""
	})
	p.Eval(gaps+`0`, nil)
	began := time.Now()
	<-h.programs["p1"].Burst(int(mb * (1 << 20)))
	hosted := time.Since(began)
	var lags []time.Duration
	typed := ""
	for range 4 {
		typed += "k"
		at := time.Now()
		p.Type("k")
		h.awaitTyped("p2", typed)
		lags = append(lags, time.Since(at))
	}
	flooding := ""
	if err := p.TryEval(`(kakelGuest.screen("p1") || "").includes("`+demo.Done+`") ? "over" : "going on"`, &flooding); err != nil {
		t.Fatal(err)
	}
	shows(p, "p1", demo.Done)
	took := time.Since(began)
	var frames []float64
	p.Eval(`(() => { const all = frameGaps; window.frameGaps = null; return all })()`, &frames)
	slices.Sort(frames)
	slices.Sort(lags)
	if len(frames) > 0 {
		t.Logf("%v MB: the host was done after %v, and the page after %v; %d frames, %.0f ms apart in the middle, %.0f ms at the 99th, %.0f ms at most; a key reached the host in %v in the middle, %v at most, the flood %s by then",
			mb, hosted, took, len(frames), frames[len(frames)/2], frames[len(frames)*99/100], frames[len(frames)-1], lags[len(lags)/2], lags[len(lags)-1], flooding)
	}
	for _, line := range p.Console() {
		if strings.Contains(line, "gunim ") {
			t.Log(line)
		}
	}
	keep(t, p, "guest-flood")
	h.sound(p, true)
}
