//go:build !js

package relay

import (
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/marrasen/kakel/internal/build"
)

// tokenHeader carries a served window's token: kept with its code, so
// the relay can tell the same window coming back after its network
// dropped from another trying to take the code.
const tokenHeader = "Kakel-Token"

// Limits, so somebody given the relay's URL can tie up a little of it
// and no more. Everyone a link was ever sent to has the secret.
const (
	// mostHosts is how many windows may be registered at once, and
	// mostHostsFrom how many of them from one address.
	mostHosts     = 1000
	mostHostsFrom = 16
	// mostWaiting is how many windows may be waiting for one served
	// window to answer them.
	mostWaiting = 8
	// mostTickets is how many may be waiting on the relay as a whole.
	mostTickets = 512
	// mostMisses is how many codes one address may ask for that nobody
	// serves, in missWindow, before it is told to wait: a code is not
	// guessed by asking a few at a time.
	mostMisses = 30
	missWindow = time.Minute
	// mostMissing is how many addresses the relay counts misses for at
	// once; past that it forgets the counts that have run out.
	mostMissing = 10000
)

// answerWithin is how long a served window has to bring the stream for
// a window connecting. It is up and listening, so a few seconds is all
// it should take; this is for a slow proxy.
var answerWithin = 20 * time.Second

// knock is what the relay tells a served window about a window wanting
// it: the ticket to bring a stream with, and where it came from.
type knock struct {
	Ticket string `json:"knock"`
	From   string `json:"from,omitempty"`
}

// Server is a relay: an http.Handler joining served windows and the
// windows connecting to them, and shared windows and their guests.
type Server struct {
	secret string
	// Log is told what happened, a line at a time. A nil one is quiet.
	Log func(format string, args ...any)
	// AllowUnsigned takes a web client no release key has signed: for
	// trying one that is not released. Whoever has the relay's URL can
	// then put a page of their own on its domain.
	AllowUnsigned bool

	// within is answerWithin as it was when the relay was made, as grace
	// is graceFor and back is backWithin.
	within, grace, back time.Duration
	// key is whose signature a web client needs.
	key ed25519.PublicKey
	// settled is when a share the relay does not know is one it will not
	// hear of: the hosts of a relay that started again come back to it
	// first.
	settled time.Time
	// shareJoins and guestJoins are how often a share may be joined, and
	// how often from one address.
	shareJoins, guestJoins pace

	mu      sync.Mutex
	hosts   map[string]*host
	tickets map[string]*ticket
	// misses counts, by address, the codes asked for that nobody
	// serves, and lost the links no share has. They are counted apart,
	// as a code takes the relay's secret to ask for.
	misses, lost map[string]*misses
	// shares are the shares by id, and past those there were.
	shares map[string]*share
	past   map[string]pastShare
	// turn counts what is remembered and used, in the order it was, which
	// the clock may not tell apart.
	turn uint64
	// bundles are the web clients kept, by hash, and bundleBytes how
	// much that is.
	bundles     map[string]*keptBundle
	bundleBytes int
	// uploading are the addresses a web client is being uploaded from,
	// and uploaded is closed, and made again, as one of them is done.
	uploading map[string]bool
	uploaded  chan struct{}
}

// misses are the codes one address asked for that nobody serves, since
// from.
type misses struct {
	from  time.Time
	count int
}

// host is a served window, or the host of a shared one, registered on
// the relay.
type host struct {
	// name is what the log calls it.
	name, token string
	// from is the address it registered from.
	from string
	// knocks are the knocks waiting to be written to it, by the one
	// goroutine that writes them.
	knocks chan []byte
	// waiting counts the tickets made for it not yet answered, and most
	// is how many there may be.
	waiting, most int
	// gone is closed once it has left the relay, and out when the relay
	// is to let it go, for the reason why.
	gone, out chan struct{}
	why       string
}

// newHost is a host the relay is told of under name.
func newHost(name, token, from string, most int) *host {
	return &host{name: name, token: token, from: from, most: most, knocks: make(chan []byte, most),
		gone: make(chan struct{}), out: make(chan struct{})}
}

// turnOut has the relay let h go, once.
func (h *host) turnOut(why string) {
	h.why = why
	close(h.out)
}

// ticket is one window connecting, or one guest, waiting for the stream
// the host brings for it.
type ticket struct {
	host *host
	// brought is where the host's stream arrives.
	brought chan *websocket.Conn
	// done is closed once the two are no longer joined, for the
	// handler holding the host's stream.
	done chan struct{}
}

// NewServer makes a relay answering under secret. It takes the web
// clients kakel's release key has signed.
func NewServer(secret string) *Server {
	key, _ := base64.StdEncoding.DecodeString(build.ReleaseKey)
	return &Server{secret: secret, key: key,
		within: answerWithin, grace: graceFor, back: backWithin,
		shareJoins: pace{burst: 20, every: 2 * time.Second}, guestJoins: pace{burst: 10, every: 4 * time.Second},
		uploading: map[string]bool{}, uploaded: make(chan struct{}),
		hosts: map[string]*host{}, tickets: map[string]*ticket{},
		misses: map[string]*misses{}, lost: map[string]*misses{},
		shares: map[string]*share{}, past: map[string]pastShare{}, bundles: map[string]*keptBundle{}}
}

// logf says what happened, when anybody is listening.
func (s *Server) logf(format string, args ...any) {
	if s.Log != nil {
		s.Log(format, args...)
	}
}

// ServeHTTP answers a request on any of the relay's doors. The path
// may have anything before /k/ or /s/, for a relay behind a proxy that
// hands it a folder of its own.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	holdToTime(w, r)
	at := strings.LastIndex(r.URL.Path, secretPath)
	if sat := strings.LastIndex(r.URL.Path, sharePath); sat > at {
		s.serveGuest(w, r, r.URL.Path[sat+len(sharePath):])
		return
	}
	if at < 0 {
		http.NotFound(w, r)
		return
	}
	secret, door, _ := strings.Cut(r.URL.Path[at+len(secretPath):], "/")
	// The same answer as a path that is not there at all, so the relay
	// says nothing about itself to someone without the secret.
	if !same(secret, s.secret) {
		http.NotFound(w, r)
		return
	}
	door, arg, _ := strings.Cut(door, "/")
	switch door {
	case doorAbout:
		s.serveAbout(w, r)
		return
	case doorBundle:
		s.serveBundle(w, r, arg)
		return
	case doorShare:
		if r.Method == http.MethodDelete {
			s.endShare(w, r)
			return
		}
	case doorHost, doorJoin, doorAccept:
	default:
		http.NotFound(w, r)
		return
	}
	// Turned away before anything is done for it, as a proxy in front
	// that does not pass WebSockets on would have the served window
	// bring a stream for every try.
	if !upgrading(r) {
		http.Error(w, noUpgrade, http.StatusUpgradeRequired)
		return
	}
	switch door {
	case doorHost:
		s.serveHost(w, r)
	case doorShare:
		s.serveShare(w, r)
	case doorJoin:
		s.serveJoin(w, r, arg)
	case doorAccept:
		s.serveAccept(w, r, arg)
	}
}

// noUpgrade is said to a request for a WebSocket that is not one.
const noUpgrade = "the relay was reached without a WebSocket upgrade: a proxy in front of it has to pass the Upgrade and Connection headers on"

// bodyWithin and replyWithin are how long a request may take that is no
// upload, no download and no stream: what it sends, and its answer.
var (
	bodyWithin  = 10 * time.Second
	replyWithin = time.Minute
)

// holdToTime bounds a request that does not say how long it takes: a
// caller sending its body a byte at a time, or never reading its answer,
// does not hold a connection for as long as it likes. A WebSocket taken
// is bound by neither, and the doors that carry much set their own.
func holdToTime(w http.ResponseWriter, r *http.Request) {
	c := http.NewResponseController(w)
	if r.ContentLength != 0 {
		_ = c.SetReadDeadline(time.Now().Add(bodyWithin))
	}
	_ = c.SetWriteDeadline(time.Now().Add(replyWithin))
}

// same compares two secrets in a time that says nothing of where they
// differ.
func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// upgrading reports whether r asks for a WebSocket.
func upgrading(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, v := range r.Header.Values("Connection") {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), "upgrade") {
				return true
			}
		}
	}
	return false
}

// accept takes a WebSocket on a request: binary, uncompressed, as what
// it carries is encrypted and does not compress.
func accept(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return websocket.Accept(w, r, &websocket.AcceptOptions{CompressionMode: websocket.CompressionDisabled})
}

// hostToken is the token a request to register gives, or "" once it has
// been told it gave none.
func hostToken(w http.ResponseWriter, r *http.Request) string {
	token := r.Header.Get(tokenHeader)
	if len(token) < 16 {
		http.Error(w, "a kakel registering gives a token with its code", http.StatusBadRequest)
		return ""
	}
	return token
}

// serveHost registers a served window under its code, and tells it of
// each window connecting until it leaves.
func (s *Server) serveHost(w http.ResponseWriter, r *http.Request) {
	code, err := ParseCode(r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	token := hostToken(w, r)
	if token == "" {
		return
	}
	// Registered before the WebSocket is taken, so the window can be
	// joined the moment it hears it has been: a window connecting a
	// moment later finds it, and its knock waits until it is told.
	h := newHost(code, token, clientAddr(r), mostWaiting)
	s.mu.Lock()
	why, status := s.mayRegisterLocked(code, token, h.from)
	if why != "" {
		s.mu.Unlock()
		http.Error(w, why, status)
		return
	}
	old := s.hosts[code]
	s.hosts[code] = h
	if old != nil {
		// The same window, back before the relay noticed it had gone.
		old.turnOut(registeredAgain)
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.hosts[code] == h {
			delete(s.hosts, code)
		}
		close(h.gone)
		s.mu.Unlock()
	}()
	s.hold(w, r, h)
}

// registeredAgain is why a host is let go that came back before the
// relay noticed it had gone.
const registeredAgain = "the same kakel registered again"

// hold takes the WebSocket of a host registered, and tells it of
// whoever asks for it until it leaves or the relay lets it go.
func (s *Server) hold(w http.ResponseWriter, r *http.Request, h *host) {
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	defer func() { _ = ws.CloseNow() }()
	s.logf("relay: %s registered from %s", h.name, h.from)

	ctx := ws.CloseRead(r.Context())
	go keepUp(ctx, ws, nil)
	go s.tell(ctx, h, ws)
	select {
	case <-ctx.Done():
	case <-h.out:
		// Not waited for: a connection that went dead takes a while to
		// close, and the one that replaced it is served meanwhile.
		go func() { _ = ws.Close(websocket.StatusNormalClosure, h.why) }()
	}
	s.logf("relay: %s left", h.name)
}

// tell writes h's knocks to it, one at a time, until ctx ends. Writing
// them here rather than from the handler of each window connecting means
// one that gives up part way through a write cannot close the served
// window's connection under it.
func (s *Server) tell(ctx context.Context, h *host, ws *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case line := <-h.knocks:
			wctx, cancel := context.WithTimeout(ctx, pingWait)
			err := ws.Write(wctx, websocket.MessageText, line)
			cancel()
			if err != nil {
				_ = ws.CloseNow()
				return
			}
		}
	}
}

// mayRegister says why a window may not register code with token from
// the address from now, and the status saying so, or "".
func (s *Server) mayRegister(code, token, from string) (string, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mayRegisterLocked(code, token, from)
}

// mayRegisterLocked is mayRegister, with s.mu held.
func (s *Server) mayRegisterLocked(code, token, from string) (string, int) {
	old := s.hosts[code]
	if old != nil {
		if !same(old.token, token) {
			return "that code is in use by another kakel; make a new code", http.StatusConflict
		}
		// Taking its own place back takes no more room.
		return "", 0
	}
	if len(s.hosts) >= mostHosts {
		return "the relay has as many kakels as it takes", http.StatusServiceUnavailable
	}
	n, key := 0, addrKey(from)
	for _, h := range s.hosts {
		if addrKey(h.from) == key {
			n++
		}
	}
	if n >= mostHostsFrom {
		return fmt.Sprintf("the relay takes %d kakels from one address", mostHostsFrom), http.StatusServiceUnavailable
	}
	return "", 0
}

// missed counts in all a code asked for from the address from that
// nobody serves, or a link no share has.
func (s *Server) missed(all map[string]*misses, from string, now time.Time) {
	from = addrKey(from)
	s.mu.Lock()
	defer s.mu.Unlock()
	m := all[from]
	if m == nil || now.Sub(m.from) > missWindow {
		if len(all) >= mostMissing {
			for k, old := range all {
				if now.Sub(old.from) > missWindow {
					delete(all, k)
				}
			}
			if len(all) >= mostMissing {
				// Too many to count: this one goes uncounted rather than
				// the relay growing without end.
				return
			}
		}
		m = &misses{from: now}
		all[from] = m
	}
	m.count++
}

// missing reports whether the address from has asked for too much that
// is not there, as all counts it, to be answered now.
func (s *Server) missing(all map[string]*misses, from string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := all[addrKey(from)]
	return m != nil && now.Sub(m.from) <= missWindow && m.count >= mostMisses
}

// Why a host brought no stream for somebody.
var (
	errBusy   = errors.New("relay: as many as may are waiting")
	errLeft   = errors.New("relay: the host left")
	errSilent = errors.New("relay: the host did not answer")
)

// ask has h bring a stream for somebody at the address from, or says
// why it did not. The stream is done with once done is called, which is
// nil with an error.
func (s *Server) ask(ctx context.Context, h *host, from string) (far *websocket.Conn, done func(), err error) {
	id := NewSecret()
	t := &ticket{host: h, brought: make(chan *websocket.Conn, 1), done: make(chan struct{})}
	s.mu.Lock()
	if h.waiting >= h.most || len(s.tickets) >= mostTickets {
		s.mu.Unlock()
		return nil, nil, errBusy
	}
	h.waiting++
	s.tickets[id] = t
	s.mu.Unlock()
	// Taken back however this ends: a stream brought late finds no
	// ticket and is turned away.
	defer func() {
		s.mu.Lock()
		delete(s.tickets, id)
		h.waiting--
		s.mu.Unlock()
		if err != nil {
			close(t.done)
		}
	}()

	line, _ := json.Marshal(knock{Ticket: id, From: from})
	// Room for it: no more knocks wait than tickets do, and those are
	// held to most.
	select {
	case h.knocks <- line:
	default:
		return nil, nil, errBusy
	}
	wctx, cancel := context.WithTimeout(ctx, s.within)
	defer cancel()
	select {
	case far = <-t.brought:
		return far, func() { close(t.done) }, nil
	case <-h.gone:
	case <-h.out:
	case <-wctx.Done():
		return nil, nil, errSilent
	}
	return nil, nil, errLeft
}

// serveJoin joins a window connecting to the served window registered
// under code, once that window has brought a stream for it.
func (s *Server) serveJoin(w http.ResponseWriter, r *http.Request, code string) {
	from := clientAddr(r)
	if s.missing(s.misses, from, time.Now()) {
		http.Error(w, "too many codes not on this relay were asked for from here; wait a minute", http.StatusTooManyRequests)
		return
	}
	code, err := ParseCode(code)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	h := s.hosts[code]
	s.mu.Unlock()
	if h == nil {
		s.missed(s.misses, from, time.Now())
		http.Error(w, "no kakel has code "+code+" on this relay", http.StatusNotFound)
		return
	}
	far, done, err := s.ask(r.Context(), h, from)
	switch err {
	case nil:
	case errBusy:
		http.Error(w, "too many kakels are connecting to "+code+" at once; try again", http.StatusServiceUnavailable)
		return
	case errLeft:
		http.Error(w, "the kakel with code "+code+" left the relay", http.StatusBadGateway)
		return
	default:
		http.Error(w, "the kakel with code "+code+" did not answer", http.StatusGatewayTimeout)
		return
	}
	defer done()
	near, err := accept(w, r)
	if err != nil {
		_ = far.Close(websocket.StatusGoingAway, "the kakel connecting left")
		return
	}
	s.logf("relay: %s joined from %s", code, from)
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	join(cancel, carried(ctx, near), carried(ctx, far))
}

// serveAccept takes the stream a host brings for a ticket, and holds it
// while it is joined.
func (s *Server) serveAccept(w http.ResponseWriter, r *http.Request, id string) {
	token := r.Header.Get(tokenHeader)
	s.mu.Lock()
	t := s.tickets[id]
	if t != nil && !same(token, t.host.token) {
		// Not the host it was made for: left for that one.
		t = nil
	}
	if t != nil {
		// One stream a ticket.
		delete(s.tickets, id)
	}
	s.mu.Unlock()
	if t == nil {
		http.Error(w, "no kakel is waiting for that", http.StatusNotFound)
		return
	}
	ws, err := accept(w, r)
	if err != nil {
		return
	}
	t.brought <- ws
	<-t.done
	// Closed here too, for one brought just as whoever it was for gave up
	// waiting, which nothing joined.
	_ = ws.CloseNow()
}

// join carries one stream into the other until either ends, and ends
// both with cancel.
func join(cancel context.CancelFunc, a, b net.Conn) {
	var wg sync.WaitGroup
	carry := func(to, from net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(to, from)
		cancel()
	}
	wg.Add(2)
	go carry(a, b)
	go carry(b, a)
	wg.Wait()
	_ = a.Close()
	_ = b.Close()
}

// clientAddr is where a request came from: for a proxy on this machine,
// the address it says it was asked from, the last in X-Forwarded-For,
// and the address that connected otherwise. Only the last is the
// proxy's own word; the rest are whatever the caller sent.
func clientAddr(r *http.Request) string {
	if fromThisMachine(r) {
		if last := lastOf(r, "X-Forwarded-For"); last != "" {
			return last
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// addrKey is what an address is counted under where there is a limit to
// an address: the address itself, and for IPv6 its first 64 bits, as
// one machine has every address that begins with them.
func addrKey(addr string) string {
	ip, err := netip.ParseAddr(addr)
	if err != nil {
		// As some proxies write it, with a port.
		ap, perr := netip.ParseAddrPort(addr)
		if perr != nil {
			return addr
		}
		ip = ap.Addr()
	}
	ip = ip.Unmap().WithZone("")
	if ip.Is4() {
		return ip.String()
	}
	net64, err := ip.Prefix(64)
	if err != nil {
		return addr
	}
	return net64.String()
}

// fromThisMachine reports whether a request was connected from this
// machine, as a proxy on it does.
func fromThisMachine(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// lastOf is the last of a header's values, each a list with commas, or
// "".
func lastOf(r *http.Request, header string) string {
	all := r.Header.Values(header)
	if len(all) == 0 {
		return ""
	}
	parts := strings.Split(all[len(all)-1], ",")
	return strings.TrimSpace(parts[len(parts)-1])
}

// answered is a relay's answer that turned a request away: its status,
// and why in words.
type answered struct {
	status int
	why    string
}

func (a answered) Error() string { return a.why }

// Is reports whether target is the error this answer stands for.
func (a answered) Is(target error) bool {
	return target == ErrNoBundle && a.status == http.StatusFailedDependency
}

// said turns a relay's refusal into a sentence: the text it answered
// with, or the status when it said nothing a person can read.
func said(resp *http.Response, err error) error {
	if resp == nil {
		return err
	}
	var text string
	if resp.Body != nil {
		kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if kind == "text/plain" {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			text = strings.TrimSpace(string(b))
		}
	}
	switch {
	case resp.StatusCode == http.StatusNotFound && (text == "404 page not found" || text == ""):
		text = "the relay does not know that URL: check its secret, the part after /k/"
	case text == "":
		text = "the relay answered " + resp.Status
	}
	return answered{resp.StatusCode, text}
}
