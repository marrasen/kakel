//go:build !js

package relay

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// The limits of shares. A host has the relay's secret; a guest has the
// id of a share and no more.
const (
	// mostShares is how many shares there may be at once, and
	// mostSharesFrom how many of them from one address.
	mostShares     = 2000
	mostSharesFrom = 64
	// mostGuests is how many guests may be waiting for one share's host
	// at once.
	mostGuests = 4
	// mostJoined is how many streams one share may have joined. A share
	// has one guest; the rest are those it left behind when its network
	// dropped, and others who have the link, until the host has turned
	// them away.
	mostJoined = 16
	// mostPast is how many shares the relay remembers that there were,
	// and mostPastFrom how many of them from one address. pastFor is how
	// long each is.
	mostPast     = 10000
	mostPastFrom = 1000
	pastFor      = 24 * time.Hour
	// mostDownloads is how many files of the web client one share may be
	// sending, and mostDownloadsFrom how many of them to one address: a
	// browser asks for six at once.
	mostDownloads     = 32
	mostDownloadsFrom = 12
)

// graceFor is how long a share is kept whose host has left without
// ending it: long enough for a network to come back, or a relay to be
// started again.
var graceFor = 2 * time.Minute

// backWithin is how long a guest waits for a host that has left. One
// that does not make it tries again by itself.
var backWithin = 10 * time.Second

// share is a window shared with a link.
type share struct {
	id, token string
	// from is the address its host last registered from.
	from string
	// bundle is the web client its guests are sent.
	bundle *keptBundle
	// host is its host, nil while it is away. back is then closed when it
	// is back, and over by its going for good, as forget does.
	host   *host
	back   chan struct{}
	over   chan struct{}
	forget *time.Timer
	// waiting and joined count its guests' streams.
	waiting, joined int
	// paced is when it was joined, and pacedFrom the same by address.
	paced     time.Time
	pacedFrom map[string]time.Time
	// sending counts the files being sent of it, by address, and
	// downloads all of them.
	sending   map[string]int
	downloads int
}

// pace is how often something may be done: burst times at once, and once
// every so long after that.
type pace struct {
	burst int
	every time.Duration
}

// ok reports whether it may be done now, after it was done as far as at
// says, and what at is once it has been.
func (p pace) ok(at, now time.Time) (time.Time, bool) {
	if at.Before(now) {
		at = now
	}
	if at.Sub(now) > time.Duration(p.burst-1)*p.every {
		return at, false
	}
	return at.Add(p.every), true
}

// mayJoinLocked reports whether a share may have its host asked for a
// guest at the address key now, and counts it when it may. Both are
// held to a pace, so nobody who has a link floods its host, and one who
// does from one address leaves room for another. s.mu is held.
func (s *Server) mayJoinLocked(sh *share, key string, now time.Time) bool {
	from, fromOK := s.guestJoins.ok(sh.pacedFrom[key], now)
	all, allOK := s.shareJoins.ok(sh.paced, now)
	if !fromOK || !allOK {
		return false
	}
	if sh.pacedFrom == nil {
		sh.pacedFrom = map[string]time.Time{}
	}
	// Those whose pace is made up for: no more are left than the share's
	// own pace lets there be.
	for k, at := range sh.pacedFrom {
		if at.Before(now) {
			delete(sh.pacedFrom, k)
		}
	}
	sh.paced, sh.pacedFrom[key] = all, from
	return true
}

// sendingTo counts a file of a share being sent to the address from, and
// returns what is called once it has been, or nil when as many are as
// may be.
func (s *Server) sendingTo(sh *share, from string) (done func()) {
	key := addrKey(from)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sh.downloads >= mostDownloads || sh.sending[key] >= mostDownloadsFrom {
		return nil
	}
	if sh.sending == nil {
		sh.sending = map[string]int{}
	}
	sh.downloads++
	sh.sending[key]++
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		sh.downloads--
		if sh.sending[key]--; sh.sending[key] == 0 {
			delete(sh.sending, key)
		}
	}
}

// pastShare is a share there was: it ended, or its host stayed away. No
// other host may take its id, which its guests still have.
type pastShare struct {
	// token is its host's, hashed.
	token [sha256.Size]byte
	// from is the address it was shared from, at when it was last there,
	// and ended says it is not to come back to its own host either.
	from  string
	at    time.Time
	ended bool
	// turn counts the shares remembered, which the clock may not tell
	// apart.
	turn uint64
}

// is reports whether token is the past share's.
func (p pastShare) is(token string) bool {
	sum := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(p.token[:], sum[:]) == 1
}

// turnLocked is the next turn. s.mu is held.
func (s *Server) turnLocked() uint64 {
	s.turn++
	return s.turn
}

// rememberLocked remembers a share there was. s.mu is held.
func (s *Server) rememberLocked(id, token, from string, ended bool) {
	now, key := time.Now(), addrKey(from)
	delete(s.past, id)
	// The oldest make room: of the same address, when it has as many as
	// it may, and of all otherwise.
	var oldest, oldestFrom string
	n := 0
	for k, p := range s.past {
		if now.Sub(p.at) > pastFor {
			delete(s.past, k)
			continue
		}
		if oldest == "" || p.turn < s.past[oldest].turn {
			oldest = k
		}
		if p.from == key {
			n++
			if oldestFrom == "" || p.turn < s.past[oldestFrom].turn {
				oldestFrom = k
			}
		}
	}
	switch {
	case n >= mostPastFrom:
		delete(s.past, oldestFrom)
	case len(s.past) >= mostPast:
		delete(s.past, oldest)
	}
	s.past[id] = pastShare{token: sha256.Sum256([]byte(token)), from: key, at: now, ended: ended, turn: s.turnLocked()}
}

// pastLocked is the past share id, and whether there was one that is
// remembered. s.mu is held.
func (s *Server) pastLocked(id string) (pastShare, bool) {
	p, ok := s.past[id]
	return p, ok && time.Since(p.at) <= pastFor
}

// serveShare registers the host of a shared window under its id, and
// tells it of each guest until it leaves.
func (s *Server) serveShare(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, hash := q.Get("id"), q.Get("bundle")
	if !isShareID(id) || !isHash(hash) {
		http.Error(w, "a share is registered with an id and the hash of its web client", http.StatusBadRequest)
		return
	}
	token := hostToken(w, r)
	if token == "" {
		return
	}
	h := newHost("share "+shownID(id), token, clientAddr(r), mostGuests)
	s.mu.Lock()
	why, status := s.mayShareLocked(id, token, hash, h.from)
	if why != "" {
		s.mu.Unlock()
		http.Error(w, why, status)
		return
	}
	sh := s.shares[id]
	if sh == nil {
		sh = &share{id: id, token: token, over: make(chan struct{})}
		s.shares[id] = sh
		delete(s.past, id)
	}
	sh.from = h.from
	s.useLocked(sh, s.bundles[hash])
	switch {
	case sh.host != nil:
		// The same window, back before the relay noticed it had gone.
		sh.host.turnOut(registeredAgain)
	case sh.back != nil:
		sh.forget.Stop()
		close(sh.back)
	}
	sh.host, sh.back = h, nil
	s.mu.Unlock()
	defer s.leaveShare(sh, h)
	s.hold(w, r, h)
}

// mayShareLocked says why a window may not be shared under id, with the
// web client hash, and the status saying so, or "". s.mu is held.
func (s *Server) mayShareLocked(id, token, hash, from string) (string, int) {
	sh := s.shares[id]
	if sh != nil && !same(sh.token, token) {
		return "that link is in use by another kakel; make a new link", http.StatusConflict
	}
	if was, ok := s.pastLocked(id); ok {
		switch {
		case !was.is(token):
			return "that link was another kakel's; make a new link", http.StatusConflict
		case was.ended:
			return "that share has ended; make a new link", http.StatusGone
		}
	}
	if s.bundles[hash] == nil {
		return "the relay does not have that web client", http.StatusFailedDependency
	}
	if sh != nil {
		// Taking its own place back takes no more room.
		return "", 0
	}
	if len(s.shares) >= mostShares {
		return "the relay has as many shared windows as it takes", http.StatusServiceUnavailable
	}
	n, key := 0, addrKey(from)
	for _, o := range s.shares {
		if addrKey(o.from) == key {
			n++
		}
	}
	if n >= mostSharesFrom {
		return fmt.Sprintf("the relay takes %d shared windows from one address", mostSharesFrom), http.StatusServiceUnavailable
	}
	return "", 0
}

// leaveShare has the host h of a share gone, and the share forgotten
// unless it is back in time.
func (s *Server) leaveShare(sh *share, h *host) {
	s.mu.Lock()
	defer s.mu.Unlock()
	close(h.gone)
	if s.shares[sh.id] != sh || sh.host != h {
		return
	}
	back := make(chan struct{})
	sh.host, sh.back = nil, back
	sh.forget = time.AfterFunc(s.grace, func() {
		s.mu.Lock()
		// Not for an absence that ended just as this began.
		late := s.shares[sh.id] == sh && sh.back == back
		if late {
			s.dropLocked(sh)
			s.rememberLocked(sh.id, sh.token, sh.from, false)
		}
		s.mu.Unlock()
		if late {
			s.logf("relay: share %s was not back in time", shownID(sh.id))
		}
	})
}

// dropLocked has the relay know a share no more. s.mu is held.
func (s *Server) dropLocked(sh *share) {
	delete(s.shares, sh.id)
	close(sh.over)
	s.useLocked(sh, nil)
	if sh.forget != nil {
		sh.forget.Stop()
	}
	if sh.host != nil {
		sh.host.turnOut("the share ended")
	}
}

// endShare ends a share at its host's word: its link is dead from now
// on. Streams already joined are the host's to close.
func (s *Server) endShare(w http.ResponseWriter, r *http.Request) {
	id, token := r.URL.Query().Get("id"), hostToken(w, r)
	if token == "" {
		return
	}
	if !isShareID(id) {
		http.Error(w, "not the id of a share", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	sh := s.shares[id]
	was, past := s.pastLocked(id)
	if (sh != nil && !same(sh.token, token)) || (past && !was.is(token)) {
		s.mu.Unlock()
		http.Error(w, "that link is another kakel's", http.StatusForbidden)
		return
	}
	if sh != nil {
		s.dropLocked(sh)
	}
	// One the relay does not know too: it may be registering as this
	// comes.
	s.rememberLocked(id, token, clientAddr(r), true)
	s.mu.Unlock()
	if sh != nil {
		s.logf("relay: share %s ended", shownID(id))
	}
	w.WriteHeader(http.StatusNoContent)
}

// shared is the share id stands for and the web client its guests are
// sent, or nil.
func (s *Server) shared(id string) (*share, *keptBundle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sh := s.shares[id]
	if sh == nil {
		return nil, nil
	}
	return sh, sh.bundle
}

// settling reports whether the relay started so short a while ago that
// a share it does not know may be one whose host has yet to come back.
func (s *Server) settling() bool { return time.Now().Before(s.settled) }

// serveGuestJoin joins a guest to the host of the share id. Once the
// WebSocket is taken, why a guest is turned away is said by what it is
// closed with, as that is all a page is told.
func (s *Server) serveGuestJoin(w http.ResponseWriter, r *http.Request, id string) {
	if !upgrading(r) {
		http.Error(w, noUpgrade, http.StatusUpgradeRequired)
		return
	}
	r, err := s.fromOwnPage(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	defer func() { _ = ws.CloseNow() }()
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	if err := s.joinGuest(ctx, cancel, ws, id, clientAddr(r)); err != nil {
		// Before ctx ends, which cuts the stream off with nothing said.
		why := RefusedAway
		errors.As(err, &why)
		_ = ws.Close(websocket.StatusCode(why), why.reason())
	}
}

// joinGuest joins the guest at the other end of ws, whose stream cancel
// ends, to the host of the share id until either ends, or returns the
// Refused saying why not.
func (s *Server) joinGuest(ctx context.Context, cancel context.CancelFunc, ws *websocket.Conn, id, from string) error {
	sh, _ := s.shared(id)
	if sh == nil {
		// Only what is not there is counted and held to a limit. A guest
		// with a link that is good is let through, whatever others behind
		// the same address asked for, or a page elsewhere had its browser
		// ask for.
		switch {
		case s.settling() && isShareID(id):
			return RefusedAway
		case s.missing(s.lost, from, time.Now()):
			return RefusedTooMany
		}
		s.missed(s.lost, from, time.Now())
		return RefusedGone
	}
	near := newEarly(carried(ctx, ws))
	far, done, err := s.askForGuest(ctx, near, sh, from)
	if err != nil {
		return err
	}
	defer done()
	wctx, stop := context.WithTimeout(ctx, pingWait)
	err = ws.Write(wctx, websocket.MessageText, []byte(joinedWord))
	stop()
	if err != nil {
		_ = far.Close(websocket.StatusGoingAway, "the guest left")
		return nil
	}
	s.logf("relay: share %s joined from %s", shownID(id), from)
	join(cancel, near, carried(ctx, far))
	return nil
}

// askForGuest has the host of a share bring a stream for a guest,
// waiting a moment for one that is away, and no longer than the guest
// does.
func (s *Server) askForGuest(ctx context.Context, near *early, sh *share, from string) (far *websocket.Conn, done func(), err error) {
	s.mu.Lock()
	if sh.waiting >= mostGuests || sh.joined >= mostJoined {
		s.mu.Unlock()
		return nil, nil, RefusedBusy
	}
	sh.waiting++
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		sh.waiting--
		if err == nil {
			sh.joined++
		}
		s.mu.Unlock()
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		select {
		case <-near.gone:
		case <-sh.over:
		case <-ctx.Done():
		}
		cancel()
	}()
	// Why it is turned away, when it is.
	why := func() error {
		select {
		case <-sh.over:
			return RefusedGone
		default:
			return RefusedAway
		}
	}
	h := s.hostOf(ctx, sh)
	if h == nil {
		return nil, nil, why()
	}
	s.mu.Lock()
	may := s.mayJoinLocked(sh, addrKey(from), time.Now())
	s.mu.Unlock()
	if !may {
		return nil, nil, RefusedBusy
	}
	far, left, err := s.ask(ctx, h, from)
	if err == errBusy {
		return nil, nil, RefusedBusy
	} else if err != nil {
		return nil, nil, why()
	}
	return far, func() {
		s.mu.Lock()
		sh.joined--
		s.mu.Unlock()
		left()
	}, nil
}

// hostOf is the host of a share, waited for a moment while it is away,
// or nil.
func (s *Server) hostOf(ctx context.Context, sh *share) *host {
	ctx, cancel := context.WithTimeout(ctx, s.back)
	defer cancel()
	for {
		s.mu.Lock()
		h, back := sh.host, sh.back
		s.mu.Unlock()
		if h != nil {
			return h
		}
		select {
		case <-back:
		case <-ctx.Done():
			return nil
		}
	}
}

// early is a guest's stream, read from before it is joined, so a guest
// that has left is noticed while it waits.
type early struct {
	net.Conn
	// gone is closed if it ended before it was joined, or right after.
	gone chan struct{}
	// first is the first that came, and why there is no more of it.
	first chan []byte
	why   error
	rest  []byte
}

// newEarly begins to read from c.
func newEarly(c net.Conn) *early {
	e := &early{Conn: c, gone: make(chan struct{}), first: make(chan []byte, 1)}
	go func() {
		defer close(e.first)
		b := make([]byte, 4<<10)
		n, err := c.Read(b)
		if n > 0 {
			e.first <- b[:n]
		}
		if err != nil {
			e.why = err
			close(e.gone)
		}
	}()
	return e
}

func (e *early) Read(p []byte) (int, error) {
	if len(e.rest) == 0 {
		b, ok := <-e.first
		if !ok {
			if e.why != nil {
				return 0, e.why
			}
			return e.Conn.Read(p)
		}
		e.rest = b
	}
	n := copy(p, e.rest)
	e.rest = e.rest[n:]
	return n, nil
}

// fromOwnPage is r, with the host its browser asked for, when it comes
// from the relay's own pages or from no page at all. A page of another
// site's is turned away: its visitor's browser would join a share for
// it. A proxy on this machine that puts a host of its own in the request
// says which was asked for in X-Forwarded-Host, which no page can.
func (s *Server) fromOwnPage(r *http.Request) (*http.Request, error) {
	own := *r
	own.Host = askedHost(r)
	if origin := r.Header.Get("Origin"); origin != "" {
		u, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(u.Host, own.Host) {
			// Said in the log, a few times: to whoever set up a proxy, as the
			// guest is told nothing.
			if from, now := clientAddr(r), time.Now(); !s.missing(s.lost, from, now) {
				s.missed(s.lost, from, now)
				s.logf("relay: a guest at %s was turned away: its page is from %.100q and the relay was asked for as %.100q. A proxy in front has to pass the Host header on",
					from, origin, own.Host)
			}
			return nil, errors.New("a share is joined from its own page")
		}
	}
	return &own, nil
}

// askedHost is the host a request was sent to, by the word of a proxy on
// this machine as clientAddr has it.
func askedHost(r *http.Request) string {
	if fromThisMachine(r) {
		if last := lastOf(r, "X-Forwarded-Host"); last != "" {
			return last
		}
	}
	return r.Host
}
