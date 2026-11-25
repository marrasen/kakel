package guest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
)

func TestASecretIsReadBackAsItWasWritten(t *testing.T) {
	_, s, err := newShare()
	if err != nil {
		t.Fatal(err)
	}
	text := s.String()
	if len(text) != 43 || strings.ContainsAny(text, "+/=#") {
		t.Errorf("the secret is written %q", text)
	}
	got, err := ParseSecret(text)
	if err != nil || got != s {
		t.Errorf("it reads back as %v, %v", got, err)
	}
	last := text[:42] + string("AB"[map[bool]int{true: 1}[text[42] == 'A']])
	for _, bad := range []string{"", text[:42], text + "A", text[:42] + "=", text[:42] + "\n", text + "\n", text[:20] + "\r\n" + text[20:], strings.Repeat("/", 43)} {
		if _, err := ParseSecret(bad); err == nil {
			t.Errorf("%q reads as a secret", bad)
		}
	}
	if other, err := ParseSecret(last); err == nil && other == s {
		t.Errorf("%q and %q read the same", text, last)
	}
	k := key(t)
	if got, err := ParseKey(k.String()); err != nil || got != k {
		t.Errorf("a key reads back as %v, %v", got, err)
	}
}

// Of any number who click Join at once, one is in, and the rest are told
// the link is used: then, and whenever they come again.
func TestALinkIsSpentOnce(t *testing.T) {
	r := newRig(t, false)
	var sessions []*Session
	for range unjoinedAtOnce {
		s, err := r.guest(key(t))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = s.Close() }()
		if got := s.View().Phase; got != PhaseJoin {
			t.Fatalf("a new guest is at phase %v", got)
		}
		sessions = append(sessions, s)
	}
	errs := make([]error, len(sessions))
	var all sync.WaitGroup
	for i, s := range sessions {
		all.Go(func() { errs[i] = s.Join(context.Background(), "Guest", "") })
	}
	all.Wait()
	in := 0
	for _, err := range errs {
		switch {
		case err == nil:
			in++
		case !errors.Is(err, ErrUsed):
			t.Errorf("one that was too late was told %v", err)
		}
	}
	if in != 1 {
		t.Fatalf("%d joined", in)
	}
	if _, err := r.guest(key(t)); !errors.Is(err, ErrUsed) {
		t.Errorf("one that came later was told %v", err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.asked) != 1 {
		t.Errorf("the host was asked %d times", len(r.asked))
	}
}

func TestOnlyTheLinksOwnKeyGetsIn(t *testing.T) {
	r := newRig(t, false)
	_, other, err := newShare()
	if err != nil {
		t.Fatal(err)
	}
	// The right host, and a seed that is not the link's.
	wrong := r.host.Secret()
	wrong.seed = other.seed
	if _, err := Dial(context.Background(), r.dial(), Options{Secret: wrong, Key: key(t)}); !errors.Is(err, ErrDenied) {
		t.Errorf("a wrong secret is told %v", err)
	}
	// The right seed, and another host.
	wrong = r.host.Secret()
	wrong.host = other.host
	if _, err := Dial(context.Background(), r.dial(), Options{Secret: wrong, Key: key(t)}); !errors.Is(err, ErrWrongHost) {
		t.Errorf("a wrong host is %v", err)
	}
	// And a guest is bound by its own key alone.
	r.in(key(t))
	if _, err := r.raw(signer(t, key(t))); err == nil {
		t.Error("another guest's key got in")
	}
	// Nothing but a public key is taken.
	nc := r.dial()
	_, _, _, err = ssh.NewClientConn(nc, "", &ssh.ClientConfig{
		User: "guest", Auth: []ssh.AuthMethod{ssh.Password("x")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // the host is this test's own
	})
	_ = nc.Close()
	if err == nil || !strings.Contains(err.Error(), "[none]") {
		t.Errorf("a password was tried: %v", err)
	}
}

// A guest that joined with the link's own key would keep the link alive.
func TestAGuestCannotJoinWithTheLinksKey(t *testing.T) {
	r := newRig(t, false)
	conn, err := r.raw(r.linkSigner())
	if err != nil {
		t.Fatal(err)
	}
	ch, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	pub := r.linkSigner().PublicKey().Marshal()
	if _, err := ch.Write(joinFrame(t, "Guest", pub)); err != nil {
		t.Fatal(err)
	}
	_ = conn.Wait()
	if st := r.host.Status(); st.Phase != PhaseJoin {
		t.Errorf("the share is at phase %v", st.Phase)
	}
}

func TestARefusedGuestIsToldAndTheShareEnds(t *testing.T) {
	r := newRig(t, false)
	s := r.joined(key(t))
	r.answer <- false
	r.ended(s, ErrRefused)
	r.serving.Wait()
	if st := r.host.Status(); st.Phase != PhaseOver || st.Reason != ReasonRefused {
		t.Errorf("the share is %+v", st)
	}
}

// What the host hears of a guest is made plain.
func TestAGuestsWordsAreMadePlain(t *testing.T) {
	r := newRig(t, false)
	conn, err := r.raw(r.linkSigner())
	if err != nil {
		t.Fatal(err)
	}
	ch, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	name := "  \x1b[31mEve\u202etxt.\u2066\u200b\n\tadmin \x07" + strings.Repeat("e", 80)
	if _, err := ch.Write(joinFrame(t, name, signer(t, key(t)).PublicKey().Marshal())); err != nil {
		t.Fatal(err)
	}
	r.until("the host is asked", func() bool { r.mu.Lock(); defer r.mu.Unlock(); return len(r.asked) == 1 })
	got := r.asked[0]
	if want := "[31mEvetxt. admin " + strings.Repeat("e", 22); got.Name != want {
		t.Errorf("the name is %q, want %q", got.Name, want)
	}
	if got.From == "" || len([]rune(got.About)) > mostAbout {
		t.Errorf("the rest is %+v", got)
	}
	for _, bad := range []string{"", " \n", "\u202e\u200b"} {
		if _, _, err := decodeJoining(joinFrame(t, bad, signer(t, key(t)).PublicKey().Marshal())[headerLen:]); err == nil {
			t.Errorf("%q is a name", bad)
		}
	}
}

// A guest that waits is sent two bytes' worth of news, and nothing of the
// window, whatever it asks for.
func TestAWaitingGuestLearnsNothing(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 40, 10)
	r.share("secret-pane", term)
	r.publish(terminals(true, "secret-pane"))
	k := key(t)
	conn, err := r.raw(r.linkSigner())
	if err != nil {
		t.Fatal(err)
	}
	ch, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	var mu sync.Mutex
	var heard []byte
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := ch.Read(buf)
			mu.Lock()
			heard = append(heard, buf[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	refusal := func(id string) string {
		_, _, err := conn.OpenChannel(chanPane, ssh.Marshal(openPane{id}))
		var no *ssh.OpenChannelError
		if !errors.As(err, &no) {
			t.Fatalf("opening %s: %v", id, err)
		}
		return no.Error()
	}
	// Before joining, and while waiting.
	for range 2 {
		if a, b := refusal("secret-pane"), refusal("no-such-pane"); a != b {
			t.Errorf("a pane that is there is refused with %q, and one that is not with %q", a, b)
		}
		_, _ = ch.Write(joinFrame(t, "Guest", signer(t, k).PublicKey().Marshal()))
		r.until("the guest waits", func() bool { return r.host.Status().Phase == PhaseWaiting })
	}
	say(r, term, p, "what the program says", "a-title")
	st := terminals(true, "secret-pane", "another")
	st.Title = "a new title"
	r.publish(st)
	_, _ = ch.Write(frame(frameFocus, []byte("secret-pane")))
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	want := append(frame(frameJoin), frame(frameWaiting)...)
	if !bytes.Equal(heard, want) {
		t.Errorf("it heard %q, want %q", heard, want)
	}
	if st := r.host.Status(); st.Focus != "" {
		t.Errorf("its focus is %q", st.Focus)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.opened) != 0 {
		t.Errorf("the host was asked to open %v", r.opened)
	}
}

// joinFrame is a join as a guest's own program might write it.
func joinFrame(t *testing.T, name string, pub []byte) []byte {
	t.Helper()
	body, err := json.Marshal(joining{Name: name, About: strings.Repeat("b", 400), Key: pub})
	if err != nil {
		t.Fatal(err)
	}
	return frame(frameName, body)
}

// A guest that lost its connection comes back where it was, with the key
// its tab kept: to wait on the same question, and then to the window.
func TestAGuestComesBack(t *testing.T) {
	r := newRig(t, false)
	k := key(t)
	s := r.joined(k)
	_ = s.Close()
	r.ended(s, ErrLost)
	r.until("the host knows it is away", func() bool { return !r.host.Status().Here })

	s, err := r.guest(k)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if got := s.View().Phase; got != PhaseWaiting {
		t.Fatalf("it comes back at phase %v", got)
	}
	r.publish(terminals(true, "a"))
	r.answer <- true
	r.until("it is in", func() bool { return s.View().Phase == PhaseIn })
	if st := s.View().State; st.Title != "the window" || len(st.Panes) != 1 {
		t.Errorf("the window is %+v", st)
	}
	_ = s.Close()

	// The window changed while it was away.
	r.until("the host knows it is away again", func() bool { return !r.host.Status().Here })
	r.publish(terminals(false, "a", "b"))
	s, err = r.guest(k)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if st := s.View(); st.Phase != PhaseIn || len(st.State.Panes) != 2 || st.State.CanType {
		t.Errorf("it comes back to %+v", st)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.asked) != 1 {
		t.Errorf("the host was asked %d times", len(r.asked))
	}
}

func TestAnAllowedGuestThatIsAwayComesStraightIn(t *testing.T) {
	r := newRig(t, false)
	k := key(t)
	_ = r.joined(k).Close()
	r.until("the host knows it is away", func() bool { return !r.host.Status().Here })
	r.answer <- true
	r.until("the share is in", func() bool { return r.host.Status().Phase == PhaseIn })
	s, err := r.guest(k)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if got := s.View().Phase; got != PhaseIn {
		t.Errorf("it comes back at phase %v", got)
	}
}

// The same guest somewhere else takes the share with it.
func TestASecondTabTakesOver(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 40, 10)
	r.share("a", term)
	r.publish(terminals(true, "a"))
	k := key(t)
	first := r.in(k)
	pane, err := first.Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.guest(k)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	r.ended(first, ErrReplaced)
	if _, err := io.ReadAll(pane); err != nil {
		t.Errorf("the pane of the one replaced: %v", err)
	}
	if second.View().Phase != PhaseIn || !r.host.Status().Here {
		t.Error("the second is not in")
	}
	two, err := second.Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = two.Write([]byte("typed"))
	r.until("the second types", func() bool { return p.typed() == "typed" })
}

// The host decides whether the guest may type, whatever the guest sends,
// and while it types: once Publish has returned, nothing more is typed.
func TestAGuestTypesOnlyWhileItMay(t *testing.T) {
	r := newRig(t, false)
	src := &recorder{}
	r.share("a", src)
	r.publish(terminals(false, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	// A guest's own program, which minds nothing it is told, and types all
	// the while.
	stop := make(chan struct{})
	var typing sync.WaitGroup
	typing.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				if _, err := pane.Write([]byte(".")); err != nil {
					return
				}
				time.Sleep(time.Millisecond)
			}
		}
	})
	for range 10 {
		r.publish(terminals(false, "a"))
		was := src.got()
		time.Sleep(30 * time.Millisecond)
		if got := src.got(); got != was {
			t.Fatalf("%d bytes were typed by a guest that may not", got-was)
		}
		r.publish(terminals(true, "a"))
		r.until("the guest types", func() bool { return src.got() > was })
	}
	close(stop)
	typing.Wait()
	if st := r.host.Status(); st.TypedIn != "a" || st.TypedAt.IsZero() {
		t.Errorf("the host is told %+v", st)
	}
}

// recorder is a pane that only keeps what it is sent.
type recorder struct {
	mu   sync.Mutex
	sent []byte
	f    uiterm.Follower
}

func (p *recorder) Follow(f uiterm.Follower) func() {
	p.mu.Lock()
	p.f = f
	p.mu.Unlock()
	f.Screen(80, 24, []byte("screen"))
	return func() {}
}
func (p *recorder) CatchUp(f uiterm.Follower) { f.Screen(80, 24, []byte("again")) }
func (p *recorder) Lane() uiterm.Lane         { return recorderLane{p} }

// recorderLane takes all that is typed at once.
type recorderLane struct{ p *recorder }

func (l recorderLane) Type(b []byte) int {
	l.p.mu.Lock()
	defer l.p.mu.Unlock()
	l.p.sent = append(l.p.sent, b...)
	return len(b)
}
func (l recorderLane) Room() <-chan struct{} { return nil }
func (l recorderLane) Drop()                 {}
func (l recorderLane) Close()                {}

// follower is who follows it, once somebody does.
func (p *recorder) follower(r *rig) (f uiterm.Follower) {
	r.until("somebody follows", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		f = p.f
		return f != nil
	})
	return f
}

func (p *recorder) got() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.sent)
}

// A paste of a megabyte goes through, whole and soon.
func TestALargePasteIsTyped(t *testing.T) {
	r := newRig(t, false)
	src := &recorder{}
	r.share("a", src)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	text := bytes.Repeat([]byte("0123456789abcdef"), 1<<16)
	start := time.Now()
	if n, err := pane.Write(text); n != len(text) || err != nil {
		t.Fatalf("wrote %d, %v", n, err)
	}
	r.until("all of it is typed", func() bool { return src.got() == len(text) })
	if !bytes.Equal(src.sent, text) {
		t.Error("it was typed wrong")
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("it took %v", took)
	}
}

// Once a pane has left the window nothing more of it is sent, and nothing
// typed in it, however much is on its way.
func TestAPaneThatLeavesTheWindowIsSentNoMore(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 40, 10)
	r.share("a", term)
	for round := range 20 {
		if round == 0 {
			r.publish(terminals(true, "a", "b"))
			r.in(key(t))
		}
		r.publish(terminals(true, "a", "b"))
		s := r.last
		pane, err := s.Pane(context.Background(), "a")
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		all := make(chan []byte)
		go func() { b, _ := io.ReadAll(pane); all <- b }()
		// A program that writes all the while, before and after.
		stop := make(chan struct{})
		var writing sync.WaitGroup
		writing.Go(func() {
			for {
				select {
				case <-stop:
					return
				case p.out <- []byte("shared "):
				}
			}
		})
		time.Sleep(time.Duration(round) * time.Millisecond)
		r.publish(terminals(true, "b"))
		close(stop)
		writing.Wait()
		p.out <- []byte("\r\nnot for the guest")
		select {
		case got := <-all:
			if bytes.Contains(got, []byte("not for")) {
				t.Fatalf("round %d: the guest was sent what was written after", round)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("the pane was never closed")
		}
		_, _ = pane.Write([]byte("typed after"))
		if _, err := s.Pane(context.Background(), "a"); !errors.Is(err, ErrNotShared) {
			t.Errorf("a pane that left is opened again: %v", err)
		}
		r.until("the terminal has it", func() bool { return strings.Contains(term.Text(), "guest") })
		p.out <- []byte("\x1bc\x1b[3J")
		r.until("the terminal is cleared", func() bool { return !strings.Contains(term.Text(), "guest") })
	}
	if got := p.typed(); got != "" {
		t.Errorf("%q was typed", got)
	}
}

func TestOnlyPanesOfTheWindowAreOpened(t *testing.T) {
	r := newRig(t, false)
	term, _ := terminal(t, 40, 10)
	for _, id := range []string{"a", "gone", "files", "elsewhere"} {
		r.share(id, term)
	}
	r.mu.Lock()
	delete(r.panes, "gone")
	r.mu.Unlock()
	st := terminals(true, "a", "gone")
	st.Panes = append(st.Panes, Pane{ID: "files", Kind: KindOther})
	r.publish(st)
	s := r.in(key(t))
	for _, id := range []string{"gone", "files", "elsewhere", "", strings.Repeat("x", mostID+1)} {
		if _, err := s.Pane(context.Background(), id); !errors.Is(err, ErrNotShared) {
			t.Errorf("opening %q: %v", id, err)
		}
	}
	r.mu.Lock()
	if want := []string{"gone"}; !slices.Equal(r.opened, want) {
		t.Errorf("the host was asked to open %v, want %v", r.opened, want)
	}
	r.mu.Unlock()
	pane, err := s.Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pane(context.Background(), "a"); !errors.Is(err, ErrNotShared) {
		t.Errorf("it is opened twice: %v", err)
	}
	// Closed, it may be opened again.
	_ = pane.Close()
	_ = pane.Wait()
	r.until("it is opened again", func() bool {
		again, err := s.Pane(context.Background(), "a")
		if err == nil {
			_ = again.Close()
		}
		return err == nil
	})
}

func TestFocusIsToldAndBounded(t *testing.T) {
	r := newRig(t, false)
	r.publish(terminals(true, "a", "b", "c"))
	s := r.in(key(t))
	s.Focus("b")
	r.until("the focus is b", func() bool { return r.host.Status().Focus == "b" })
	s.Focus("elsewhere")
	s.Focus(strings.Repeat("x", mostID+1))
	for range 2000 {
		s.Focus("a")
		s.Focus("b")
	}
	// The last of them.
	s.Focus("c")
	r.until("the focus is c", func() bool { return r.host.Status().Focus == "c" })
	r.publish(terminals(true, "a", "b"))
	if got := r.host.Status().Focus; got != "" {
		t.Errorf("the focus is %q, which has left", got)
	}
	r.until("OnChange has it", func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.statuss[len(r.statuss)-1].Focus == ""
	})
}

func TestLeavingEndsTheShare(t *testing.T) {
	r := newRig(t, false)
	s := r.in(key(t))
	s.Leave()
	r.ended(s, &EndedError{ReasonLeft})
	r.until("the share is over", func() bool { return r.host.Status().Reason == ReasonLeft })
}

func TestTheHostsEndIsTold(t *testing.T) {
	for _, why := range []Reason{ReasonStopped, ReasonWindowClosed, ReasonQuitting} {
		r := newRig(t, false)
		term, _ := terminal(t, 40, 10)
		r.share("a", term)
		r.publish(terminals(true, "a"))
		s := r.in(key(t))
		pane, err := s.Pane(context.Background(), "a")
		if err != nil {
			t.Fatal(err)
		}
		r.host.End(why)
		r.ended(s, &EndedError{why})
		if _, err := io.ReadAll(pane); err != nil {
			t.Errorf("the pane: %v", err)
		}
		r.until("OnChange has it", func() bool {
			r.mu.Lock()
			defer r.mu.Unlock()
			n := len(r.statuss)
			return n > 0 && r.statuss[n-1].Phase == PhaseOver && r.statuss[n-1].Reason == why
		})
	}
}

// Whatever a guest asks for besides is refused, and it goes on.
func TestNothingElseIsThere(t *testing.T) {
	r := newRig(t, false)
	r.publish(terminals(true, "a"))
	k := key(t)
	first := r.in(k)
	conn, err := r.raw(signer(t, k))
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"session", "direct-tcpip", "session@gridterm", "tcp@gridterm", "x11", "", chanPane} {
		if _, _, err := conn.OpenChannel(kind, []byte("anything")); err == nil {
			t.Errorf("a %q channel was opened", kind)
		}
	}
	for _, kind := range []string{"tcpip-forward", "start-again@gridterm", "keepalive@openssh.com"} {
		if ok, _, err := conn.SendRequest(kind, true, nil); ok || err != nil {
			t.Errorf("%s: %v, %v", kind, ok, err)
		}
	}
	// It has not taken over by coming alone, only by its control channel.
	if first.Err() != nil {
		t.Error("the guest was replaced by a connection that did nothing")
	}
	ch, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.OpenChannel(chanControl, nil); err == nil {
		t.Error("there are two control channels")
	}
	if ok, err := ch.SendRequest("shell", true, nil); ok || err != nil {
		t.Errorf("a request on it: %v, %v", ok, err)
	}
	go ssh.DiscardRequests(reqs)
	r.ended(first, ErrReplaced)
}

// A guest that breaks the rules is hung up on, and the share goes on.
func TestABadGuestIsHungUpOn(t *testing.T) {
	src := &recorder{}
	for name, bad := range map[string]func(conn ssh.Conn, control ssh.Channel){
		"a huge frame": func(_ ssh.Conn, ch ssh.Channel) {
			_, _ = ch.Write([]byte{frameFocus, 0xff, 0xff, 0xff, 0xff})
		},
		"a kind of frame there is not": func(_ ssh.Conn, ch ssh.Channel) { _, _ = ch.Write(frame('?')) },
		"a frame of the host's":        func(_ ssh.Conn, ch ssh.Channel) { _, _ = ch.Write(frame(frameState, []byte("{}"))) },
		"a second join": func(_ ssh.Conn, ch ssh.Channel) {
			_, _ = ch.Write(joinFrame(t, "Guest", signer(t, key(t)).PublicKey().Marshal()))
		},
		"a join that is not one": func(_ ssh.Conn, ch ssh.Channel) { _, _ = ch.Write(frame(frameName, []byte("{"))) },
		"a huge frame of typing": func(conn ssh.Conn, _ ssh.Channel) {
			ch, reqs, err := conn.OpenChannel(chanPane, ssh.Marshal(openPane{"a"}))
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			_, _ = ch.Write([]byte{frameTyped, 0, 1, 0, countLen + 1})
		},
		"a frame on a pane that is not typing": func(conn ssh.Conn, _ ssh.Channel) {
			ch, reqs, err := conn.OpenChannel(chanPane, ssh.Marshal(openPane{"a"}))
			if err != nil {
				return
			}
			go ssh.DiscardRequests(reqs)
			_, _ = ch.Write(frame(frameScreen, []byte("\x00\x50\x00\x18")))
		},
		"channels without end": func(conn ssh.Conn, _ ssh.Channel) {
			for range 10 * opensBurst {
				_, _, _ = conn.OpenChannel("session", nil)
			}
		},
		"panes opened and closed without end": func(conn ssh.Conn, _ ssh.Channel) {
			for range 10 * opensBurst {
				ch, reqs, err := conn.OpenChannel(chanPane, ssh.Marshal(openPane{"a"}))
				if err != nil {
					continue
				}
				go ssh.DiscardRequests(reqs)
				_ = ch.Close()
			}
		},
		"requests without end": func(conn ssh.Conn, _ ssh.Channel) {
			for range 10 * requestsBurst {
				_, _, _ = conn.SendRequest("x", false, nil)
			}
		},
		"requests on a channel without end": func(_ ssh.Conn, ch ssh.Channel) {
			for range 10 * requestsBurst {
				_, _ = ch.SendRequest("x", false, nil)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, false)
			r.share("a", src)
			r.publish(terminals(true, "a"))
			k := key(t)
			_ = r.in(k).Close()
			conn, err := r.raw(signer(t, k))
			if err != nil {
				t.Fatal(err)
			}
			ch, reqs, err := conn.OpenChannel(chanControl, nil)
			if err != nil {
				t.Fatal(err)
			}
			go ssh.DiscardRequests(reqs)
			go func() { _, _ = io.Copy(io.Discard, ch) }()
			r.until("it is here", func() bool { return r.host.Status().Here })
			gone := make(chan struct{})
			go func() { _ = conn.Wait(); close(gone) }()
			// Not waited for: a channel asked for as the connection goes is
			// never answered.
			go bad(conn, ch)
			select {
			case <-gone:
			case <-time.After(10 * time.Second):
				t.Fatal("it was not hung up on")
			}
			s, err := r.guest(k)
			if err != nil {
				t.Fatalf("the guest cannot come back: %v", err)
			}
			_ = s.Close()
		})
	}
}

// Callers who prove nothing fill only their own few places, and the guest
// who is in hears nothing of them.
func TestCallersThatProveNothingAreBounded(t *testing.T) {
	r := newRig(t, false)
	s := r.in(key(t))
	arriving := func() int {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		return len(r.host.arriving)
	}
	var callers []net.Conn
	for i := range 3 * handshakesAtOnce {
		nc := r.dial()
		defer func() { _ = nc.Close() }()
		callers = append(callers, nc)
		if i < handshakesAtOnce {
			r.until("it is here", func() bool { return arriving() == i+1 })
		} else if n := arriving(); n > handshakesAtOnce {
			t.Errorf("%d callers are here", n)
		}
	}
	// The first made room for those that came after.
	_ = callers[0].SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(callers[0]); errors.Is(err, os.ErrDeadlineExceeded) {
		t.Errorf("the first is here still: %v", err)
	}
	for _, c := range callers {
		_ = c.Close()
	}
	// Those who hold the link, spent, are told so and go.
	for range 3 * unjoinedAtOnce {
		r.until("they have gone", func() bool {
			_, err := r.guest(key(t))
			return errors.Is(err, ErrUsed)
		})
	}
	if s.Err() != nil {
		t.Errorf("the guest is gone: %v", s.Err())
	}
}

// A pane goes on through its program ending and another being started.
func TestAPaneGoesOnThroughARestart(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 40, 10)
	r.share("a", term)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	there := shownOn(t, pane)
	say(r, term, p, "first\r\n", "one")
	r.until("the guest sees the first", func() bool { return there.Title() == "one" })
	_ = p.Close()
	r.until("the guest hears it ended", pane.Ended)
	_, _ = pane.Write([]byte("to nobody"))
	term.Say("the program has gone")
	r.until("the guest sees what the window said", func() bool { return strings.Contains(there.Text(), "has gone") })

	again := newProgram()
	if err := term.Restart(again); err != nil {
		t.Fatal(err)
	}
	r.until("the guest hears it started", func() bool { return !pane.Ended() })
	say(r, term, again, "second", "two")
	r.until("the guest sees the second", func() bool { return there.Title() == "two" })
	if a, b := term.Text(), there.Text(); a != b {
		t.Errorf("the host has\n%s\nand the guest\n%s", a, b)
	}
	_, _ = pane.Write([]byte("typed"))
	r.until("the guest types in the second", func() bool { return strings.HasSuffix(again.typed(), "typed") })
	// One opened on a pane whose program has ended hears so.
	_ = again.Close()
	r.until("it ended", pane.Ended)
	_ = pane.Close()
	_ = pane.Wait()
	var late *ShownPane
	r.until("it is opened again", func() bool {
		late, err = r.last.Pane(context.Background(), "a")
		return err == nil
	})
	seen := shownOn(t, late)
	r.until("it ended, and shows what it left", func() bool { return late.Ended() && strings.Contains(seen.Text(), "second") })
}

// shownOn is a guest's terminal on pane.
func shownOn(t *testing.T, pane *ShownPane) *uiterm.Terminal {
	t.Helper()
	term, err := uiterm.New(uiterm.Config{Session: pane, Size: ui.Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	return term
}

// A guest that takes nothing holds up nobody, and is not held for.
func TestASlowGuestIsBoundedAndCatchesUp(t *testing.T) {
	r := newRig(t, false)
	term, p := terminal(t, 80, 24)
	r.share("a", term)
	r.publish(terminals(true, "a"))
	s := r.in(key(t))
	pane, err := s.Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	var st *stream
	r.until("the host carries it", func() bool {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		for c := range r.host.conns {
			st = c.streams["a"]
		}
		return st != nil
	})
	// More than the window of the channel and what is held besides.
	const total = 8 << 20
	line := bytes.Repeat([]byte("a line of some length, over and over\r\n"), 400)
	most := 0
	start := time.Now()
	for range total / len(line) {
		p.out <- line
		st.mu.Lock()
		most = max(most, len(st.data)+len(st.screen))
		st.mu.Unlock()
	}
	say(r, term, p, "\r\nthe end", "end")
	t.Logf("%d bytes in %v, at most %d held", total, time.Since(start), most)
	if most > mostBehind+64<<10 {
		t.Errorf("%d bytes were held for it", most)
	}
	there := shownOn(t, pane)
	r.until("it catches up", func() bool { return there.Title() == "end" })
	if a, b := term.Text(), there.Text(); a != b {
		t.Errorf("the host has\n%s\nand the guest\n%s", a, b)
	}
}

// A guest that reads no state holds up nobody, and is sent the latest once
// it does, not all there were.
func TestStatesAreLatestOnly(t *testing.T) {
	r := newRig(t, false)
	k := key(t)
	_ = r.in(k).Close()
	conn, err := r.raw(signer(t, k))
	if err != nil {
		t.Fatal(err)
	}
	ch, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	big := terminals(true, "a")
	big.Layout = []byte(`"` + strings.Repeat("x", 900<<10) + `"`)
	const times = 12
	for range times {
		// Held up, this would never be back: nothing is read.
		r.publish(big)
	}
	r.publish(terminals(false, "a", "b"))
	got := 0
	for {
		_, body, err := readFrame(ch, controlToGuest, nil)
		if err != nil {
			t.Fatal(err)
		}
		got++
		if st, err := decodeState(body); err != nil {
			t.Fatal(err)
		} else if len(st.Panes) == 2 {
			break
		}
	}
	// What fits the window of the channel was sent, and the last.
	if got > 6 {
		t.Errorf("%d states of %d were sent", got, times+1)
	}
	big.Layout = []byte(`"` + strings.Repeat("x", mostState) + `"`)
	if err := r.host.Publish(big); err == nil {
		t.Error("a state too large was published")
	}
}

func TestASharesTimesRunOut(t *testing.T) {
	t.Run("a link nobody uses", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			time.Sleep(LinkFor - 10*time.Second)
			s, err := r.guest(key(t))
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(11 * time.Second)
			r.ended(s, ErrExpired)
			r.over(ReasonExpired)
		})
	})
	t.Run("one that does not join", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			s, err := r.guest(key(t))
			if err != nil {
				t.Fatal(err)
			}
			time.Sleep(joinWindow + time.Second)
			r.ended(s, ErrLost)
			if st := r.host.Status(); st.Phase != PhaseJoin {
				t.Errorf("the share is %+v", st)
			}
		})
	})
	t.Run("one that says nothing", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			nc := r.dial()
			start := time.Now()
			_, _ = io.ReadAll(nc)
			if took := time.Since(start); took > handshakeWindow+time.Second {
				t.Errorf("it was held for %v", took)
			}
			_ = nc.Close()
		})
	})
	t.Run("a host that does not answer", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			s := r.joined(key(t))
			time.Sleep(AskFor - time.Second)
			if s.View().Phase != PhaseWaiting {
				t.Fatal("it does not wait")
			}
			time.Sleep(2 * time.Second)
			r.ended(s, &EndedError{ReasonUnanswered})
			r.over(ReasonUnanswered)
		})
	})
	t.Run("a guest away while asked about", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			k := key(t)
			_ = r.joined(k).Close()
			time.Sleep(AskAwayFor - time.Second)
			s, err := r.guest(k)
			if err != nil {
				t.Fatal(err)
			}
			// Back in time, and away anew.
			time.Sleep(time.Minute)
			_ = s.Close()
			time.Sleep(AskAwayFor - time.Second)
			if st := r.host.Status(); st.Phase != PhaseWaiting || st.Here {
				t.Fatalf("the share is %+v", st)
			}
			time.Sleep(2 * time.Second)
			r.over(ReasonAway)
		})
	})
	t.Run("a guest away", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			r := newRig(t, true)
			k := key(t)
			_ = r.in(k).Close()
			time.Sleep(AwayFor - time.Second)
			s, err := r.guest(k)
			if err != nil {
				t.Fatal(err)
			}
			// Here, it may stay.
			time.Sleep(3 * AwayFor)
			if s.Err() != nil {
				t.Fatalf("it was ended: %v", s.Err())
			}
			_ = s.Close()
			time.Sleep(AwayFor + time.Second)
			r.over(ReasonAway)
		})
	})
}

func (r *rig) over(why Reason) {
	r.t.Helper()
	synctest.Wait()
	if st := r.host.Status(); st.Phase != PhaseOver || st.Reason != why {
		r.t.Errorf("the share is %+v, want it over for %s", st, why)
	}
}

// A line that was cut without a word is noticed by both ends.
func TestACutLineIsNoticed(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, true)
		k := key(t)
		var mu sync.Mutex
		var line *memConn
		was := r.dial
		r.dial = func() (c net.Conn) {
			c = was()
			mu.Lock()
			line = c.(*memConn)
			mu.Unlock()
			return c
		}
		s := r.in(k)
		time.Sleep(5 * time.Minute)
		if s.Err() != nil {
			t.Fatalf("an idle guest was lost: %v", s.Err())
		}
		line.cut()
		start := time.Now()
		<-s.Done()
		if took := time.Since(start); took > time.Minute || !errors.Is(s.Err(), ErrLost) {
			t.Errorf("the guest noticed after %v: %v", took, s.Err())
		}
		for r.host.Status().Here {
			time.Sleep(time.Second)
		}
		if took := time.Since(start); took > time.Minute {
			t.Errorf("the host noticed after %v", took)
		}
		if _, err := r.guest(k); err != nil {
			t.Errorf("it cannot come back: %v", err)
		}
		r.stop()
	})
}

// A host that is sent nothing still is heard, and one whose guest takes a
// long time over much output is not taken for gone.
func TestASlowLineIsNotACutOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, true)
		src := &recorder{}
		r.share("a", src)
		r.publish(terminals(true, "a"))
		s := r.in(key(t))
		pane, err := s.Pane(context.Background(), "a")
		if err != nil {
			t.Fatal(err)
		}
		f := src.follower(r)
		stop := make(chan struct{})
		var writing sync.WaitGroup
		writing.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					f.Write(make([]byte, 64<<10))
					time.Sleep(100 * time.Millisecond)
				}
			}
		})
		buf := make([]byte, 8<<10)
		for range 300 {
			if _, _, err := pane.ReadScreen(buf); err != nil {
				t.Fatal(err)
			}
			time.Sleep(time.Second)
		}
		close(stop)
		writing.Wait()
		if s.Err() != nil {
			t.Errorf("it was lost: %v", s.Err())
		}
	})
}

// Nothing is left running once a share is over.
func TestNothingIsLeftRunning(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 5 {
		r := newRig(t, false)
		term, p := terminal(t, 40, 10)
		r.share("a", term)
		r.publish(terminals(true, "a"))
		s := r.in(key(t))
		pane, err := s.Pane(context.Background(), "a")
		if err != nil {
			t.Fatal(err)
		}
		there := shownOn(t, pane)
		say(r, term, p, "hello", "t")
		r.until("it is seen", func() bool { return there.Title() == "t" })
		_, _ = r.guest(key(t))
		_ = r.dial().Close()
		r.stop()
		<-s.Done()
		_ = there.Close()
		_ = term.Close()
	}
	end := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(end) {
		time.Sleep(10 * time.Millisecond)
	}
	if now := runtime.NumGoroutine(); now > before {
		buf := make([]byte, 1<<20)
		t.Errorf("%d goroutines before and %d after:\n%s", before, now, buf[:runtime.Stack(buf, true)])
	}
}

// A share goes on when its listener fails, and takes another.
func TestAShareTakesAnotherListener(t *testing.T) {
	r := newRig(t, false)
	first, second := newMemListener(), newMemListener()
	done := make(chan error, 2)
	go func() { done <- r.host.Serve(first) }()
	_ = first.Close()
	if err := <-done; err == nil {
		t.Error("a listener that failed is said to have ended well")
	}
	go func() { done <- r.host.Serve(second) }()
	s, err := Dial(context.Background(), second.dial(), Options{Secret: r.host.Secret(), Key: key(t)})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	r.host.End(ReasonStopped)
	if err := <-done; err != nil {
		t.Errorf("the end of the share: %v", err)
	}
	if _, err := Dial(context.Background(), second.dial(), Options{Secret: r.host.Secret(), Key: key(t)}); !errors.Is(err, ErrLost) {
		t.Errorf("a share that is over: %v", err)
	}
}
