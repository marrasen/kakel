package app

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/internal/clientbuild"
	"github.com/marrasen/kakel/internal/webtest"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/webclient/demo"
)

// The pages of the guest's program, by the numbers it has for them.
const (
	webJoin = iota
	webWaiting
	webWindow
	webOver
)

// webRig is a rig with the real web client to share with, and a browser.
type webRig struct {
	*linkRig
	browser *webtest.Browser
	// echoes are the panes' programs that show what is typed, by pane.
	echoes map[string]*demo.Session
}

func newWebRig(t *testing.T) *webRig {
	t.Helper()
	if _, err := webtest.FindChrome(); errors.Is(err, webtest.ErrNoChrome) {
		t.Skipf("a guest in a browser is not tested here: %v", err)
	}
	files, err := clientbuild.Build(clientbuild.Demo)
	if err != nil {
		t.Fatal(err)
	}
	packed, err := clientbuild.Packed(files)
	if err != nil {
		t.Fatal(err)
	}
	zipped, _, err := relay.MakeBundle(packed)
	if err != nil {
		t.Fatal(err)
	}
	r := &webRig{linkRig: newLinkRig(t), echoes: map[string]*demo.Session{}}
	r.a.links.client = func() ([]byte, []byte, bool) { return zipped, nil, true }
	if r.browser, err = webtest.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.browser.Close)
	return r
}

// echo opens a terminal with a program that shows what is typed.
func (r *webRig) echo(id string) {
	prog := demo.New(func() {})
	r.echoes[id] = prog
	r.a.addPane(Pane{ID: id, Title: id}, screen.Open(prog, r.a.palette, r.a.hooks(id)), Placement{})
}

// until is linkRig's, for as long as a browser may take on a machine with
// more to do.
func (r *webRig) until(what string, why func() string) {
	r.t.Helper()
	for end := time.Now().Add(time.Minute); ; {
		last := why()
		if last == "" {
			return
		}
		if time.Now().After(end) {
			r.t.Fatalf("waited for %s: %s", what, last)
		}
		stop := stuckAfter(what, 2*time.Minute)
		select {
		case f := <-r.a.events:
			f()
			r.a.publish()
		case <-time.After(20 * time.Millisecond):
		}
		stop()
	}
}

// open opens the link in a page of its own.
func (r *webRig) open(link string) *webtest.Page {
	r.t.Helper()
	p := r.browser.Open(r.t, link, 640, 400, 1, "")
	r.t.Cleanup(func() {
		if r.t.Failed() {
			r.t.Logf("the page's console: %q", p.Console())
		}
		p.Close()
	})
	return p
}

// page waits until JavaScript js is true in p, with the host going on.
func (r *webRig) page(p *webtest.Page, what, js string) {
	r.t.Helper()
	r.until(what, func() string {
		var ok bool
		if err := p.TryEval("!!("+js+")", &ok); err != nil {
			return err.Error()
		}
		if !ok {
			return fmt.Sprintf("it is not so, and the page's errors are %q", p.Errors())
		}
		return ""
	})
}

// on waits for a page of the guest's program.
func (r *webRig) on(p *webtest.Page, page int) {
	r.t.Helper()
	r.page(p, fmt.Sprintf("the guest's page %d", page), fmt.Sprintf(`window.kakelGuest && kakelGuest.state().page === %d`, page))
}

// shows waits until pane id shows text in the browser.
func (r *webRig) shows(p *webtest.Page, id, text string) {
	r.t.Helper()
	r.page(p, fmt.Sprintf("%q in %s", text, id), fmt.Sprintf(`(kakelGuest.screen(%q) || "").includes(%q)`, id, text))
}

// was waits until a pane's program has been sent want in all.
func (r *webRig) was(id, want string) {
	r.t.Helper()
	r.until("what was typed in "+id, func() string {
		if got := string(r.echoes[id].Sent()); got != want {
			return fmt.Sprintf("it is %q, and not %q", got, want)
		}
		return ""
	})
}

// A window shared from here, and a guest in it with a web browser: the
// relay, the host's half and the client, all of them the real ones.
func TestAGuestInABrowser(t *testing.T) {
	r := newWebRig(t)
	r.echo("p6")
	r.echo("p7")
	r.do(FocusPane{Pane: "p6"})
	l := r.share(true)
	p := r.open(l.Link)
	r.on(p, webJoin)
	// The name's field has what is typed once the page has come in.
	r.page(p, "the keyboard in the name", `document.activeElement.tagName === "TEXTAREA"`)
	p.Type("Bob\r")
	r.on(p, webWaiting)
	if got := r.only(); got.Stands != LinkAsking || got.Guest != "Bob" || !strings.Contains(got.About, "Chrome") {
		t.Fatalf("with the guest asking, the share reads %+v", got)
	}
	r.do(AnswerGuest{ID: l.ID, Allow: true, CanType: true})
	r.on(p, webWindow)

	// Both type, and both see it.
	r.shows(p, "p6", "~$")
	r.page(p, "the keyboard in a terminal", `document.activeElement.tagName === "TEXTAREA"`)
	p.Type("from the guest")
	r.was("p6", "from the guest")
	r.shows(p, "p6", "from the guest")
	for _, c := range " and the host" {
		if _, err := r.a.terminal("p6").HandleKey(input.Event{Kind: input.Text, Rune: c, NormalText: true}); err != nil {
			t.Fatal(err)
		}
	}
	r.was("p6", "from the guest and the host")
	r.shows(p, "p6", "from the guest and the host")
	r.until("where the guest types", func() string {
		if got := r.only(); got.Stands != LinkIn || got.In != "p6" || got.TypedIn != "p6" {
			return fmt.Sprintf("the share reads %+v", got)
		}
		return ""
	})

	// What the host does with its panes.
	r.do(MovePane{Pane: "p7", Beside: "p6"})
	r.page(p, "the split", `["p6", "p7"].every(id => kakelGuest.state().shown.includes(id))`)
	r.shows(p, "p7", "~$")
	r.a.moveToWindow("p7", r.two)
	r.a.publish()
	r.echoes["p7"].Print("ELSEWHERE-NOW")
	r.page(p, "the pane that left", `!kakelGuest.state().panes.includes("p7") && kakelGuest.screen("p7") === null`)
	var tells string
	p.Eval(`JSON.stringify(kakelGuest.state())`, &tells)
	if strings.Contains(tells, "p4") || strings.Contains(tells, "p5") {
		t.Errorf("the guest is told of another window: %s", tells)
	}

	// Only to watch, and to type again.
	r.do(SetLinkMay{ID: l.ID})
	r.page(p, "watching", `kakelGuest.state().canType === false`)
	p.Type("xyz")
	r.do(SetLinkMay{ID: l.ID, CanType: true})
	r.page(p, "typing", `kakelGuest.state().canType === true`)
	// What comes right after keys that went to nobody goes to nobody too,
	// and a slow machine hears those late.
	pause := time.Now().Add(3 * time.Second)
	r.until("a pause", func() string {
		if time.Now().Before(pause) {
			return "it is not over"
		}
		return ""
	})
	p.Type("!")
	r.was("p6", "from the guest and the host!")

	// The link is used.
	other := r.open(l.Link)
	r.on(other, webJoin)
	other.Type("Eve\r")
	r.on(other, webOver)
	if got := r.only(); got.Stands != LinkIn || got.Guest != "Bob" {
		t.Errorf("with the link used again, the share reads %+v", got)
	}
	r.page(p, "the guest to stay", `kakelGuest.state().page === 2 && !kakelGuest.state().lost`)

	r.do(StopLink{l.ID})
	r.on(p, webOver)
	var over string
	p.Eval(`kakelGuest.state().over`, &over)
	if over == "" {
		t.Error("the guest is not told how it ended")
	}
	for _, page := range []*webtest.Page{p, other} {
		if bad := page.Errors(); len(bad) > 0 {
			t.Errorf("a page's errors: %q", bad)
		}
		if said := page.Console(); slices.ContainsFunc(said, func(s string) bool { return strings.Contains(s, strings.SplitN(l.Link, "#", 2)[1]) }) {
			t.Errorf("the link's secret is on the console: %q", said)
		}
	}
}
