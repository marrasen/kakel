//go:build !js

package relay

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// sharing is a relay that takes the web clients key has signed, its
// URL, and the hash of one it has.
func sharing(t *testing.T) (*Server, URL, string) {
	t.Helper()
	s, u := started(t)
	b := kept(t, s)
	return s, u, b
}

// kept has s keep a web client, and returns its hash.
func kept(t *testing.T, s *Server) string {
	t.Helper()
	zipped, _ := made(t, clientFiles(t))
	b, err := OpenBundle(zipped)
	if err != nil {
		t.Fatal(err)
	}
	if !s.keepBundle(b, "192.0.2.99") {
		t.Fatal("there was no room for the web client")
	}
	return b.Hash
}

// quick has the relays and listeners made in a test wait less.
func quick(t *testing.T, grace, back time.Duration) {
	t.Helper()
	g, b, again := graceFor, backWithin, tryAgainAfter
	graceFor, backWithin, tryAgainAfter = grace, back, []time.Duration{20 * time.Millisecond}
	t.Cleanup(func() { graceFor, backWithin, tryAgainAfter = g, b, again })
}

// sharedAs shares through u under id, and waits until it is registered.
func sharedAs(t *testing.T, u URL, id, token, bundle string) (*ShareListener, *states) {
	t.Helper()
	st := newStates()
	l, err := ListenShare(ShareConfig{Relay: u, ID: id, Token: token, Bundle: bundle,
		OnState: func(err error) { st.tell("", err) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	if err := st.next(t); err != nil {
		t.Fatalf("registering: %v", err)
	}
	return l, st
}

// linkTo is the link to id on u.
func linkTo(t *testing.T, u URL, id string) ShareLink {
	t.Helper()
	l, err := NewShareLink(u, id, "theguestssecret")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// echoing sends each of l's guests back what it sends.
func echoing(l net.Listener) {
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				_, _ = io.Copy(c, c)
				_ = c.Close()
			}()
		}
	}()
}

// says checks that what is sent on c comes back.
func says(t *testing.T, c net.Conn) {
	t.Helper()
	go func() { _, _ = c.Write([]byte("hello")) }()
	got := make([]byte, 5)
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != "hello" {
		t.Fatalf("read %q, %v", got, err)
	}
	_ = c.SetReadDeadline(time.Time{})
}

// until waits for ok.
func until(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); !ok(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatal(what)
		}
	}
}

// away waits until s has share id with no host.
func away(t *testing.T, s *Server, id string) {
	t.Helper()
	until(t, "the share still has its host", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shares[id] != nil && s.shares[id].host == nil
	})
}

func TestAGuestReachesTheHostOfItsShare(t *testing.T) {
	_, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	near, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = near.Close() }()
	far := <-accepted
	defer func() { _ = far.Close() }()
	if !strings.HasPrefix(far.RemoteAddr().String(), "127.0.0.1 ") {
		t.Errorf("the host is told the guest came from %q", far.RemoteAddr())
	}
	go func() { _, _ = io.Copy(far, far) }()
	says(t, near)
	_ = far.Close()
	if _, err := near.Read(make([]byte, 1)); err == nil {
		t.Fatal("the guest read on after the host closed")
	}
}

func TestWhatAGuestSendsBeforeItIsJoinedIsNotLost(t *testing.T) {
	s, u, bundle := sharing(t)
	id, token := NewShareID(), NewToken()
	l, _ := sharedAs(t, u, id, token, bundle)
	_ = l.Close()
	away(t, s, id)
	ws, err := Dialer{}.open(context.Background(), linkTo(t, u, id).join(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.CloseNow() }()
	ctx := t.Context()
	for _, part := range []string{"one ", "two ", "three"} {
		if err := ws.Write(ctx, websocket.MessageBinary, []byte(part)); err != nil {
			t.Fatal(err)
		}
	}
	back, _ := sharedAs(t, u, id, token, bundle)
	far, err := back.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = far.Close() }()
	got := make([]byte, len("one two three"))
	if _, err := io.ReadFull(far, got); err != nil || string(got) != "one two three" {
		t.Fatalf("the host read %q, %v", got, err)
	}
}

func TestALinkNoShareHasIsGone(t *testing.T) {
	_, u, _ := sharing(t)
	_, err := JoinShare(context.Background(), linkTo(t, u, NewShareID()), Dialer{})
	if !errors.Is(err, RefusedGone) {
		t.Fatalf("got %v", err)
	}
	var why Refused
	if !errors.As(err, &why) || why.Retry() {
		t.Fatalf("a link that is gone is one to try again: %v", err)
	}
}

func TestAHostAwayIsWaitedForAndThenSaidToBe(t *testing.T) {
	quick(t, time.Minute, 300*time.Millisecond)
	s, u, bundle := sharing(t)
	id, token := NewShareID(), NewToken()
	l, _ := sharedAs(t, u, id, token, bundle)
	_ = l.Close()
	away(t, s, id)
	_, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	var why Refused
	if !errors.As(err, &why) || why != RefusedAway || !why.Retry() {
		t.Fatalf("got %v", err)
	}

	// Its page is there all the while, and one that comes back meanwhile
	// takes the guest that waits.
	if resp := get(t, pageOf(u, id)); resp.StatusCode != http.StatusOK {
		t.Fatalf("the page of a share whose host is away: %s", resp.Status)
	}
	s.back = 10 * time.Second
	joined := make(chan error, 1)
	go func() {
		c, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
		if err == nil {
			_ = c.Close()
		}
		joined <- err
	}()
	until(t, "no guest waits", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shares[id].waiting == 1
	})
	back, _ := sharedAs(t, u, id, token, bundle)
	echoing(back)
	if err := <-joined; err != nil {
		t.Fatalf("the host came back, and %v", err)
	}
}

func TestAShareWhoseHostStaysAwayIsForgotten(t *testing.T) {
	quick(t, 50*time.Millisecond, 10*time.Second)
	s, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	_ = l.Close()
	until(t, "the share is still there", func() bool { sh, _ := s.shared(id); return sh == nil })
	if _, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{}); !errors.Is(err, RefusedGone) {
		t.Fatalf("got %v", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.bundles[bundle].shares; n != 0 {
		t.Errorf("its web client is still in use by %d shares", n)
	}
}

func TestAShareThatCameBackIsNotForgotten(t *testing.T) {
	quick(t, 150*time.Millisecond, time.Second)
	s, u, bundle := sharing(t)
	id, token := NewShareID(), NewToken()
	for range 3 {
		l, _ := sharedAs(t, u, id, token, bundle)
		_ = l.Close()
		away(t, s, id)
	}
	sharedAs(t, u, id, token, bundle)
	time.Sleep(300 * time.Millisecond)
	if sh, _ := s.shared(id); sh == nil {
		t.Fatal("it was forgotten after it came back")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.bundles[bundle].shares; n != 1 {
		t.Errorf("its web client is in use by %d shares", n)
	}
}

func TestTheSameHostComingBackTakesItsShareBack(t *testing.T) {
	quick(t, time.Minute, time.Second)
	_, u, bundle := sharing(t)
	id, token := NewShareID(), NewToken()
	first, _ := sharedAs(t, u, id, token, bundle)
	second, _ := sharedAs(t, u, id, token, bundle)
	_ = first.Close()
	echoing(second)
	c, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	says(t, c)
}

func TestAShareHeldByAnotherIsRefused(t *testing.T) {
	quick(t, time.Minute, time.Second)
	s, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	// While its host is there, and while it is away.
	for range 2 {
		st := newStates()
		other, err := ListenShare(ShareConfig{Relay: u, ID: id, Token: NewToken(), Bundle: bundle,
			OnState: func(err error) { st.tell("", err) }})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.next(t); err == nil || !strings.Contains(err.Error(), "in use by another kakel") {
			t.Fatalf("got %v", err)
		}
		if err := other.End(context.Background()); err == nil {
			t.Fatal("another ended it")
		}
		_ = l.Close()
		away(t, s, id)
	}
}

func TestAnEndedShareIsGoneAtOnce(t *testing.T) {
	quick(t, time.Minute, 10*time.Second)
	s, u, bundle := sharing(t)
	id, token := NewShareID(), NewToken()
	l, _ := sharedAs(t, u, id, token, bundle)
	// A guest the host has not answered is told too.
	waits := make(chan error, 1)
	_ = l.Close()
	away(t, s, id)
	ws, err := Dialer{}.open(context.Background(), u.to(doorShare)+"?id="+id+"&bundle="+bundle, http.Header{tokenHeader: {token}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.CloseNow() }()
	go func() {
		_, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
		waits <- err
	}()
	until(t, "no guest waits", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		sh := s.shares[id]
		return sh != nil && sh.waiting == 1
	})
	if err := l.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-waits; !errors.Is(err, RefusedGone) {
		t.Fatalf("the guest that waited got %v", err)
	}
	if resp := get(t, pageOf(u, id)); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("the page of a share that ended: %s", resp.Status)
	}
	if _, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{}); !errors.Is(err, RefusedGone) {
		t.Fatalf("got %v", err)
	}
	// It ended for good, though its host tries again: it may have been on
	// its way.
	st := newStates()
	again, err := ListenShare(ShareConfig{Relay: u, ID: id, Token: token, Bundle: bundle,
		OnState: func(err error) { st.tell("", err) }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = again.Close() }()
	if err := st.next(t); err == nil || !strings.Contains(err.Error(), "has ended") {
		t.Fatalf("got %v", err)
	}
	// Ending it again is no error.
	if err := l.End(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAShareNeedsAWebClientTheRelayHas(t *testing.T) {
	quick(t, time.Minute, time.Second)
	s, u := started(t)
	s.AllowUnsigned = true
	zipped, _ := made(t, clientFiles(t))
	st := newStates()
	cfg := ShareConfig{Relay: u, ID: NewShareID(), Token: NewToken(), Bundle: BundleHash(zipped),
		OnState: func(err error) { st.tell("", err) }}
	l, err := ListenShare(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.next(t); !errors.Is(err, ErrNoBundle) {
		t.Fatalf("got %v", err)
	}
	_ = l.Close()

	// With a way to give it, it is given, and the share is registered
	// with no failure told of.
	c := Client{Relay: u}
	cfg.Upload = func(ctx context.Context) error { return c.PutBundle(ctx, zipped, "") }
	st = newStates()
	if l, err = ListenShare(cfg); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := st.next(t); err != nil {
		t.Fatal(err)
	}
}

func TestAnUploadThatFailsIsToldOf(t *testing.T) {
	quick(t, time.Minute, time.Second)
	_, u := started(t)
	zipped, _ := made(t, clientFiles(t))
	st := newStates()
	c := Client{Relay: u}
	l, err := ListenShare(ShareConfig{Relay: u, ID: NewShareID(), Token: NewToken(), Bundle: BundleHash(zipped),
		Upload:  func(ctx context.Context) error { return c.PutBundle(ctx, zipped, "") },
		OnState: func(err error) { st.tell("", err) }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	if err := st.next(t); err == nil || !strings.Contains(err.Error(), releasedOnly) {
		t.Fatalf("got %v", err)
	}
}

func TestAShareWaitsForAFewGuestsAndNoMore(t *testing.T) {
	s, u, bundle := sharing(t)
	id, token := NewShareID(), NewToken()
	l, _ := sharedAs(t, u, id, token, bundle)
	_ = l.Close()
	away(t, s, id)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for range mostGuests {
		wg.Go(func() { _, _ = JoinShare(ctx, linkTo(t, u, id), Dialer{}) })
	}
	waiting := func(n int) func() bool {
		return func() bool {
			s.mu.Lock()
			defer s.mu.Unlock()
			return s.shares[id].waiting == n
		}
	}
	until(t, "the guests do not wait", waiting(mostGuests))
	if _, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{}); !errors.Is(err, RefusedBusy) {
		t.Fatalf("got %v", err)
	}
	// Those that leave make room at once, though the host never came.
	cancel()
	wg.Wait()
	until(t, "the guests that left wait still", waiting(0))
}

func TestAShareHasAFewStreamsAndNoMore(t *testing.T) {
	s, u, bundle := sharing(t)
	s.guestJoins.burst, s.shareJoins.burst = 100, 100
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	echoing(l)
	var conns []net.Conn
	for range mostJoined {
		c, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		conns = append(conns, c)
	}
	if _, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{}); !errors.Is(err, RefusedBusy) {
		t.Fatalf("got %v", err)
	}
	_ = conns[0].Close()
	until(t, "the stream that closed is still counted", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shares[id].joined == mostJoined-1
	})
	c, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

func TestManyWindowsMayBeConnectedAtOnce(t *testing.T) {
	_, u := started(t)
	code := NewCode()
	l, _ := hosted(t, u, code, NewToken())
	echoing(l)
	for range mostWaiting + 2 {
		c, err := Join(context.Background(), Link{Relay: u, Code: code}, Dialer{})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = c.Close() }()
		says(t, c)
	}
}

func TestGuessingLinksIsSlowedDown(t *testing.T) {
	_, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	echoing(l)
	for i := range mostMisses {
		guess := NewShareID()
		if i%2 == 1 {
			guess = "nonsense"
		}
		if resp := get(t, pageOf(u, guess)); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("a guess: %s", resp.Status)
		}
	}
	if resp := get(t, pageOf(u, NewShareID())); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("one more: %s", resp.Status)
	}
	if _, err := JoinShare(context.Background(), linkTo(t, u, NewShareID()), Dialer{}); !errors.Is(err, RefusedTooMany) {
		t.Fatalf("got %v", err)
	}
	// A guest with a good link is let through all the same: it was others
	// behind its address, or a page elsewhere with the pictures it has.
	if resp := get(t, pageOf(u, id)); resp.StatusCode != http.StatusOK {
		t.Errorf("a good link's page: %s", resp.Status)
	}
	good, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	if err != nil {
		t.Fatalf("a good link: %v", err)
	}
	_ = good.Close()
	// Codes are counted apart, so a stranger cannot have a window
	// connecting turned away.
	code := NewCode()
	served, _ := hosted(t, u, code, NewToken())
	echoing(served)
	c, err := Join(context.Background(), Link{Relay: u, Code: code}, Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

// joinedFrom joins a guest at the address from, as a proxy on this
// machine says it, and reports whether it was let through.
func joinedFrom(t *testing.T, u URL, id, from string) bool {
	t.Helper()
	ws, err := Dialer{}.open(context.Background(), linkTo(t, u, id).join(), http.Header{"X-Forwarded-For": {from}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.CloseNow() }()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, word, err := ws.Read(ctx)
	if err != nil && refusal(int(websocket.CloseStatus(err))) != RefusedBusy {
		t.Fatalf("from %s: %v", from, err)
	}
	return string(word) == joinedWord
}

func TestNobodyFloodsAShareWithStreams(t *testing.T) {
	s, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	var mu sync.Mutex
	brought := 0
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			brought++
			mu.Unlock()
			_ = c.Close()
		}
	}()
	through := func(from func(i int) string, tries int) (n int) {
		for i := range tries {
			if joinedFrom(t, u, id, from(i)) {
				n++
			}
		}
		return n
	}
	// One address, all of one network at that, over and over. A few of
	// the same guest's reloads fit.
	began := time.Now()
	n := through(func(i int) string { return fmt.Sprintf("2001:db8::%x", i+1) }, 60)
	slack := 1 + int(time.Since(began)/s.guestJoins.every)
	if n < s.guestJoins.burst || n > s.guestJoins.burst+slack {
		t.Errorf("one address was joined %d times", n)
	}
	// Another has its own.
	if !joinedFrom(t, u, id, "192.0.2.1") {
		t.Error("another address was turned away")
	}
	// And all of them together have no more than the share does.
	n += 1 + through(func(i int) string { return fmt.Sprintf("198.51.100.%d", i+1) }, 60)
	slack = 1 + int(time.Since(began)/s.shareJoins.every)
	if n < s.shareJoins.burst || n > s.shareJoins.burst+slack {
		t.Errorf("the share was joined %d times", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if brought > n {
		t.Errorf("its host brought %d streams for %d guests", brought, n)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if kept := len(s.shares[id].pacedFrom); kept > s.shareJoins.burst+slack {
		t.Errorf("%d addresses are kept count of", kept)
	}
}

func TestAPaceIsABurstAndThenOneAtATime(t *testing.T) {
	p := pace{burst: 3, every: time.Second}
	now := time.Now()
	var at time.Time
	try := func(after time.Duration) bool {
		next, ok := p.ok(at, now.Add(after))
		if ok {
			at = next
		}
		return ok
	}
	for i, c := range []struct {
		after time.Duration
		ok    bool
	}{{0, true}, {0, true}, {0, true}, {0, false}, {900 * time.Millisecond, false}, {time.Second, true}, {time.Second, false},
		{2500 * time.Millisecond, true}, {2500 * time.Millisecond, false}, {time.Hour, true}, {time.Hour, true}, {time.Hour, true}, {time.Hour, false}} {
		if got := try(c.after); got != c.ok {
			t.Errorf("%d, after %v: %v", i, c.after, got)
		}
	}
}

func TestAnAddressIsCountedWithTheNetworkItHas(t *testing.T) {
	for _, same := range [][2]string{
		{"2001:db8:1:2::1", "2001:db8:1:2:ffff:ffff:ffff:ffff"}, {"192.0.2.1", "::ffff:192.0.2.1"},
		{"192.0.2.1", "192.0.2.1:8080"}, {"fe80::1%eth0", "fe80::2%eth1"}, {"[2001:db8::1]:80", "2001:db8::2"},
	} {
		if addrKey(same[0]) != addrKey(same[1]) {
			t.Errorf("%s and %s are counted apart", same[0], same[1])
		}
	}
	for _, apart := range [][2]string{
		{"2001:db8:1:2::1", "2001:db8:1:3::1"}, {"192.0.2.1", "192.0.2.2"}, {"somewhere", "elsewhere"},
	} {
		if addrKey(apart[0]) == addrKey(apart[1]) {
			t.Errorf("%s and %s are counted as one", apart[0], apart[1])
		}
	}
	// In each of the limits.
	s := NewServer(NewSecret())
	bundle := kept(t, s)
	at := func(i int) string { return fmt.Sprintf("2001:db8::%x", i+1) }
	for i := range mostHostsFrom {
		s.hosts[NewCode()] = &host{from: at(i)}
	}
	if why, _ := s.mayRegister(NewCode(), NewToken(), at(1000)); why == "" {
		t.Error("one network took one place more than it may")
	}
	for i := range mostSharesFrom {
		s.shares[NewShareID()] = &share{from: at(i)}
	}
	if why, _ := s.mayShareLocked(NewShareID(), NewToken(), bundle, at(1000)); why == "" {
		t.Error("one network shares one window more than it may")
	}
	for i := range mostMisses {
		s.missed(s.lost, at(i), time.Now())
	}
	if !s.missing(s.lost, at(1000), time.Now()) {
		t.Error("one network asks for more than it may")
	}
}

func TestAnIDThatWasAnothersStaysItsOwn(t *testing.T) {
	quick(t, 20*time.Millisecond, time.Second)
	s, u, bundle := sharing(t)
	refused := func(id, token string) error {
		t.Helper()
		st := newStates()
		l, err := ListenShare(ShareConfig{Relay: u, ID: id, Token: token, Bundle: bundle, OnState: func(err error) { st.tell("", err) }})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = l.Close() }()
		return st.next(t)
	}
	gone := func(id string) func() bool {
		return func() bool { sh, _ := s.shared(id); return sh == nil }
	}
	// One whose host stayed away, well past its grace.
	id, token := NewShareID(), NewToken()
	l, _ := sharedAs(t, u, id, token, bundle)
	_ = l.Close()
	until(t, "the share is still there", gone(id))
	time.Sleep(100 * time.Millisecond)
	if err := refused(id, NewToken()); err == nil {
		t.Error("another took the id of a share that was forgotten")
	}
	// Nor can another end it, and so keep its own host from it.
	other, err := ListenShare(ShareConfig{Relay: u, ID: id, Token: NewToken(), Bundle: bundle})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.End(context.Background()); err == nil {
		t.Error("another ended a share that was forgotten")
	}
	// Its own host has it back, as a machine that slept does.
	back, _ := sharedAs(t, u, id, token, bundle)
	if err := back.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	// And one that ended is nobody's, its own host's neither.
	time.Sleep(100 * time.Millisecond)
	for what, token := range map[string]string{"another": NewToken(), "its own host": token} {
		if err := refused(id, token); err == nil {
			t.Errorf("%s took the id of a share that ended", what)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p := s.past[id]; bytes.Contains(fmt.Append(nil, p), []byte(token)) {
		t.Error("its token is kept")
	}
}

func TestSharesThatWereAreRememberedToALimit(t *testing.T) {
	s := NewServer(NewSecret())
	mine := NewShareID()
	s.rememberLocked(mine, "mine", "192.0.2.1", true)
	first := NewShareID()
	s.rememberLocked(first, "token", "192.0.2.2", true)
	for range mostPastFrom {
		s.rememberLocked(NewShareID(), "token", "192.0.2.2", true)
	}
	if n := len(s.past); n != mostPastFrom+1 {
		t.Errorf("%d are remembered", n)
	}
	if _, ok := s.pastLocked(first); ok {
		t.Error("the oldest is remembered still")
	}
	if _, ok := s.pastLocked(mine); !ok {
		t.Error("another address's made room")
	}
	old := s.past[mine]
	old.at = time.Now().Add(-pastFor - time.Second)
	s.past[mine] = old
	if _, ok := s.pastLocked(mine); ok {
		t.Error("one is remembered longer than it is to be")
	}
}

func TestTheLogIsWrittenWithNothingHeld(t *testing.T) {
	quick(t, 20*time.Millisecond, time.Second)
	s, u, bundle := sharing(t)
	held := make(chan string, 64)
	s.Log = func(format string, args ...any) {
		if !s.mu.TryLock() {
			// Once more, as another request may hold it for a moment.
			time.Sleep(200 * time.Millisecond)
			if !s.mu.TryLock() {
				held <- fmt.Sprintf(format, args...)
				return
			}
		}
		s.mu.Unlock()
	}
	ended, _ := sharedAs(t, u, NewShareID(), NewToken(), bundle)
	if err := ended.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	left, _ := sharedAs(t, u, NewShareID(), NewToken(), bundle)
	id := left.ID()
	_ = left.Close()
	until(t, "the share is still there", func() bool { sh, _ := s.shared(id); return sh == nil })
	time.Sleep(300 * time.Millisecond)
	select {
	case line := <-held:
		t.Errorf("the relay stood still to say %q", line)
	default:
	}
}

func TestOneAddressCannotTakeEveryShare(t *testing.T) {
	s := NewServer(NewSecret())
	bundle := kept(t, s)
	may := func(from string) bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		why, _ := s.mayShareLocked(NewShareID(), NewToken(), bundle, from)
		return why == ""
	}
	for range mostSharesFrom {
		s.shares[NewShareID()] = &share{from: "192.0.2.1"}
	}
	if may("192.0.2.1") {
		t.Error("one more from the same address was let in")
	}
	if !may("192.0.2.2") {
		t.Error("another address was turned away")
	}
}

func TestARelayJustStartedHasAGuestTryAgain(t *testing.T) {
	s, u, bundle := sharing(t)
	s.settled = time.Now().Add(time.Minute)
	id := NewShareID()
	if _, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{}); !errors.Is(err, RefusedAway) {
		t.Fatalf("got %v", err)
	}
	if resp := get(t, pageOf(u, id)); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("its page: %s", resp.Status)
	}
	// What is no link at all is not waited for.
	if resp := get(t, pageOf(u, "nonsense")); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nonsense: %s", resp.Status)
	}
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	echoing(l)
	c, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
}

// behind is a proxy in front of u that hands it a folder of its own, and
// the relay's URL through it. change changes each request it passes on.
func behind(t *testing.T, u URL, change func(*httputil.ProxyRequest)) URL {
	t.Helper()
	to, err := url.Parse("http://" + u.Host())
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(http.StripPrefix("/kakel", &httputil.ReverseProxy{Rewrite: func(r *httputil.ProxyRequest) {
		r.SetURL(to)
		change(r)
	}}))
	t.Cleanup(p.Close)
	through, err := ParseURL(p.URL + "/kakel" + u.u.Path)
	if err != nil {
		t.Fatal(err)
	}
	return through
}

func TestAShareIsReachedThroughAProxysFolder(t *testing.T) {
	_, u, bundle := sharing(t)
	through := behind(t, u, func(r *httputil.ProxyRequest) { r.Out.Host = r.In.Host })
	id := NewShareID()
	l, _ := sharedAs(t, through, id, NewToken(), bundle)
	echoing(l)
	link := linkTo(t, through, id)
	if !strings.HasPrefix(link.String(), "http://"+through.Host()+"/kakel/s/"+id+"/#") {
		t.Fatalf("the link is %s", link)
	}
	// With no slash at its end, as somebody may leave it: one redirect,
	// to the same folder.
	page := strings.TrimSuffix(pageOf(through, id), "/")
	resp := get(t, page)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != "/kakel/s/"+id+"/" {
		t.Fatalf("%s led to %s: %s", page, resp.Request.URL, resp.Status)
	}
	c, err := JoinShare(context.Background(), link, Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	says(t, c)
}

func TestAPageOfAnotherSiteCannotJoin(t *testing.T) {
	_, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	echoing(l)
	from := func(u URL, origin string) error {
		ws, err := Dialer{}.open(context.Background(), linkTo(t, u, id).join(), http.Header{"Origin": {origin}})
		if err == nil {
			_ = ws.CloseNow()
		}
		return err
	}
	if err := from(u, "http://"+u.Host()); err != nil {
		t.Errorf("its own page: %v", err)
	}
	for _, other := range []string{"https://evil.example.com", "null", "http://" + u.Host() + ".example.com"} {
		if err := from(u, other); err == nil {
			t.Errorf("a page of %s joined", other)
		}
	}

	// Behind a proxy that passes the host on, behind one that says it in
	// a header of its own, and behind one that does neither.
	passes := behind(t, u, func(r *httputil.ProxyRequest) { r.Out.Host = r.In.Host })
	tells := behind(t, u, func(r *httputil.ProxyRequest) { r.SetXForwarded() })
	drops := behind(t, u, func(*httputil.ProxyRequest) {})
	for name, p := range map[string]URL{"passes": passes, "tells": tells} {
		if err := from(p, "http://"+p.Host()); err != nil {
			t.Errorf("its own page, behind a proxy that %s: %v", name, err)
		}
		if err := from(p, "https://evil.example.com"); err == nil {
			t.Errorf("another site's page joined behind a proxy that %s", name)
		}
	}
	if err := from(drops, "http://"+drops.Host()); err == nil {
		t.Error("a page was let through with no telling whose it is")
	}
}

func TestAnOldRelayIsSaidToBeOne(t *testing.T) {
	s, u := started(t)
	// A relay from before shares: the same one, without their doors.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, door := range []string{doorAbout, doorBundle, doorShare} {
			if strings.Contains(r.URL.Path, s.secret+"/"+door) {
				http.NotFound(w, r)
				return
			}
		}
		s.ServeHTTP(w, r)
	}))
	t.Cleanup(old.Close)
	at, err := ParseURL(old.URL + "/k/" + s.secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Client{Relay: at}).About(context.Background()); !errors.Is(err, ErrOldRelay) {
		t.Fatalf("got %v", err)
	}
	// A wrong secret is something else.
	wrong, err := ParseURL(strings.Replace(u.String(), s.secret, "nottheone12345678", 1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (Client{Relay: wrong}).About(context.Background()); err == nil || errors.Is(err, ErrOldRelay) {
		t.Fatalf("got %v", err)
	}
	about, err := Client{Relay: u}.About(context.Background())
	if err != nil || about.Shares != 1 || about.Bundle != MaxBundle || about.Unsigned {
		t.Fatalf("got %+v, %v", about, err)
	}
}

func TestNoErrorHoldsTheRelaysSecret(t *testing.T) {
	s, u, bundle := sharing(t)
	id := NewShareID()
	down, err := ParseURL("http://127.0.0.1:1/k/" + s.secret)
	if err != nil {
		t.Fatal(err)
	}
	// And one that sends whoever asks elsewhere.
	elsewhere := httptest.NewServer(http.RedirectHandler("http://127.0.0.1:1/k/"+s.secret, http.StatusFound))
	t.Cleanup(elsewhere.Close)
	moved, err := ParseURL(elsewhere.URL + "/k/" + s.secret)
	if err != nil {
		t.Fatal(err)
	}
	var errs []error
	for _, r := range []URL{down, moved} {
		c := Client{Relay: r}
		_, err := c.About(context.Background())
		errs = append(errs, err)
		_, err = c.HasBundle(context.Background(), bundle)
		errs = append(errs, err, c.PutBundle(context.Background(), []byte("PK"), "sig"))
		st := newStates()
		l, err := ListenShare(ShareConfig{Relay: r, ID: id, Token: NewToken(), Bundle: bundle, OnState: func(err error) { st.tell("", err) }})
		if err != nil {
			t.Fatal(err)
		}
		errs = append(errs, st.next(t), l.End(context.Background()))
		if strings.Contains(l.Addr().String(), s.secret) {
			t.Errorf("the listener's address is %s", l.Addr())
		}
	}
	for i, err := range errs {
		if err == nil {
			t.Errorf("%d: no error", i)
		} else if strings.Contains(err.Error(), s.secret) {
			t.Errorf("%d: the error holds the secret: %v", i, err)
		}
	}
	link := linkTo(t, u, id)
	if strings.Contains(link.String(), s.secret) {
		t.Errorf("the link holds the relay's secret")
	}
	if shown := link.Shown(); strings.Contains(shown, id) || strings.Contains(shown, link.Secret) {
		t.Errorf("shown as %s", shown)
	}
}

func TestTheLogHoldsNoSecretAndNoID(t *testing.T) {
	s, u, bundle := sharing(t)
	var mu sync.Mutex
	var said strings.Builder
	s.Log = func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		_, _ = io.WriteString(&said, fmt.Sprintf(format, args...)+"\n")
	}
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	echoing(l)
	c, err := JoinShare(context.Background(), linkTo(t, u, id), Dialer{})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if err := l.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	log := said.String()
	if !strings.Contains(log, shownID(id)) || strings.Contains(log, id) || strings.Contains(log, s.secret) {
		t.Errorf("the log says %s", log)
	}
}

func TestShareLinksReadAsTheyAreWritten(t *testing.T) {
	u, err := ParseURL("https://relay.example.com:8443/x/k/abc")
	if err != nil {
		t.Fatal(err)
	}
	id := NewShareID()
	l, err := NewShareLink(u, id, "abc_-DEF")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://relay.example.com:8443/x/s/" + id + "/#abc_-DEF"; l.String() != want {
		t.Fatalf("made %s, not %s", l, want)
	}
	// As it is written, and as a browser names the page at it.
	for in, secret := range map[string]string{
		l.String(): "abc_-DEF",
		"https://relay.example.com:8443/x/s/" + id:                       "",
		"https://relay.example.com:8443/x/s/" + id + "/":                 "",
		"https://RELAY.example.com:8443/x/s/" + id + "/index.html?a=b#s": "s",
	} {
		got, err := ParseShareLink(in)
		if err != nil || got.ID != id || got.Secret != secret || got.join() != "https://relay.example.com:8443/x/s/"+id+"/join" {
			t.Errorf("ParseShareLink(%q) = %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "https://relay.example.com/", "https://relay.example.com/s/abc/#x", "ftp://r/s/" + id,
		"https://r/s/" + strings.ToUpper(id), "https://r/s/" + id + "/client.wasm", "/s/" + id + "/", "https://r/k/abc"} {
		if _, err := ParseShareLink(bad); err == nil {
			t.Errorf("ParseShareLink(%q) took it", bad)
		}
	}
}

// signed is a key and its signature of manifest, as gunimsign writes
// them.
func signed(t *testing.T, manifest []byte) (ed25519.PublicKey, string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return pub, base64.StdEncoding.EncodeToString(ed25519.Sign(key, manifest)) + "\n"
}
