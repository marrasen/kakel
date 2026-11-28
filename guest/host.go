package guest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	uiterm "github.com/marrasen/kakel/ui/term"
)

const (
	// handshakesAtOnce is how many callers may be part way through proving
	// a key, and unjoinedAtOnce how many may hold the link's without
	// having joined. A share has one guest, so neither is ever many. One
	// more is taken on in place of one that is there: see makeRoomLocked.
	handshakesAtOnce = 4
	unjoinedAtOnce   = 4

	// authTries is how many keys a caller may offer. A guest has two: its
	// own, and the link's.
	authTries = 3

	// quietFor is how long is waited before another caller turned away is
	// reported, so one in a loop makes no flood of news.
	quietFor = 5 * time.Second

	// typedAgain is how long after a guest typed in a pane it is told that
	// it types there still.
	typedAgain = time.Second
)

// How long a share waits for things, for a HostConfig that does not say.
const (
	LinkFor    = 15 * time.Minute
	AskFor     = 10 * time.Minute
	AskAwayFor = 2 * time.Minute
	AwayFor    = 30 * time.Minute
)

// Request is a guest asking to come in. Its words are the guest's, made
// plain enough to show.
type Request struct {
	// Name is what the guest called itself, and About the browser and the
	// system it says it has.
	Name, About string

	// From is the address it came from, as the connection says.
	From string
}

// PaneSource is a pane as it is shared: a *uiterm.Terminal, or something
// that stands in front of one.
type PaneSource interface {
	// Follow shows the pane to f until stop, and CatchUp gives f the
	// screen again: see uiterm.Terminal.Follow.
	Follow(f uiterm.Follower) (stop func())
	CatchUp(f uiterm.Follower)

	// Lane is what the guest types on: see uiterm.Terminal.Lane.
	Lane() uiterm.Lane
}

var _ PaneSource = (*uiterm.Terminal)(nil)

// HostConfig is what a share is made of.
type HostConfig struct {
	// Ask asks the host whether a guest may come in. It is called once,
	// on a goroutine of its own, and ctx ends when the answer is no longer
	// wanted: the guest left, or nobody answered in time.
	Ask func(ctx context.Context, r Request) (allow bool)

	// Open is the pane the guest asks to be shown. It is called for each
	// time one is opened, on a goroutine of its own, and has to check
	// then that the pane stands in the shared window and is a terminal: an
	// error is a pane that is not shown. A connection that ended is not
	// done with until it has returned.
	Open func(id string) (PaneSource, error)

	// OnChange hears how the share stands each time that changes, the
	// latest only, on a goroutine of the share's. The last it hears is
	// PhaseOver.
	OnChange func(Status)

	// OnError hears what went wrong with a caller or with the guest, for
	// the log. Nothing the user has to act on comes here.
	OnError func(error)

	// LinkFor is how long the link waits to be joined with, AskFor how
	// long the host is asked, AskAwayFor how long a guest may be gone
	// while asked about, and AwayFor how long once in. Zero is the
	// constant of that name.
	LinkFor, AskFor, AskAwayFor, AwayFor time.Duration
}

// Status is how a share stands, for its host.
type Status struct {
	Phase Phase
	// Reason is why, once the phase is PhaseOver.
	Reason Reason

	// Guest is who joined, from PhaseWaiting on.
	Guest Request
	// Here says the guest is connected now.
	Here bool
	// Focus is the pane the guest is in, TypedIn the one it last typed in
	// and TypedAt when, zero before it has.
	Focus, TypedIn string
	TypedAt        time.Time
	// Follows says the guest follows the host: Focus is then the pane the
	// host is in, as the host last published it, whatever the guest's page
	// has drawn.
	Follows bool

	// Until is when the phase ends by itself: a link's expiry, the end of
	// asking, or the end of waiting for a guest who is away. Zero while
	// nothing is waited for.
	Until time.Time
}

// Host is one share of one window, from the link made to the end. It
// takes connections from whatever listeners it is told to serve, and has
// at most one guest.
type Host struct {
	cfg    HostConfig
	signer ssh.Signer
	secret Secret
	// link is the link key's public half, on the wire.
	link []byte

	// ctx ends with the share.
	ctx    context.Context
	cancel context.CancelFunc

	// pubMu orders the states published.
	pubMu sync.Mutex

	// typeMu is held reading while something is typed, so that once it
	// has been held writing, nothing more is typed under what it was. It
	// guards canType. mu may be taken with it held, and not the other way.
	typeMu  sync.RWMutex
	canType bool

	// mu guards what is under it. Nothing that can wait is called with it
	// held.
	mu    sync.Mutex
	phase Phase
	why   Reason
	guest Request
	// session is the guest's own key, on the wire, once it has joined.
	session []byte
	// cur is the guest's connection, nil while it is away.
	cur *hostConn
	// state is the window's, as a frame, and panes the terminals in it.
	state []byte
	panes map[string]bool

	// focus is the pane the guest picked, and follows says it picked none
	// and follows the host, who is in hostIn.
	focus, typedIn string
	typedAt        time.Time
	follows        bool
	hostIn         string

	conns map[*hostConn]struct{}
	// arriving are the callers that prove a key, and unjoined those that
	// hold the link and have not joined, the oldest first.
	arriving []arrival
	unjoined []*hostConn
	lns      map[net.Listener]struct{}

	// phaseEnd ends the phase by itself, at until; away ends the share of
	// a guest who is gone, at awayUntil.
	phaseEnd, away   *time.Timer
	until, awayUntil time.Time

	// refused counts the callers turned away since the last was reported,
	// at toldAt.
	refused int
	toldAt  time.Time

	wake chan struct{}
}

// NewHost begins a share: the link can be joined with from now, once Serve
// has somewhere to take connections from.
func NewHost(cfg HostConfig) (*Host, error) {
	if cfg.Ask == nil || cfg.Open == nil {
		return nil, errors.New("a share needs somebody to ask, and panes to open")
	}
	for _, d := range []struct {
		of *time.Duration
		to time.Duration
	}{{&cfg.LinkFor, LinkFor}, {&cfg.AskFor, AskFor}, {&cfg.AskAwayFor, AskAwayFor}, {&cfg.AwayFor, AwayFor}} {
		if *d.of <= 0 {
			*d.of = d.to
		}
	}
	signer, secret, err := newShare()
	if err != nil {
		return nil, err
	}
	link, err := secret.linkKey()
	if err != nil {
		return nil, err
	}
	h := &Host{
		cfg: cfg, signer: signer, secret: secret, link: link.PublicKey().Marshal(),
		panes: map[string]bool{}, conns: map[*hostConn]struct{}{},
		lns:  map[net.Listener]struct{}{},
		wake: make(chan struct{}, 1),
	}
	h.state, err = encodeState(State{})
	if err != nil {
		return nil, err
	}
	h.ctx, h.cancel = context.WithCancel(context.Background())
	h.mu.Lock()
	h.armLocked(&h.phaseEnd, &h.until, cfg.LinkFor, ReasonExpired)
	h.mu.Unlock()
	if cfg.OnChange != nil {
		go h.tell()
	}
	return h, nil
}

// Secret is what the link carries.
func (h *Host) Secret() Secret { return h.secret }

// Status is how the share stands now.
func (h *Host) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := Status{
		Phase: h.phase, Reason: h.why, Guest: h.guest, Here: h.cur != nil,
		Focus: h.inLocked(), TypedIn: h.typedIn, TypedAt: h.typedAt, Until: h.until, Follows: h.follows,
	}
	if !h.awayUntil.IsZero() && (st.Until.IsZero() || h.awayUntil.Before(st.Until)) {
		st.Until = h.awayUntil
	}
	return st
}

// tell tells OnChange each time the share changes, and ends with it.
func (h *Host) tell() {
	for range h.wake {
		st := h.Status()
		h.cfg.OnChange(st)
		if st.Phase == PhaseOver {
			return
		}
	}
}

// Serve takes connections from ln until the share is over, and then
// returns nil, or until ln fails. A share whose listener failed goes on:
// another may be served in its place, as after a relay was lost. Serve
// closes ln.
func (h *Host) Serve(ln net.Listener) error {
	h.mu.Lock()
	over := h.phase == PhaseOver
	if !over {
		h.lns[ln] = struct{}{}
	}
	h.mu.Unlock()
	if over {
		return ln.Close()
	}
	for {
		nc, err := ln.Accept()
		if err == nil {
			go h.handshake(nc)
			continue
		}
		h.mu.Lock()
		delete(h.lns, ln)
		over := h.phase == PhaseOver
		h.mu.Unlock()
		_ = ln.Close()
		if over {
			return nil
		}
		return fmt.Errorf("take a guest: %w", err)
	}
}

// Publish says how the shared window stands now. The guest is told the
// latest once it is let in, and nothing of it before.
//
// A pane that is no longer listed, or no longer a terminal, is the guest's
// no more: once Publish has returned, nothing its program writes is sent
// and nothing is typed into it, so Publish has to come before such a pane
// shows anything that is not shared. The same goes for CanType.
//
// A state that cannot be sent, as one too large, takes all of that away
// the same. The guest is then told the least of it, with no layout and no
// theme, and the error says why.
func (h *Host) Publish(st State) error {
	panes := make(map[string]bool, len(st.Panes))
	for _, p := range st.Panes {
		if p.Kind == KindTerminal {
			panes[p.ID] = true
		}
	}

	h.pubMu.Lock()
	defer h.pubMu.Unlock()
	h.mu.Lock()
	if h.phase == PhaseOver {
		h.mu.Unlock()
		return nil
	}
	// What is taken away first: whatever comes after, that holds.
	was := h.inLocked()
	h.panes, h.hostIn = panes, st.HostIn
	if !panes[h.focus] {
		h.focus = ""
	}
	if h.inLocked() != was {
		nudge(h.wake)
	}
	var gone, kept []*stream
	for c := range h.conns {
		gone = append(gone, c.withdrawLocked(func(id string) bool { return !panes[id] })...)
		for _, s := range c.streams {
			kept = append(kept, s)
		}
		for s := range c.closing {
			kept = append(kept, s)
		}
	}
	h.mu.Unlock()
	for _, s := range gone {
		s.end()
	}
	h.typeMu.Lock()
	h.canType = st.CanType
	if !st.CanType {
		for _, s := range kept {
			s.dropTyped()
		}
	}
	h.typeMu.Unlock()

	state, err := encodeState(st)
	if err != nil {
		least, also := encodeState(st.least())
		if also != nil {
			// Not to go on with a guest that is told what is no more.
			go h.End(ReasonStopped)
			return errors.Join(err, also)
		}
		state = least
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.phase == PhaseOver {
		return err
	}
	h.state = state
	if h.cur != nil && h.phase == PhaseIn {
		h.cur.out.put(state)
	}
	return err
}

// End ends the share, telling the guest why, and waits a moment for it to
// hear. It is the host's Stop Sharing, its window closing, or kakel
// quitting.
func (h *Host) End(why Reason) {
	h.mu.Lock()
	if h.phase == PhaseOver {
		h.mu.Unlock()
		return
	}
	h.phase, h.why = PhaseOver, why
	h.disarmLocked(&h.phaseEnd, &h.until)
	h.disarmLocked(&h.away, &h.awayUntil)
	h.state, h.panes, h.focus, h.follows, h.cur = nil, nil, "", false, nil
	conns := make([]*hostConn, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	arriving := h.arriving
	h.arriving = nil
	lns := h.lns
	h.lns = map[net.Listener]struct{}{}
	h.mu.Unlock()

	h.cancel()
	for ln := range lns {
		_ = ln.Close()
	}
	for _, a := range arriving {
		_ = a.nc.Close()
	}
	h.typeMu.Lock()
	h.canType = false
	h.typeMu.Unlock()
	var told sync.WaitGroup
	for _, c := range conns {
		told.Go(func() { c.end(why) })
	}
	told.Wait()
	nudge(h.wake)
}

// armLocked has the share end for why once d is over, unless slot is
// armed again or disarmed first. mu is held, as it is for disarmLocked.
func (h *Host) armLocked(slot **time.Timer, until *time.Time, d time.Duration, why Reason) {
	h.disarmLocked(slot, until)
	*until = time.Now().Add(d)
	var t *time.Timer
	t = time.AfterFunc(d, func() {
		h.mu.Lock()
		due := *slot == t
		h.mu.Unlock()
		if due {
			h.End(why)
		}
	})
	*slot = t
}

func (h *Host) disarmLocked(slot **time.Timer, until *time.Time) {
	if *slot != nil {
		(*slot).Stop()
	}
	*slot, *until = nil, time.Time{}
}

// report tells OnError of a caller turned away or a guest that broke the
// rules, at most once every quietFor and with a count of those since.
func (h *Host) report(err error) {
	if h.cfg.OnError == nil {
		return
	}
	h.mu.Lock()
	now := time.Now()
	h.refused++
	if !h.toldAt.IsZero() && now.Sub(h.toldAt) < quietFor {
		h.mu.Unlock()
		return
	}
	since := h.refused - 1
	h.refused, h.toldAt = 0, now
	h.mu.Unlock()
	// What went wrong may be told in the far end's own words.
	text := plain(err.Error(), mostTitle)
	if since > 0 {
		text = fmt.Sprintf("%s (and %d more since)", text, since)
	}
	h.cfg.OnError(errors.New(text))
}

// role is which key a caller proved.
type role string

const (
	// roleLink is the link's, which says no more than that the caller has
	// the link, spent or not.
	roleLink role = "link"
	// roleSession is the guest's own.
	roleSession role = "session"
)

// roleExt carries the role from the proof of the key to the connection.
const roleExt = "kakel-guest-role"

// roleOf is the role key has now.
func (h *Host) roleOf(key ssh.PublicKey) (role, bool) {
	if key.Type() != ssh.KeyAlgoED25519 {
		return "", false
	}
	wire := key.Marshal()
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.phase == PhaseOver:
		return "", false
	case h.session != nil && bytes.Equal(wire, h.session):
		return roleSession, true
	case bytes.Equal(wire, h.link):
		// Good for being told the link is spent, once it is.
		return roleLink, true
	}
	return "", false
}

// serverConfig is the way in: those two keys, and nothing else.
func (h *Host) serverConfig() *ssh.ServerConfig {
	errKey := errors.New("that key is not this share's")
	cfg := &ssh.ServerConfig{
		Config:                  algorithms(),
		PublicKeyAuthAlgorithms: keyAlgorithms,
		MaxAuthTries:            authTries,
		ServerVersion:           version,
		// For every key offered, also ones never signed with, so what is
		// decided travels with the key it was decided about.
		PublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			r, ok := h.roleOf(key)
			if !ok {
				return nil, errKey
			}
			return &ssh.Permissions{Extensions: map[string]string{roleExt: string(r)}}, nil
		},
		// Once the key is proved, and asked again: what was decided when
		// it was offered may have changed.
		VerifiedPublicKeyCallback: func(_ ssh.ConnMetadata, key ssh.PublicKey, perms *ssh.Permissions, _ string) (*ssh.Permissions, error) {
			r, ok := h.roleOf(key)
			if !ok || perms == nil || perms.Extensions[roleExt] != string(r) {
				return nil, errKey
			}
			return perms, nil
		},
	}
	cfg.AddHostKey(h.signer)
	return cfg
}

// handshake takes one connection, from a stranger to the end of it.
func (h *Host) handshake(nc net.Conn) {
	h.mu.Lock()
	over := h.phase == PhaseOver
	var out net.Conn
	if !over {
		h.arriving = append(h.arriving, arrival{nc, addrHost(nc)})
		out = h.makeRoomLocked()
	}
	h.mu.Unlock()
	if over {
		_ = nc.Close()
		return
	}
	if out != nil {
		_ = out.Close()
		h.report(fmt.Errorf("%s was turned away from a shared window: %d callers are already connecting", out.RemoteAddr(), handshakesAtOnce))
	}
	timer := time.AfterFunc(handshakeWindow, func() { _ = nc.Close() })
	live := newHeard(nc)
	conn, chans, reqs, err := ssh.NewServerConn(live, h.serverConfig())
	inTime := timer.Stop()
	h.mu.Lock()
	h.arriving = slices.DeleteFunc(h.arriving, func(a arrival) bool { return a.nc == nc })
	h.mu.Unlock()
	if err != nil || !inTime {
		_ = nc.Close()
		if err == nil {
			err = errors.New("it took too long")
		}
		h.report(fmt.Errorf("%s could not connect to a shared window: %w", nc.RemoteAddr(), err))
		return
	}
	ctx, cancel := context.WithCancel(h.ctx)
	defer cancel()
	c := newHostConn(ctx, h, conn, role(conn.Permissions.Extensions[roleExt]))
	oldest, ok := h.add(c)
	if oldest != nil {
		_ = oldest.conn.Close()
	}
	if !ok {
		_ = conn.Close()
		return
	}
	c.run(live, chans, reqs)
	h.drop(c)
}

// arrival is a caller that has proved nothing yet, and the machine it
// calls from.
type arrival struct {
	nc   net.Conn
	from string
}

// addrHost is the machine nc comes from, as far as its address says.
func addrHost(nc net.Conn) string {
	addr := nc.RemoteAddr().String()
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// makeRoomLocked takes one caller off those arriving when there are too
// many, for the caller of this to hang up on: the oldest of the machine
// that calls most. Callers that say nothing keep nobody out then, and one
// machine that calls without end turns out only its own.
func (h *Host) makeRoomLocked() net.Conn {
	if len(h.arriving) <= handshakesAtOnce {
		return nil
	}
	calls := map[string]int{}
	most := 0
	for _, a := range h.arriving {
		calls[a.from]++
		most = max(most, calls[a.from])
	}
	i := slices.IndexFunc(h.arriving, func(a arrival) bool { return calls[a.from] == most })
	out := h.arriving[i].nc
	h.arriving = slices.Delete(h.arriving, i, i+1)
	return out
}

// add takes on a connection that has proved a key, and reports whether
// there is anything to take it on for. out is one that held the link
// without joining, to hang up on in its place.
func (h *Host) add(c *hostConn) (out *hostConn, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.phase == PhaseOver {
		return nil, false
	}
	if c.role == roleLink {
		h.unjoined = append(h.unjoined, c)
		if len(h.unjoined) > unjoinedAtOnce {
			out = h.unjoined[0]
			h.countLocked(out)
		}
	}
	h.conns[c] = struct{}{}
	return out, true
}

// drop forgets a connection that has gone. The guest's begins its time
// away.
func (h *Host) drop(c *hostConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.conns, c)
	h.countLocked(c)
	if h.cur != c {
		return
	}
	h.cur, h.focus, h.follows = nil, "", false
	h.awayLocked()
	nudge(h.wake)
}

// countLocked takes c off those that have not joined.
func (h *Host) countLocked(c *hostConn) {
	h.unjoined = slices.DeleteFunc(h.unjoined, func(have *hostConn) bool { return have == c })
}

// awayLocked begins a guest's time away, when it has one.
func (h *Host) awayLocked() {
	switch h.phase {
	case PhaseWaiting:
		h.armLocked(&h.away, &h.awayUntil, h.cfg.AskAwayFor, ReasonAway)
	case PhaseIn:
		h.armLocked(&h.away, &h.awayUntil, h.cfg.AwayFor, ReasonAway)
	case PhaseJoin, PhaseOver:
	}
}

// greet tells a connection how things stand, once its control channel is
// open. why is not empty for one that has to go, and replaced is one that
// has to go in its favour.
func (h *Host) greet(c *hostConn) (why Reason, replaced *hostConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.phase == PhaseOver:
		return h.why, nil
	case c.role == roleLink && h.phase == PhaseJoin:
		c.out.put(frame(frameJoin))
		return "", nil
	case c.role == roleLink:
		return ReasonUsed, nil
	}
	replaced = h.cur
	h.takeLocked(c)
	return "", replaced
}

// takeLocked makes c the guest's connection, and tells it what a guest is
// told. That is put with mu held, so a state published meanwhile is never
// put before one it stands in place of.
func (h *Host) takeLocked(c *hostConn) {
	// Where it is, the guest says again.
	h.cur, h.focus, h.follows = c, "", false
	h.countLocked(c)
	h.disarmLocked(&h.away, &h.awayUntil)
	c.out.put(h.viewLocked())
	nudge(h.wake)
}

// viewLocked is what the guest is told of the window now: nothing but that
// it waits, until it is let in.
func (h *Host) viewLocked() []byte {
	if h.phase == PhaseIn {
		return h.state
	}
	return frame(frameWaiting)
}

// join has c join, the first that does: why says what it is told when it
// is not.
func (h *Host) join(c *hostConn, j joining, key ssh.PublicKey) (why Reason, err error) {
	wire := key.Marshal()
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case h.phase == PhaseOver:
		return h.why, nil
	case c.role != roleLink || h.cur == c:
		return "", fmt.Errorf("%w: a join after joining", errFrame)
	case h.phase != PhaseJoin:
		return ReasonUsed, nil
	case bytes.Equal(wire, h.link):
		// It would keep the link alive.
		return "", fmt.Errorf("%w: a join with the link's key", errFrame)
	}
	// The link is spent from here, whatever the host says.
	h.session = wire
	h.phase = PhaseWaiting
	h.guest = Request{Name: j.Name, About: j.About, From: plain(c.conn.RemoteAddr().String(), mostAbout)}
	h.armLocked(&h.phaseEnd, &h.until, h.cfg.AskFor, ReasonUnanswered)
	h.takeLocked(c)
	go h.ask(h.guest)
	return "", nil
}

// ask asks the host about r, and does as it says.
func (h *Host) ask(r Request) {
	if !h.cfg.Ask(h.ctx, r) {
		h.End(ReasonRefused)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.phase != PhaseWaiting {
		return
	}
	h.phase = PhaseIn
	h.disarmLocked(&h.phaseEnd, &h.until)
	nudge(h.wake)
	if h.cur == nil {
		// Away, and from now for as long as a guest who is in may be.
		h.awayLocked()
		return
	}
	h.cur.out.put(h.viewLocked())
}

// focused notes the pane the guest says it picked: none that is shared,
// where that is not one.
func (h *Host) focused(c *hostConn, id string) {
	if !h.panes[id] {
		id = ""
	}
	h.place(c, id, false)
}

// followed notes that the guest follows the host.
func (h *Host) followed(c *hostConn) { h.place(c, "", true) }

// place notes where the guest says it is.
func (h *Host) place(c *hostConn, focus string, follows bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur != c || h.phase != PhaseIn || focus == h.focus && follows == h.follows {
		return
	}
	h.focus, h.follows = focus, follows
	nudge(h.wake)
}

// inLocked is the pane the guest is in: the host's while it follows, and
// only one that is shared.
func (h *Host) inLocked() string {
	switch {
	case !h.follows:
		return h.focus
	case h.panes[h.hostIn]:
		return h.hostIn
	}
	return ""
}

// typed notes that the guest typed in a pane.
func (h *Host) typed(id string) {
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	if id == h.typedIn && now.Sub(h.typedAt) < typedAgain {
		return
	}
	h.typedIn, h.typedAt = id, now
	nudge(h.wake)
}

// leave is the guest saying it goes.
func (h *Host) leave(c *hostConn) {
	h.mu.Lock()
	is := h.cur == c
	h.mu.Unlock()
	if is {
		// Not on this goroutine, which the end waits for.
		go h.End(ReasonLeft)
	}
}
