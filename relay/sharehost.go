//go:build !js

package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ErrOldRelay says a relay is there and is too old to share a window
// with a link.
var ErrOldRelay = errors.New("the relay cannot share windows")

// ErrNoBundle says a relay does not have the web client a share is to
// be opened with: PutBundle gives it.
var ErrNoBundle = errors.New("the relay does not have the web client")

// Client is a relay as the host of a shared window uses it.
type Client struct {
	Relay  URL
	Dialer Dialer
}

// About is what the relay can do. The error is ErrOldRelay, as errors.Is
// finds it, for a relay from before windows were shared with links.
func (c Client) About(ctx context.Context) (About, error) {
	ctx, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	resp, body, err := c.do(ctx, http.MethodGet, c.Relay.to(doorAbout), nil, nil)
	if err != nil {
		return About{}, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// The answer to a wrong secret too, which the door every relay
		// has tells apart: with the right one it wants a WebSocket.
		host, _, err := c.do(ctx, http.MethodGet, c.Relay.to(doorHost), nil, nil)
		if err == nil && host.StatusCode != http.StatusNotFound {
			return About{}, reachErr(c.Relay, ErrOldRelay)
		}
		return About{}, reachErr(c.Relay, saidIn(resp, body))
	default:
		return About{}, reachErr(c.Relay, saidIn(resp, body))
	}
	var a About
	if err := json.Unmarshal(body, &a); err != nil {
		return About{}, reachErr(c.Relay, errors.New("it did not answer as a relay"))
	}
	if a.Shares < sharesVersion {
		return a, reachErr(c.Relay, ErrOldRelay)
	}
	return a, nil
}

// HasBundle reports whether the relay has the web client hash.
func (c Client) HasBundle(ctx context.Context, hash string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	resp, body, err := c.do(ctx, http.MethodHead, c.Relay.to(doorBundle+"/"+hash), nil, nil)
	switch {
	case err != nil:
		return false, err
	case resp.StatusCode == http.StatusOK:
		return true, nil
	case resp.StatusCode == http.StatusNotFound:
		return false, nil
	}
	return false, reachErr(c.Relay, saidIn(resp, body))
}

// PutBundle gives the relay a web client, with the signature of its
// manifest as gunimsign writes it, or "" for a relay that takes one
// without.
func (c Client) PutBundle(ctx context.Context, zipped []byte, sig string) error {
	ctx, cancel := context.WithTimeout(ctx, uploadWithin+uploadTurn)
	defer cancel()
	// Nothing is sent before the relay says it takes it.
	header := http.Header{"Expect": {"100-continue"}}
	if sig = strings.TrimSpace(sig); sig != "" {
		header.Set(signatureHeader, sig)
	}
	resp, body, err := c.do(ctx, http.MethodPut, c.Relay.to(doorBundle+"/"+BundleHash(zipped)), header, zipped)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return reachErr(c.Relay, saidIn(resp, body))
	}
	return nil
}

// do makes a request on the relay's door at to, and reads its answer.
// No error names the URL, which holds the relay's secret, and no
// redirect is followed, which would send it elsewhere.
func (c Client) do(ctx context.Context, method, to string, header http.Header, body []byte) (*http.Response, []byte, error) {
	if c.Relay.IsZero() {
		return nil, nil, errors.New("no relay to reach")
	}
	tr := c.Dialer.transport()
	defer tr.CloseIdleConnections()
	var from io.Reader
	if body != nil {
		from = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, to, from)
	if err != nil {
		return nil, nil, reachErr(c.Relay, unsaid(err))
	}
	if header != nil {
		req.Header = header
	}
	client := http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, reachErr(c.Relay, unsaid(err))
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, nil, reachErr(c.Relay, unsaid(err))
	}
	return resp, answer, nil
}

// saidIn is said, for an answer read already.
func saidIn(resp *http.Response, body []byte) error {
	again := *resp
	again.Body = io.NopCloser(bytes.NewReader(body))
	return said(&again, nil)
}

// ShareConfig is what sharing a window through a relay needs.
type ShareConfig struct {
	Relay URL
	// ID is the share's id, as NewShareID makes it, and Token is what
	// tells the relay this is the window the id is kept for.
	ID, Token string
	// Bundle is the hash of the web client its guests are sent.
	Bundle string
	Dialer Dialer
	// Upload gives the relay the web client, when it turns out not to
	// have it: it may have started over. A nil one leaves that to
	// whoever OnState tells, as ErrNoBundle.
	Upload func(context.Context) error
	// OnState is told each time the share's place on the relay changes:
	// nil once it is registered, and why not otherwise. It is called
	// from the listener's goroutine.
	OnState func(error)
}

// ShareListener is a window shared through a relay: the streams of its
// guests, one by one. It reaches the relay again by itself whenever it
// loses it. Its streams' RemoteAddr begins with the guest's address.
type ShareListener struct{ *place }

// ListenShare registers a share with the relay and keeps it registered
// until closed. It returns at once: how registering goes is told to
// OnState.
func ListenShare(cfg ShareConfig) (*ShareListener, error) {
	switch {
	case cfg.Relay.IsZero():
		return nil, errors.New("no relay to register with")
	case cfg.Token == "":
		return nil, errors.New("no token to register with")
	case !isShareID(cfg.ID):
		return nil, errors.New("not the id of a share")
	case !isHash(cfg.Bundle):
		return nil, errors.New("not the hash of a web client")
	}
	l := &ShareListener{&place{relay: cfg.Relay, token: cfg.Token, dialer: cfg.Dialer,
		door: func(id string) string {
			return cfg.Relay.to(doorShare) + "?" + url.Values{"id": {id}, "bundle": {cfg.Bundle}}.Encode()
		}}}
	if cfg.OnState != nil {
		l.told = func(_ string, err error) { cfg.OnState(err) }
	}
	if cfg.Upload != nil {
		l.mend = func(ctx context.Context, err error) error {
			if !errors.Is(err, ErrNoBundle) {
				return err
			}
			return cfg.Upload(ctx)
		}
	}
	l.start(cfg.ID)
	return l, nil
}

// ID is the share's id.
func (l *ShareListener) ID() string { return l.named() }

// Addr is the share as it is shown: the start of its id and the relay's
// machine.
func (l *ShareListener) Addr() net.Addr {
	return linkAddr(fmt.Sprintf("%s on %s", shownID(l.ID()), l.relay.Host()))
}

// End closes the listener and has the relay know the share no more: its
// link is dead from now on, where a listener that is only closed may
// come back to it for a while. Streams already accepted are left to
// whoever took them.
func (l *ShareListener) End(ctx context.Context) error {
	_ = l.Close()
	ctx, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	c := Client{Relay: l.relay, Dialer: l.dialer}
	resp, body, err := c.do(ctx, http.MethodDelete, l.relay.to(doorShare)+"?"+url.Values{"id": {l.ID()}}.Encode(),
		http.Header{tokenHeader: {l.token}}, nil)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusNoContent {
		return reachErr(l.relay, saidIn(resp, body))
	}
	return nil
}

var _ net.Listener = (*ShareListener)(nil)
