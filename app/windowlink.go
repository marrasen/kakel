package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/settings"
	uiterm "github.com/marrasen/kakel/ui/term"
	"github.com/marrasen/kakel/webbundle"
	"github.com/marrasen/kakel/words"
)

// Sharing a window with a link: somebody with a web browser and no kakel
// opens the link, asks to come in, and works in the terminals of that
// one window. The link goes through a relay, which serves the web page,
// and package guest is what the two ends say to each other.
//
// What a guest is shown follows the layout, which says what window a
// pane stands in. tellLinks tells the shares of it each time the program
// publishes its state, before any window shows it, and as a theme is
// picked, before the terminals have its colours. Only terminals are
// shown. Of any other pane the guest is told its kind, and not its
// title, which may be a path, a server or a secret's name.

// WindowLink is one share of a window, as the windows show it.
type WindowLink struct {
	ID string
	// Window is the window shared.
	Window int
	// Link is what the guest opens, while nobody has and the relay has
	// been reached.
	Link string
	// CanType says the guest may type, or may as it is let in.
	CanType bool
	Stands  LinkStands
	// Trouble is why the relay is not reached, while it is not.
	Trouble string
	// Over is how the share ended, in a few words.
	Over string
	// Until is when what it stands at ends by itself, if it does: the
	// link's expiry, the end of asking, or of waiting for a guest away.
	Until time.Time
	// Guest is what the guest calls itself, From the address it came
	// from and About the browser it says it has.
	Guest, From, About string
	// Since is when the guest was let in.
	Since time.Time
	// In is the pane the guest is in, TypedIn the one it last typed in,
	// and TypedAt when.
	In, TypedIn string
	TypedAt     time.Time
	// Relay is the relay's machine.
	Relay string
}

// Live reports whether the share goes on.
func (l WindowLink) Live() bool { return l.Stands != LinkEnded }

// GuestIn reports whether a guest has been let in, here or away.
func (l WindowLink) GuestIn() bool { return l.Stands == LinkIn || l.Stands == LinkAway }

// LinkStands is how far a share has come.
type LinkStands uint8

// How far a share has come.
const (
	// LinkReaching is a relay being asked what it can do.
	LinkReaching LinkStands = iota
	// LinkSending is the web client on its way to the relay.
	LinkSending
	// LinkWaiting is a link nobody has opened.
	LinkWaiting
	// LinkAsking is a guest the host is asked about.
	LinkAsking
	// LinkIn is a guest who is in the window, and LinkAway one who was,
	// and may come back.
	LinkIn
	LinkAway
	// LinkEnded is the end of it.
	LinkEnded
)

// LinkSetup is what a window is shared with, as the dialog offers it.
type LinkSetup struct {
	// Name is what the guest is told the host is called.
	Name string
	// Watch says the guest only watches.
	Watch bool
	// Relay is the relay's URL with its secret, and Via the saved server
	// it is reached through, empty for this machine.
	Relay string
	Via   machines.ID
	// NoClient says this build has no web client to share with.
	NoClient bool
	// Tries counts the times a window was asked to be shared, which
	// worked or did not, for the dialog that asked to hear how it went.
	Tries uint64
}

// Intents for sharing a window with a link.
type (
	// ShareWindow makes a link to Window, and keeps what it was made
	// with for the next.
	ShareWindow struct {
		Window  int
		Name    string
		CanType bool
		Relay   string
		Via     machines.ID
	}
	// StopLink ends one share, and StopLinks every share of a window, or
	// of every window for 0.
	StopLink  struct{ ID string }
	StopLinks struct{ Window int }
	// CopyLink copies a link nobody has opened yet.
	CopyLink struct{ ID string }
	// SetLinkMay sets whether a guest may type.
	SetLinkMay struct {
		ID      string
		CanType bool
	}
	// AnswerGuest lets a guest who asks in, or refuses it.
	AnswerGuest struct {
		ID             string
		Allow, CanType bool
	}
	// GuestUnseen says a guest asks where no kakel window has the
	// keyboard, for the user to be told outside them.
	GuestUnseen struct{ ID string }
	// ClearLinks takes away the shares of a window that have ended.
	ClearLinks struct{ Window int }
	// SetLinkSetup sets what windows are shared with from now on.
	SetLinkSetup struct {
		Name  string
		Watch bool
		Relay string
		Via   machines.ID
	}
)

// couldntShare heads what is said when a window could not be shared.
const couldntShare = "Couldn't share the window"

// Why a window is not shared.
var (
	errNoClient = errors.New("this build of kakel has no web client")
	errOldRelay = errors.New("this relay needs a newer kakel to share windows")
)

// mostEnded is how many shares that have ended are listed for a window.
const mostEnded = 4

// linkShares are the program's shares of its windows.
type linkShares struct {
	all   []*windowLink
	next  int
	tries uint64
	// ending are the shares being ended, for kakel to wait for a moment
	// as it exits.
	ending sync.WaitGroup
	// byHand is the pane the user is moving now, of which nothing needs
	// to be said as it joins a shared window.
	byHand string
	// name is the user's name, once found.
	name string
	// client, when set, stands in for the web client of this build, and
	// times for how long a share waits, for a test.
	client func() (zipped, sig []byte, ok bool)
	times  guest.HostConfig
}

// bundle is the web client to share with, and its signature.
func (l *linkShares) bundle() (zipped, sig []byte, ok bool) {
	if l.client != nil {
		return l.client()
	}
	return webbundle.Bundle()
}

// hasClient reports whether there is a web client to share with.
func (l *linkShares) hasClient() bool {
	if l.client != nil {
		_, _, ok := l.client()
		return ok
	}
	return webbundle.Built()
}

// windowLink is one share of a window. All but panes is the program's
// goroutine's.
type windowLink struct {
	id       string
	win      int
	hostName string
	canType  bool
	client   relay.Client
	// ctx ends with the share.
	ctx  context.Context
	stop context.CancelFunc
	// host and ln are the share and its place on the relay, once the
	// relay has the web client, and link what the guest opens.
	host *guest.Host
	ln   *relay.ShareListener
	link relay.ShareLink
	// reached says the relay has the share now, and was that it has had
	// it; troubled that a failure to reach it has been shown since.
	reached, was, troubled bool
	trouble                string
	stands                 LinkStands
	over                   string
	status                 guest.Status
	since                  time.Time
	// answer takes the answer about a guest who asks, nil with none.
	answer chan bool
	// told is the state last published, and unsaid the failure to last
	// shown.
	told   guest.State
	tells  int
	unsaid string
	// seen are the terminals in told, by pane, as of the last time those
	// that joined were said.
	seen map[string]bool

	// mu guards panes, the terminals the guest may be shown, by pane,
	// which the share's goroutines read.
	mu    sync.Mutex
	panes map[string]guest.PaneSource
}

// open is the pane a guest asks to be shown. It runs on a goroutine of
// the share's.
func (l *windowLink) open(id string) (guest.PaneSource, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if p := l.panes[id]; p != nil {
		return p, nil
	}
	return nil, guest.ErrNotShared
}

// setPanes says which terminals the guest may be shown from now on.
func (l *windowLink) setPanes(panes map[string]guest.PaneSource) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.panes = panes
}

// linkByID is the share with id, or nil.
func (a *app) linkByID(id string) *windowLink {
	for _, l := range a.links.all {
		if l.id == id {
			return l
		}
	}
	return nil
}

// shareWindow makes a link to a window. The relay is reached, and given
// the web client if it needs it, on a goroutine of its own.
func (a *app) shareWindow(in ShareWindow) error {
	a.links.tries++
	w := a.winByID(in.Window)
	if w == nil || w.gone || a.gone {
		return errors.New("the window is closed")
	}
	if !a.links.hasClient() {
		return errNoClient
	}
	url, err := relay.ParseURL(in.Relay)
	if err != nil {
		return err
	}
	name := oneLine(in.Name)
	if name == "" {
		name = a.ownName()
	}
	if a.settings != nil {
		a.keep("what windows are shared with", a.settings.PutShareLink(settings.ShareLink{
			Name: name, Watch: !in.CanType, Relay: url.String(), Via: string(in.Via)}))
	}
	a.links.next++
	l := &windowLink{
		id: "l" + itoa(a.links.next), win: w.id, hostName: name, canType: in.CanType,
		client: relay.Client{Relay: url, Dialer: a.relayDialer(in.Via, &throughRefused{again: "share the window again"})},
	}
	l.ctx, l.stop = context.WithCancel(a.ctx)
	a.links.all = append(a.links.all, l)
	log.Printf("sharing a window with a link through the relay %s", url.Host())
	go a.prepareLink(l)
	return nil
}

// prepareLink finds out whether the relay can share windows, and gives it
// the web client when it does not have it. It runs on a goroutine of its
// own.
func (a *app) prepareLink(l *windowLink) {
	zipped, sig, ok := a.links.bundle()
	if !ok {
		post(a, func() { a.linkFailed(l, errNoClient) })
		return
	}
	hash := relay.BundleHash(zipped)
	err := func() error {
		if _, err := l.client.About(l.ctx); err != nil {
			return err
		}
		has, err := l.client.HasBundle(l.ctx, hash)
		if err != nil || has {
			return err
		}
		post(a, func() {
			if l.stands == LinkReaching {
				l.stands = LinkSending
			}
		})
		return l.client.PutBundle(l.ctx, zipped, string(sig))
	}()
	post(a, func() {
		if err != nil {
			a.linkFailed(l, err)
			return
		}
		a.raiseLink(l, hash)
	})
}

// raiseLink begins the share, and registers it with the relay.
func (a *app) raiseLink(l *windowLink, hash string) {
	if l.stands == LinkEnded {
		return
	}
	cfg := a.links.times
	cfg.Ask = func(ctx context.Context, r guest.Request) bool { return a.askAbout(ctx, l, r) }
	cfg.Open = l.open
	cfg.OnChange = func(st guest.Status) { post(a, func() { a.linkStatus(l, st) }) }
	// Written to the window log, which this reaches from any goroutine.
	cfg.OnError = func(err error) { log.Printf("a shared window: %v", err) }
	host, err := guest.NewHost(cfg)
	if err != nil {
		a.linkFailed(l, err)
		return
	}
	l.host, l.status = host, host.Status()
	id := relay.NewShareID()
	l.link, err = relay.NewShareLink(l.client.Relay, id, host.Secret().String())
	if err == nil {
		l.ln, err = relay.ListenShare(relay.ShareConfig{
			Relay: l.client.Relay, ID: id, Token: relay.NewToken(), Bundle: hash, Dialer: l.client.Dialer,
			Upload: func(ctx context.Context) error {
				zipped, sig, ok := a.links.bundle()
				if !ok {
					return errNoClient
				}
				return l.client.PutBundle(ctx, zipped, string(sig))
			},
			OnState: func(err error) { post(a, func() { a.linkReached(l, err) }) },
		})
	}
	if err != nil {
		a.linkFailed(l, err)
		return
	}
	l.stands = LinkReaching
	a.tellLink(l)
	ln := l.ln
	go func() {
		if err := host.Serve(ln); err != nil {
			log.Printf("a shared window: %v", err)
		}
	}()
}

// linkReached hears how the share's place on the relay is going.
func (a *app) linkReached(l *windowLink, err error) {
	if l.stands == LinkEnded {
		return
	}
	if err == nil {
		l.reached, l.trouble, l.troubled = true, "", false
		if l.stands == LinkReaching {
			l.stands = LinkWaiting
		}
		if !l.was {
			l.was = true
			log.Printf("the shared window is on the relay, as %s", l.link.Shown())
			a.giveLink(l, "The link is copied", "Send it to one person: it works once.")
		}
		return
	}
	why := l.reason(err)
	if why != l.trouble {
		log.Printf("couldn't reach the relay for the shared window: %s; trying again", why)
	}
	if !l.troubled {
		l.troubled = true
		a.failed("Couldn't reach the relay", reason(errors.New(why))+"\n\nkakel keeps trying.")
		a.problem()
	}
	l.reached, l.trouble = false, why
}

// reason is why a share does not go, for the user, who is told
// beside it that it is about the relay.
func (l *windowLink) reason(err error) string {
	if errors.Is(err, relay.ErrOldRelay) {
		return errOldRelay.Error()
	}
	return strings.TrimPrefix(err.Error(), "relay "+l.client.Relay.Host()+": ")
}

// linkFailed ends a share that could not begin.
func (a *app) linkFailed(l *windowLink, err error) {
	if l.stands == LinkEnded {
		return
	}
	why := reason(errors.New(l.reason(err)))
	l.trouble = why
	a.failed(couldntShare, why)
	a.problem()
	a.linkEnded(l, "could not be shared")
	a.letLinkGo(l, guest.ReasonStopped)
}

// giveLink puts the link on the clipboard, and says so in its window.
func (a *app) giveLink(l *windowLink, title, body string) {
	a.postIn(l.win, Notice{Title: title, Body: body, Kind: NoticeWorked, Clipboard: l.link.String()})
}

// copyLink copies a link nobody has opened.
func (a *app) copyLink(id string) error {
	l := a.linkByID(id)
	if l == nil || l.stands != LinkWaiting {
		return errors.New("the link has been opened or has ended, and works no more")
	}
	a.giveLink(l, "Link copied", "It works once.")
	return nil
}

// askAbout asks the user about a guest. It runs on a goroutine of the
// share's, and waits for the answer.
func (a *app) askAbout(ctx context.Context, l *windowLink, r guest.Request) bool {
	answer := make(chan bool, 1)
	post(a, func() {
		if l.stands == LinkEnded || ctx.Err() != nil {
			return
		}
		l.answer, l.stands, l.status.Guest = answer, LinkAsking, r
		log.Printf("%s asks to join the shared window", guestForLog(r))
		w := a.winByID(l.win)
		a.pingsIn(w).Calls++
		if w != nil && w != a.cur {
			// Asked in the window shared: said where the user is.
			a.post(Notice{Title: guestName(r.Name) + " asks to join a shared window", Kind: NoticePlain, Icon: "user-round",
				Action: "Show", On: FocusPane{Pane: a.focusIn(w)}})
		}
	})
	select {
	case allow := <-answer:
		return allow
	case <-ctx.Done():
		return false
	}
}

// guestUnseen tells the user outside kakel's windows of a guest who asks.
func (a *app) guestUnseen(id string) {
	if l := a.linkByID(id); l != nil && l.stands == LinkAsking {
		a.popUp(guestName(l.status.Guest.Name)+" asks to join a shared window", "kakel waits for you to allow or refuse it.")
	}
}

// answerGuest lets a guest who asks in, or refuses it. One that has gone
// meanwhile has had its answer.
func (a *app) answerGuest(in AnswerGuest) {
	l := a.linkByID(in.ID)
	if l == nil || l.answer == nil {
		return
	}
	answer := l.answer
	l.answer = nil
	what := "refused"
	if in.Allow {
		what = "let in, to watch"
		if in.CanType {
			what = "let in, to type"
		}
		l.canType = in.CanType
		// Said before it is in, so what it may do is never more than this.
		a.tellLink(l)
	}
	log.Printf("%s was %s", guestForLog(l.status.Guest), what)
	answer <- in.Allow
}

// setLinkMay sets whether a guest may type.
func (a *app) setLinkMay(in SetLinkMay) {
	l := a.linkByID(in.ID)
	if l == nil || l.stands == LinkEnded || l.canType == in.CanType {
		return
	}
	l.canType = in.CanType
	a.tellLink(l)
	if l.status.Phase == guest.PhaseIn {
		what := "may only watch"
		if in.CanType {
			what = "may type"
		}
		log.Printf("%s %s now", guestForLog(l.status.Guest), what)
	}
}

// guestName is what a guest is called, for a guest with no name to show.
func guestName(name string) string {
	if name == "" {
		return "A guest"
	}
	return name
}

// guestForLog is a guest for the Window Log: what it calls itself, where
// it came from, and the browser it says it has.
func guestForLog(r guest.Request) string {
	s := fmt.Sprintf("the guest %q, from %s", r.Name, guestAddr(r.From))
	if r.About != "" {
		s += fmt.Sprintf(" (%s)", r.About)
	}
	return s
}

// guestAddr is the address a guest came from, off what the relay says of
// the connection, which begins with it.
func guestAddr(from string) string {
	addr, _, _ := strings.Cut(from, " ")
	return addr
}

// linkStatus hears how a share stands.
func (a *app) linkStatus(l *windowLink, st guest.Status) {
	if l.stands == LinkEnded {
		return
	}
	was := l.status
	l.status = st
	name := guestName(st.Guest.Name)
	switch st.Phase {
	case guest.PhaseJoin, guest.PhaseWaiting:
		// Asked about as askAbout hears of it.
	case guest.PhaseIn:
		l.stands = LinkIn
		if !st.Here {
			l.stands = LinkAway
		}
		switch {
		case was.Phase != guest.PhaseIn:
			l.since = time.Now()
			body := "Every terminal in the window is shown, and those opened there later."
			if !l.canType {
				body = "Only to watch. " + body
			}
			a.postIn(l.win, Notice{Title: name + " joined the window", Body: body, Kind: NoticeWorked, Icon: "user-round"})
			a.done()
		case was.Here && !st.Here:
			log.Printf("%s lost its connection", guestForLog(st.Guest))
		case !was.Here && st.Here:
			log.Printf("%s is back, from %s", guestForLog(st.Guest), guestAddr(st.Guest.From))
		}
	case guest.PhaseOver:
		l.answer = nil
		title, body, over := linkOver(st.Reason, name, was.Phase)
		a.linkEnded(l, over)
		a.letLinkGo(l, st.Reason)
		if title != "" {
			a.postIn(l.win, Notice{Title: title, Body: body, Icon: "user-round"})
		}
	}
}

// linkOver is what is said of a share that ended by itself, and the few
// words its card keeps. before is how far it had come.
func linkOver(why guest.Reason, name string, before guest.Phase) (title, body, over string) {
	switch why {
	case guest.ReasonExpired:
		return "The link expired", "Nobody opened it in time.", "expired"
	case guest.ReasonUnanswered:
		return name + " was not let in", "The question was not answered in time.", "not answered"
	case guest.ReasonRefused:
		return name + " was refused", "The link works no more.", "refused"
	case guest.ReasonLeft:
		return name + " left the window", "", "left"
	case guest.ReasonAway:
		if before == guest.PhaseWaiting {
			return name + " left before being let in", "The link works no more.", "left"
		}
		return name + " was disconnected", "The connection was lost for too long.", "connection lost"
	case guest.ReasonWindowClosed:
		return "", "", "window closed"
	}
	return "Sharing stopped", "", "stopped"
}

// linkEnded marks a share as over, and what it kept of the link gone.
func (a *app) linkEnded(l *windowLink, over string) {
	l.stands, l.over, l.reached, l.answer = LinkEnded, over, false, nil
	l.setPanes(nil)
	log.Printf("the shared window's link is over: %s", over)
	// The latest few, of each window.
	n := 0
	for i := len(a.links.all) - 1; i >= 0; i-- {
		if o := a.links.all[i]; o.win == l.win && o.stands == LinkEnded {
			if n++; n > mostEnded {
				a.links.all = slices.Delete(a.links.all, i, i+1)
			}
		}
	}
}

// letLinkGo tells the guest and the relay that a share is over, on a
// goroutine of its own.
func (a *app) letLinkGo(l *windowLink, why guest.Reason) {
	host, ln := l.host, l.ln
	a.links.ending.Go(func() {
		if host != nil {
			host.End(why)
		}
		// On the way out as a signal asks, a relay reached through a
		// saved server is not told: nothing new is dialled through one by
		// then. The guest has heard, the link is for a host that is no
		// more, and the relay lets go of the share by itself.
		if ln != nil {
			ctx, cancel := context.WithTimeout(context.Background(), hangUpWait)
			defer cancel()
			if err := ln.End(ctx); err != nil {
				log.Printf("the relay was not told that a shared window's link is over: %v", err)
			}
		}
		l.stop()
	})
}

// endLink ends one share for why, as the user or kakel itself ends it.
func (a *app) endLink(l *windowLink, why guest.Reason) {
	if l.stands == LinkEnded {
		return
	}
	over := "stopped"
	switch why {
	case guest.ReasonWindowClosed:
		over = "window closed"
	case guest.ReasonQuitting:
		over = "kakel closed"
	}
	a.linkEnded(l, over)
	a.letLinkGo(l, why)
}

// endLinks ends every share, and endLinksOf those of one window, closed.
func (a *app) endLinks(why guest.Reason) {
	for _, l := range slices.Clone(a.links.all) {
		a.endLink(l, why)
	}
}

func (a *app) endLinksOf(w *ownWin) {
	for _, l := range slices.Clone(a.links.all) {
		if l.win != w.id {
			continue
		}
		name, body := guestName(l.status.Guest.Name), "The shared window closed."
		switch l.stands {
		case LinkIn, LinkAway:
			a.notify(name+" was disconnected", body, "")
		case LinkAsking:
			a.notify(name+" was not let in", body, "")
		case LinkReaching, LinkSending, LinkWaiting:
			a.notify("The link ended", body, "")
		case LinkEnded:
		}
		a.endLink(l, guest.ReasonWindowClosed)
	}
	a.links.all = slices.DeleteFunc(a.links.all, func(l *windowLink) bool { return l.win == w.id })
}

// linksWait is the longest kakel waits, on its way out, for its guests to
// hear that it exits.
const linksWait = time.Second

// waitForLinks waits a moment for the shares ended to be let go.
func (a *app) waitForLinks() {
	done := make(chan struct{})
	go func() {
		a.links.ending.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(linksWait):
	}
}

// stopLink ends a share, as the user asked.
func (a *app) stopLink(id string) {
	if l := a.linkByID(id); l != nil {
		a.stopped([]*windowLink{l})
	}
}

// stopLinks ends every share of a window, or of every window for 0.
func (a *app) stopLinks(win int) {
	var stop []*windowLink
	for _, l := range a.links.all {
		if win == 0 || l.win == win {
			stop = append(stop, l)
		}
	}
	a.stopped(stop)
}

// stopped ends shares, and says so.
func (a *app) stopped(stop []*windowLink) {
	var names []string
	n := 0
	for _, l := range stop {
		if l.stands == LinkEnded {
			continue
		}
		n++
		if l.stands == LinkIn || l.stands == LinkAway {
			names = append(names, guestName(l.status.Guest.Name))
		}
		a.endLink(l, guest.ReasonStopped)
	}
	if n == 0 {
		return
	}
	body := ""
	if len(names) > 0 {
		body = listOf(names) + " can no longer see the window."
	}
	a.worked("Sharing stopped", body, "")
}

// clearLinks takes away a window's shares that have ended.
func (a *app) clearLinks(win int) {
	a.links.all = slices.DeleteFunc(a.links.all, func(l *windowLink) bool { return l.win == win && l.stands == LinkEnded })
}

// sharedWindows counts the windows with a share going on.
func (a *app) sharedWindows() int {
	seen := map[int]bool{}
	for _, l := range a.links.all {
		if l.stands != LinkEnded {
			seen[l.win] = true
		}
	}
	return len(seen)
}

// guestsIn counts the guests let into w, and those who ask to be, and
// the links to it that nobody has joined with.
func (a *app) guestsIn(w *ownWin) (guests, waiting int) {
	for _, l := range a.links.all {
		switch {
		case l.win != w.id || l.stands == LinkEnded:
		case l.stands == LinkIn || l.stands == LinkAway || l.stands == LinkAsking:
			guests++
		default:
			waiting++
		}
	}
	return guests, waiting
}

// tellLinks tells every share how its window stands, where that has
// changed. What is taken away from a guest is gone as it returns.
func (a *app) tellLinks() {
	for _, l := range a.links.all {
		a.tellLink(l)
	}
	a.sayJoined()
	a.links.byHand = ""
}

// sayJoined says which terminals have joined a window with a guest in it:
// the guest sees them from now on. One the user moved there themselves is
// not said.
func (a *app) sayJoined() {
	byWin := map[int][]*windowLink{}
	for _, l := range a.links.all {
		if l.stands == LinkIn || l.stands == LinkAway {
			byWin[l.win] = append(byWin[l.win], l)
		}
	}
	said := map[int]bool{}
	for _, l := range a.links.all {
		in := byWin[l.win]
		if len(in) == 0 || said[l.win] || in[0] != l {
			continue
		}
		said[l.win] = true
		var titles, names []string
		for _, p := range l.told.Panes {
			if p.Kind == guest.KindTerminal && !l.seen[p.ID] && p.ID != a.links.byHand {
				titles = append(titles, p.Title)
			}
		}
		for _, o := range in {
			names = append(names, guestName(o.status.Guest.Name))
		}
		if len(titles) == 0 {
			continue
		}
		what := titles[0] + " is"
		if len(titles) > 1 {
			what = words.ManyOf(len(titles), "terminal is", "terminals are")
		}
		a.postIn(l.win, Notice{Title: what + " now shared with " + listOf(names), Icon: "user-round", Key: "joined:" + itoa(l.win)})
	}
	for _, l := range a.links.all {
		l.seen = map[string]bool{}
		for _, p := range l.told.Panes {
			l.seen[p.ID] = p.Kind == guest.KindTerminal
		}
	}
}

// tellLink tells one share how its window stands.
func (a *app) tellLink(l *windowLink) {
	if l.host == nil || l.stands == LinkEnded {
		return
	}
	st, panes := a.sharedState(l)
	// Opened from now on, and then published: a pane gone from the one
	// is not opened before it is gone from the other.
	l.setPanes(panes)
	if l.tells > 0 && reflect.DeepEqual(st, l.told) {
		return
	}
	l.told = st
	l.tells++
	err := l.host.Publish(st)
	switch {
	case err == nil:
		l.unsaid = ""
	case err.Error() != l.unsaid:
		l.unsaid = err.Error()
		a.failed("Couldn't show the guest the whole window", reason(err))
	}
}

// sharedState is a window as its guest is told of it, and its terminals.
func (a *app) sharedState(l *windowLink) (guest.State, map[string]guest.PaneSource) {
	st := guest.State{HostName: l.hostName, CanType: l.canType}
	panes := map[string]guest.PaneSource{}
	w, win := a.winByID(l.win), a.layout.Window(l.win)
	if w == nil || w.gone || win == nil {
		return st, panes
	}
	byID := map[string]Pane{}
	for _, p := range a.panesIn(w) {
		byID[p.ID] = p
	}
	focus := a.focusIn(w)
	// In the layout's order, which the window's tabs have.
	for _, id := range win.Root.Items() {
		p, ok := byID[id]
		if !ok {
			continue
		}
		shown := guest.Pane{ID: p.ID, Title: kindWord(p.Kind), Kind: guest.KindOther, Icon: a.sharedPicture(p)}
		if t := a.terminal(p.ID); t != nil {
			shown.Kind, shown.Title, shown.Ended = guest.KindTerminal, sharedTitle(p), p.Ended
			size := t.Size()
			shown.Cols, shown.Rows = size.Cols, size.Rows
			panes[p.ID] = t
		}
		if id == focus {
			st.Title, st.HostIn = shown.Title, id
		}
		st.Panes = append(st.Panes, shown)
	}
	st.Layout = a.sharedLayout(win.Root, byID)
	st.Theme = a.sharedTheme()
	return st, panes
}

// sharedTitle is what a guest is told a terminal is called. A command's
// is its command line, which may hold a password, and so may be the title
// of a pane on another kakel, unless that says it is a shell's. A name
// the user gave is told.
func sharedTitle(p Pane) string {
	switch {
	case p.Named:
	case p.Command:
		return "Command"
	case p.MaybeCommand:
		return "Terminal"
	}
	return oneLine(p.Title)
}

// sharedPicture names a pane's image: a terminal in another kakel shows
// that kakel's, as its tab does.
func (a *app) sharedPicture(p Pane) string {
	if p.Kind == KindTerminal && p.Machine != "" && a.machines.IsWindow(p.Machine) {
		return "window"
	}
	return p.Picture()
}

// kindWord is what a guest is told of a pane that is not a terminal.
func kindWord(kind string) string {
	switch kind {
	case KindFileManager:
		return "Files"
	case KindReader:
		return "Reader"
	case KindLog:
		return "Log"
	case KindTunnel:
		return "Tunnel"
	case KindSecrets:
		return "Secrets"
	case KindSettings, KindMachine:
		return "Settings"
	case KindThemeEditor:
		return "Theme"
	case KindJobs:
		return "Jobs"
	case KindCopies:
		return "Copies"
	case KindLayouts:
		return "Layouts"
	case KindServers:
		return "Machines"
	case KindChooser:
		return "Split"
	}
	return "Pane"
}

// sharedLayout is a window's tree as it is sent.
func (a *app) sharedLayout(root *dock.Node, panes map[string]Pane) []byte {
	raw, err := guestlook.Layout(root, func(id string) bool { _, ok := panes[id]; return ok })
	if err != nil {
		log.Printf("a shared window: %v", err)
	}
	return raw
}

// sharedTheme is the theme on now as it is sent, nil for none.
func (a *app) sharedTheme() []byte {
	i := slices.IndexFunc(a.themes, func(t look.Themed) bool { return t.Name == a.st.Theme })
	if i < 0 {
		return nil
	}
	raw, err := guestlook.Theme{Look: a.themes[i], FontSize: a.st.FontSize, Font: a.st.Font.Name}.Bytes()
	if err != nil {
		log.Printf("a shared window: %v", err)
	}
	return raw
}

// windowLinks are the shares as the windows show them.
func (a *app) windowLinks() []WindowLink {
	var out []WindowLink
	for _, l := range a.links.all {
		st := l.status
		s := WindowLink{
			ID: l.id, Window: l.win, CanType: l.canType, Stands: l.stands, Over: l.over, Relay: l.client.Relay.Host(),
			Guest: st.Guest.Name, From: guestAddr(st.Guest.From), About: st.Guest.About,
			Since: l.since, In: st.Focus, TypedIn: st.TypedIn, TypedAt: st.TypedAt, Until: st.Until,
			Trouble: l.trouble,
		}
		switch l.stands {
		case LinkWaiting:
			s.Link = l.link.String()
		case LinkReaching, LinkSending, LinkEnded:
			s.Until = time.Time{}
		case LinkAsking, LinkIn, LinkAway:
		}
		out = append(out, s)
	}
	return out
}

// linkSetup is what the next window is shared with: as last set, and
// before that through the relay remote access goes through, or the one
// last connected through.
func (a *app) linkSetup() LinkSetup {
	s := LinkSetup{NoClient: !a.links.hasClient(), Tries: a.links.tries}
	if a.settings != nil {
		kept := a.settings.ShareLink()
		s.Name, s.Watch, s.Relay, s.Via = kept.Name, kept.Watch, kept.Relay, machines.ID(kept.Via)
		if s.Relay == "" {
			r := a.settings.Relay()
			s.Relay, s.Via = r.URL, machines.ID(r.Via)
		}
		if s.Relay == "" {
			s.Relay, s.Via = a.settings.RelayLast(), ""
		}
	}
	if s.Name == "" {
		s.Name = a.ownName()
	}
	return s
}

// setLinkSetup keeps what windows are shared with.
func (a *app) setLinkSetup(in SetLinkSetup) error {
	if a.settings == nil {
		return errNoSettings
	}
	at := strings.TrimSpace(in.Relay)
	if at != "" {
		u, err := relay.ParseURL(at)
		if err != nil {
			return err
		}
		at = u.String()
	}
	return a.settings.PutShareLink(settings.ShareLink{Name: oneLine(in.Name), Watch: in.Watch, Relay: at, Via: string(in.Via)})
}

// ownName is what a guest is told the host is called, before the user
// says: the user's name, or else this machine's.
func (a *app) ownName() string {
	if a.links.name == "" {
		a.links.name = findOwnName()
	}
	return a.links.name
}

func findOwnName() string {
	if runtime.GOOS == "windows" {
		// Its full name is asked of the domain, which takes its time.
		if name := oneLine(os.Getenv("USERNAME")); name != "" {
			return name
		}
	} else if u, err := user.Current(); err == nil {
		full, _, _ := strings.Cut(u.Name, ",")
		for _, name := range []string{full, u.Username} {
			if name = oneLine(name); name != "" {
				return name
			}
		}
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		return host
	}
	return "Somebody"
}

var _ guest.PaneSource = (*uiterm.Terminal)(nil)
