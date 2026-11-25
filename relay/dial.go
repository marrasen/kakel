//go:build !js

package relay

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Dialer is how this machine reaches a relay.
type Dialer struct {
	// Proxy picks the HTTP proxy for a request, as http.Transport's
	// does. A nil one goes straight to the relay.
	Proxy func(*http.Request) (*url.URL, error)

	// Dial makes the connection to the relay, or to the proxy when
	// there is one: through a jump server, say. A nil one dials from
	// this machine.
	Dial func(ctx context.Context, network, addr string) (net.Conn, error)

	// tls is what checks the relay's certificate, nil for the system's
	// roots; a test's relay has a certificate of its own.
	tls *tls.Config
}

// canPing says a stream can be pinged from this end.
const canPing = true

// transport reaches the relay for one stream or one request: the
// connection is the WebSocket's once it is open, and nothing is pooled.
func (d Dialer) transport() *http.Transport {
	dial := d.Dial
	if dial == nil {
		nd := &net.Dialer{Timeout: dialWithin, KeepAlive: 30 * time.Second}
		dial = nd.DialContext
	}
	return &http.Transport{
		Proxy:               d.Proxy,
		DialContext:         dial,
		TLSHandshakeTimeout: dialWithin,
		TLSClientConfig:     d.tls,
		DisableKeepAlives:   true,
		// A relay turning a large upload away says so before it is sent.
		ExpectContinueTimeout: 2 * time.Second,
	}
}

// open opens a WebSocket to one of the relay's doors.
func (d Dialer) open(ctx context.Context, to string, header http.Header) (*websocket.Conn, error) {
	tr := d.transport()
	defer tr.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	ws, resp, err := websocket.Dial(ctx, to, &websocket.DialOptions{
		HTTPClient:      &http.Client{Transport: tr},
		HTTPHeader:      header,
		CompressionMode: websocket.CompressionDisabled,
	})
	if err != nil {
		if resp != nil && resp.StatusCode != http.StatusSwitchingProtocols {
			return nil, said(resp, err)
		}
		return nil, unsaid(err)
	}
	return ws, nil
}

// unsaid is why a request could not be made, without the URL it was
// made to: that holds the relay's secret, which is never shown.
func unsaid(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	why := ue.Err
	if strings.Contains(why.Error(), "Proxy Authentication Required") {
		return errors.New("the proxy wants a user name and password: give them in its URL, as http://user:password@proxy:8080")
	}
	return why
}

// NewToken makes the token a served window keeps with its code.
func NewToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// HostConfig is what serving through a relay needs.
type HostConfig struct {
	Relay URL
	// Code is what a window connecting gives, until SetCode, and Token
	// is what tells the relay this is the window the code is kept for.
	Code, Token string
	Dialer      Dialer
	// OnState is told each time the window's place on the relay
	// changes, under code: nil once it is registered, and why not
	// otherwise. It is called from the listener's goroutine, and may
	// arrive after SetCode for the code before, which is what code is
	// for.
	OnState func(code string, err error)
}

// tryAgainAfter is how long to wait before reaching the relay again,
// after each failure in a row.
var tryAgainAfter = []time.Duration{time.Second, 2 * time.Second, 5 * time.Second,
	10 * time.Second, 30 * time.Second, time.Minute}

// place is a place kept on a relay under a name: registered there, and
// registered again whenever the relay is lost, with the streams of
// whoever asks for it. A window served under a code has one, and a share
// has one.
type place struct {
	relay  URL
	token  string
	dialer Dialer
	// door is the URL to register at under a name.
	door func(name string) string
	// told is told how registering under a name went, as
	// HostConfig.OnState is, and may be nil.
	told func(name string, err error)
	// mend puts right what had registering refused with err, and returns
	// nil when it did: err when it cannot, or why it failed. It may be
	// nil.
	mend func(ctx context.Context, err error) error

	ctx    context.Context
	cancel context.CancelFunc
	conns  chan net.Conn
	once   sync.Once

	// mu guards name, and current, the registration held now, which
	// rename ends so the next one is made under the new name.
	mu      sync.Mutex
	name    string
	current context.CancelFunc
	// bringing holds a place for each stream being brought.
	bringing chan struct{}
	// waits are tryAgainAfter as it was when it started.
	waits []time.Duration
}

// start registers p, with its fields before ctx set, under name.
func (p *place) start(name string) {
	p.ctx, p.cancel = context.WithCancel(context.Background())
	p.conns = make(chan net.Conn)
	p.name = name
	p.bringing = make(chan struct{}, mostWaiting)
	p.waits = tryAgainAfter
	go p.stay()
}

// Accept waits for the next stream.
func (p *place) Accept() (net.Conn, error) {
	select {
	case c := <-p.conns:
		return c, nil
	case <-p.ctx.Done():
		return nil, net.ErrClosed
	}
}

// Close leaves the relay. Streams already accepted are left to whoever
// took them.
func (p *place) Close() error {
	p.once.Do(p.cancel)
	return nil
}

// Relay is the relay the place is on.
func (p *place) Relay() URL { return p.relay }

// named is the name the place is kept under.
func (p *place) named() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.name
}

// rename keeps the place under another name from now on.
func (p *place) rename(name string) {
	p.mu.Lock()
	p.name = name
	if p.current != nil {
		p.current()
	}
	p.mu.Unlock()
}

// state tells of the registration under name, when anybody is to be
// told.
func (p *place) state(name string, err error) {
	if p.told != nil && p.ctx.Err() == nil {
		p.told(name, err)
	}
}

// stay registers, and registers again each time the relay is lost,
// waiting longer after each failure in a row.
func (p *place) stay() {
	tries, mended := 0, false
	for p.ctx.Err() == nil {
		began := time.Now()
		name, err := p.registered()
		if p.ctx.Err() != nil {
			return
		}
		if errors.Is(err, errNewName) {
			// Registered again at once, under the new name.
			tries = 0
			continue
		}
		// Once for each failure: a relay that refuses still is waited for
		// as any other.
		if !mended && p.mend != nil {
			if err = p.mend(p.ctx, err); err == nil {
				mended = true
				continue
			}
		}
		mended = false
		// One that held for a while had the relay; losing it is a fresh
		// start, not one more failure in a row.
		if time.Since(began) > time.Minute {
			tries = 0
		}
		p.state(name, err)
		wait := p.waits[min(tries, len(p.waits)-1)]
		tries++
		select {
		case <-p.ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// registered registers once, and hears of whoever asks until the relay
// is lost, returning the name it registered and why it ended.
func (p *place) registered() (string, error) {
	ctx, cancel := context.WithCancel(p.ctx)
	defer cancel()
	p.mu.Lock()
	name := p.name
	p.current = cancel
	p.mu.Unlock()
	ws, err := p.dialer.open(ctx, p.door(name), http.Header{tokenHeader: {p.token}})
	if err != nil {
		if p.moved(name) {
			return name, errNewName
		}
		return name, reachErr(p.relay, err)
	}
	defer func() { _ = ws.CloseNow() }()
	ws.SetReadLimit(4 << 10)
	go keepUp(ctx, ws, nil)
	p.state(name, nil)
	for {
		kind, line, err := ws.Read(ctx)
		if err != nil {
			if p.ctx.Err() != nil {
				return name, nil
			}
			if p.moved(name) {
				return name, errNewName
			}
			if why := websocket.CloseStatus(err); why != -1 {
				var ce websocket.CloseError
				if errors.As(err, &ce) && ce.Reason != "" {
					return name, reachErr(p.relay, errors.New(ce.Reason))
				}
			}
			return name, reachErr(p.relay, errors.New("the connection to it was lost"))
		}
		var k knock
		if kind != websocket.MessageText || json.Unmarshal(line, &k) != nil || k.Ticket == "" {
			continue
		}
		// As many at once as the relay lets wait, and no more: past
		// that the relay is not one this window should trust.
		select {
		case p.bringing <- struct{}{}:
			go func() {
				defer func() { <-p.bringing }()
				p.bring(k)
			}()
		default:
		}
	}
}

// errNewName ends a registration under a name given up for another.
var errNewName = errors.New("relay: the name changed")

// moved reports whether the place is no longer kept under name.
func (p *place) moved(name string) bool { return p.named() != name }

// bring brings the stream for one that asked, and hands it to Accept.
func (p *place) bring(k knock) {
	header := http.Header{tokenHeader: {p.token}}
	ws, err := p.dialer.open(p.ctx, p.relay.to(doorAccept+"/"+url.PathEscape(k.Ticket)), header)
	if err != nil {
		return
	}
	from := k.From
	if from == "" {
		from = "somewhere"
	}
	c := stream(ws, linkAddr(from+" through "+p.relay.Host()))
	select {
	case p.conns <- c:
	case <-p.ctx.Done():
		_ = c.Close()
	}
}

// Listener is a window served through a relay: the streams of the
// windows connecting, one by one. It reaches the relay again by itself
// whenever it loses it.
type Listener struct{ *place }

// Listen registers with the relay and keeps registered until closed.
// It returns at once: how registering goes is told to OnState.
func Listen(cfg HostConfig) (*Listener, error) {
	switch {
	case cfg.Relay.IsZero():
		return nil, errors.New("no relay to register with")
	case cfg.Token == "":
		return nil, errors.New("no token to register with")
	}
	code, err := ParseCode(cfg.Code)
	if err != nil {
		return nil, err
	}
	l := &Listener{&place{relay: cfg.Relay, token: cfg.Token, dialer: cfg.Dialer, told: cfg.OnState,
		door: func(code string) string {
			return cfg.Relay.to(doorHost) + "?" + url.Values{"code": {code}}.Encode()
		}}}
	l.start(code)
	return l, nil
}

// Addr is the window's code on the relay, as it is shown: without the
// relay's secret.
func (l *Listener) Addr() net.Addr { return linkAddr(l.Link().Shown()) }

// Link is the window's link: the relay with its secret, and the code.
func (l *Listener) Link() Link { return Link{Relay: l.relay, Code: l.Code()} }

// Code is the code the window is served under.
func (l *Listener) Code() string { return l.named() }

// SetCode serves the window under another code from now on. The
// windows connected under the old one stay connected; a window
// connecting under it is no longer let through.
func (l *Listener) SetCode(code string) error {
	code, err := ParseCode(code)
	if err != nil {
		return err
	}
	l.rename(code)
	return nil
}
