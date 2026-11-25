package guest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// A state that cannot be sent still takes away what it takes away, and
// the guest is told what can be told.
func TestAStateThatCannotBeSentWithdrawsAllTheSame(t *testing.T) {
	for name, spoil := range map[string]func(*State){
		"a layout that is not JSON": func(st *State) { st.Layout = []byte("{") },
		"too much":                  func(st *State) { st.Theme = []byte(`"` + strings.Repeat("x", mostState) + `"`) },
		"too much of everything": func(st *State) {
			st.Title, st.HostName = strings.Repeat("\x00", mostState), strings.Repeat("<", mostState)
			for i := range 5000 {
				st.Panes = append(st.Panes, Pane{ID: fmt.Sprint(strings.Repeat("<", 100), i), Kind: KindTerminal, Title: strings.Repeat("\x00", 1000), Icon: strings.Repeat("<", 1000)})
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRig(t, false)
			a, b := &recorder{}, &recorder{}
			r.share("a", a)
			r.share("b", b)
			r.publish(terminals(true, "a", "b"))
			s := r.in(key(t))
			pane, err := s.Pane(context.Background(), "a")
			if err != nil {
				t.Fatal(err)
			}
			kept, err := s.Pane(context.Background(), "b")
			if err != nil {
				t.Fatal(err)
			}
			go func() { _, _ = io.Copy(io.Discard, kept) }()
			_, _ = pane.Write([]byte("before"))
			r.until("it types", func() bool { return a.got() == 6 })

			st := terminals(false, "b")
			spoil(&st)
			if err := r.host.Publish(st); err == nil {
				t.Error("nobody is told that it could not be sent")
			}
			_, _ = pane.Write([]byte("in a pane that left"))
			_, _ = kept.Write([]byte("while it may not"))
			if _, err := io.ReadAll(pane); err != nil {
				t.Errorf("the pane that left: %v", err)
			}
			if _, err := s.Pane(context.Background(), "a"); !errors.Is(err, ErrNotShared) {
				t.Errorf("it is opened again: %v", err)
			}
			r.until("the guest is told", func() bool {
				v := s.View()
				return v.Phase == PhaseIn && !v.State.CanType && len(v.State.Panes) > 0 && v.State.Panes[0].ID == "b"
			})
			time.Sleep(20 * time.Millisecond)
			if a.got() != 6 || b.got() != 0 {
				t.Errorf("%d and %d bytes were typed", a.got(), b.got())
			}
		})
	}
}

// slow is a program that reads a little at a time.
type slow struct {
	*program
	every time.Duration
	each  int
}

func (p *slow) Write(b []byte) (int, error) {
	for done := 0; done < len(b); {
		n := min(p.each, len(b)-done)
		time.Sleep(p.every)
		_, _ = p.program.Write(b[done : done+n])
		done += n
	}
	return len(b), nil
}

// Eight megabytes pasted into a program that reads slowly arrive whole,
// and what the host types meanwhile is neither lost nor kept waiting.
func TestAPasteIntoASlowProgramArrivesWhole(t *testing.T) {
	r := newRig(t, false)
	p := &slow{program: newProgram(), every: 200 * time.Microsecond, each: 4 << 10}
	term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	r.share("a", term)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	text := bytes.Repeat([]byte("0123456789abcdef"), 8<<20/16)
	var pasting sync.WaitGroup
	pasting.Go(func() {
		if n, err := pane.Write(text); n != len(text) || err != nil {
			t.Errorf("wrote %d: %v", n, err)
		}
	})
	const keys = 20
	for i := range keys {
		r.until("the paste goes on", func() bool { return len(p.typed()) > (i+1)*64<<10 })
		was := len(p.typed())
		start := time.Now()
		term.Send([]byte{3})
		r.until("the host's key arrives", func() bool { return strings.Count(p.typed()[was:], "\x03") > 0 })
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("the host's key took %v", took)
		}
		// What was handed over before it, and no more of what waits.
		if at := strings.Index(p.typed()[was:], "\x03"); at > 256<<10 {
			t.Errorf("the host's key came after %d bytes more", at)
		}
	}
	pasting.Wait()
	for end := time.Now().Add(60 * time.Second); len(p.typed()) < len(text)+keys; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(end) {
			t.Fatalf("%d bytes of %d arrived", len(p.typed()), len(text)+keys)
		}
	}
	if got := strings.ReplaceAll(p.typed(), "\x03", ""); got != string(text) {
		t.Error("it arrived wrong")
	}
}

// What a guest typed that the program has not taken is not typed after it
// may no more.
func TestWhatWaitsIsNotTypedAfterwards(t *testing.T) {
	r := newRig(t, false)
	p := &slow{program: newProgram(), every: 5 * time.Millisecond, each: 1 << 10}
	term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	r.share("a", term)
	for _, st := range []State{terminals(false, "a"), terminals(true)} {
		r.publish(terminals(true, "a"))
		if r.last == nil {
			r.in(key(t))
		}
		var pane *ShownPane
		r.until("it is opened", func() bool {
			time.Sleep(50 * time.Millisecond)
			pane, err = r.last.Pane(context.Background(), "a")
			return err == nil
		})
		was := len(p.typed())
		go func() { _, _ = pane.Write(make([]byte, 1<<20)) }()
		r.until("it types", func() bool { return len(p.typed()) > was })
		r.publish(st)
		// The one piece being handed over, and no more.
		was = len(p.typed())
		time.Sleep(100 * time.Millisecond)
		if got := len(p.typed()) - was; got > 2<<10 {
			t.Errorf("%d bytes more were typed", got)
		}
		_ = pane.Close()
	}
}

// What a guest typed for a program that ended is not typed into the one
// started in its place, however much of it is on a slow line, or waits at
// the guest behind that.
func TestTypedForOneProgramIsNotTypedIntoTheNext(t *testing.T) {
	r := newRig(t, true)
	dial := r.dial
	var line *memConn
	r.dial = func() net.Conn {
		line = dial().(*memConn)
		line.slow(125_000)
		return line
	}
	term, p := terminal(t, 80, 24)
	r.share("a", term)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	there := shownOn(t, pane)
	for range 3 {
		there.Paste(strings.Repeat("rm -rf old\n", 1<<20/11))
	}
	r.until("it types", func() bool { return len(p.typed()) > 0 })
	_ = p.Close()
	r.until("it ended", term.Exited)
	time.Sleep(300 * time.Millisecond)
	next := newProgram()
	if err := term.Restart(next); err != nil {
		t.Fatal(err)
	}
	for range 40 {
		time.Sleep(100 * time.Millisecond)
		if got := next.typed(); got != "" {
			t.Fatalf("%d bytes were typed into the next, beginning %.30q", len(got), got)
		}
	}
	// And what is typed from now is, on a line that has come to it.
	line.slow(100 << 20)
	r.until("the guest hears it started", func() bool { return !pane.Ended() })
	there.Paste("new")
	r.until("the guest types in the next", func() bool { return next.typed() == "new" })
}

// A program that ended and was started again before the guest was told of
// either still counts: what is typed for the one is not for the other.
func TestAProgramStartedInTheMeantimeCounts(t *testing.T) {
	r := newRig(t, false)
	src := &recorder{}
	r.share("a", src)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	f := src.follower(r)
	f.Ended()
	f.Started()
	// Not read yet, so not known of.
	_, _ = pane.Write([]byte("old"))
	go func() { _, _ = io.Copy(io.Discard, pane) }()
	r.until("the guest knows", func() bool { return pane.begun() == 1 })
	_, _ = pane.Write([]byte("new"))
	r.until("it types", func() bool { return src.got() > 0 })
	if got := string(src.sent); got != "new" {
		t.Errorf("%q was typed", got)
	}
}

// Frames of typing are cut between characters and keys.
func TestTypingIsSentInWholeKeys(t *testing.T) {
	for _, one := range []string{"漢", "é", "\x1b[1;5A", "\x1bOP", "🙂", "\x1b[<35;120;40M"} {
		var sent typedFrames
		p := &ShownPane{ch: pretendChannel{w: &sent}}
		text := "x" + strings.Repeat(one, 200<<10/len(one))
		if n, err := p.Write([]byte(text)); n != len(text) || err != nil {
			t.Fatalf("wrote %d of %d, %v", n, len(text), err)
		}
		if got := strings.Join(sent.frames, ""); got != text {
			t.Errorf("%q arrives wrong", one)
		}
		for i, f := range sent.frames {
			if f = strings.TrimPrefix(f, "x"); strings.ReplaceAll(f, one, "") != "" {
				t.Errorf("%q: frame %d of %d begins %.10q and ends %.10q", one, i, len(sent.frames), f, f[max(0, len(f)-10):])
				break
			}
		}
	}
}

// typedFrames keeps the bodies of the frames of typing written to it.
type typedFrames struct{ frames []string }

func (t *typedFrames) Write(b []byte) (int, error) {
	_, body, err := readFrame(bytes.NewReader(b), paneToHost, nil)
	if err == nil {
		_, text := splitTyped(body)
		t.frames = append(t.frames, string(text))
	}
	return len(b), err
}

// A guest that closes a pane can open it again, whatever it typed there
// that a slow program has not taken.
func TestAPaneClosedOnALongPasteIsOpenedAgain(t *testing.T) {
	r := newRig(t, false)
	p := &slow{program: newProgram(), every: 100 * time.Millisecond, each: 1 << 10}
	term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: 80, Rows: 24}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = term.Close() })
	r.share("a", term)
	r.publish(terminals(true, "a"))
	pane, err := r.in(key(t)).Pane(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pane.Write(make([]byte, 1<<20)); err != nil {
		t.Fatal(err)
	}
	r.until("it types", func() bool { return len(p.typed()) > 0 })
	_ = pane.Close()
	start := time.Now()
	r.until("it is opened again", func() bool {
		time.Sleep(50 * time.Millisecond)
		again, err := r.last.Pane(context.Background(), "a")
		if err == nil {
			_ = again.Close()
		}
		return err == nil
	})
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("it took %v", took)
	}
	// And what is left of the paste is not typed for long.
	time.Sleep(closedFor + 500*time.Millisecond)
	was := len(p.typed())
	time.Sleep(time.Second)
	if got := len(p.typed()) - was; got > 1<<10 {
		t.Errorf("%d bytes more were typed", got)
	}
}

// What was typed in a pane the guest has closed is taken away with the rest.
func TestAClosedPaneIsWithdrawnToo(t *testing.T) {
	for _, st := range []State{terminals(false, "a"), terminals(true)} {
		r := newRig(t, false)
		p := &slow{program: newProgram(), every: 5 * time.Millisecond, each: 1 << 10}
		term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: 80, Rows: 24}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = term.Close() })
		r.share("a", term)
		r.publish(terminals(true, "a"))
		pane, err := r.in(key(t)).Pane(context.Background(), "a")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pane.Write(make([]byte, 1<<20)); err != nil {
			t.Fatal(err)
		}
		r.until("it types", func() bool { return len(p.typed()) > 0 })
		_ = pane.Close()
		r.until("the host knows it is closed", func() bool {
			r.host.mu.Lock()
			defer r.host.mu.Unlock()
			return len(r.host.cur.streams) == 0
		})
		r.publish(st)
		was := len(p.typed())
		time.Sleep(200 * time.Millisecond)
		if got := len(p.typed()) - was; got > 2<<10 {
			t.Errorf("%d bytes more were typed", got)
		}
	}
}

// Callers that say nothing keep nobody out: neither who joins, nor who
// comes back.
func TestSilentCallersKeepNobodyOut(t *testing.T) {
	r := newRig(t, false)
	k := key(t)
	for round := range 2 {
		for range 3 * handshakesAtOnce {
			nc := r.dial()
			defer func() { _ = nc.Close() }()
		}
		time.Sleep(20 * time.Millisecond)
		start := time.Now()
		s, err := r.guest(k)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if took := time.Since(start); took > 3*time.Second {
			t.Errorf("round %d: it took %v", round, took)
		}
		if round == 0 {
			if err := s.Join(context.Background(), "Guest", ""); err != nil {
				t.Fatal(err)
			}
		}
		_ = s.Close()
	}
}

// Whoever calls most makes room for the rest: a guest part way through is
// not the one to go.
func TestRoomIsMadeByWhoeverCallsMost(t *testing.T) {
	r := newRig(t, false)
	from := func(addr string, n int) (conns []*from) {
		for range n {
			here, there := memPipe()
			c := &from{memConn: there, addr: addr, here: here}
			go r.host.handshake(c)
			r.until("it is taken on", func() bool {
				r.host.mu.Lock()
				defer r.host.mu.Unlock()
				return c.in.closed || r.arrivingLocked(c)
			})
			conns = append(conns, c)
		}
		return conns
	}
	guest := from("192.0.2.1:1", 1)
	noisy := from("198.51.100.9:1", 3*handshakesAtOnce)
	r.host.mu.Lock()
	if !r.arrivingLocked(guest[0]) {
		t.Error("the one that came first was dropped for those that call without end")
	}
	if r.arrivingLocked(noisy[0]) {
		t.Error("the oldest of those is there still")
	}
	r.host.mu.Unlock()
	for _, c := range append(guest, noisy...) {
		_ = c.Close()
	}
}

// from is a connection from an address.
type from struct {
	*memConn
	addr string
	here *memConn
}

func (c *from) RemoteAddr() net.Addr { return memAddr(c.addr) }

func (r *rig) arrivingLocked(nc net.Conn) bool {
	for _, a := range r.host.arriving {
		if a.nc == nc {
			return true
		}
	}
	return false
}

// A caller has seconds to prove its key, and no more.
func TestProvingAKeyHasSeconds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, true)
		nc := r.dial()
		start := time.Now()
		_, _ = io.ReadAll(nc)
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("a silent caller is held for %v", took)
		}
		_ = nc.Close()
	})
}

// Screens no window has are not held for anybody.
func TestScreensAreOfASizeThatIsOne(t *testing.T) {
	for _, size := range [][2]int{{mostCols + 1, 1}, {1, mostRows + 1}, {0xffff, 16}, {0, 5}, {5, 0}, {-1, 5}} {
		if goodSize(size[0], size[1]) {
			t.Errorf("%v is a size", size)
		}
	}
	if !goodSize(mostCols, mostRows) {
		t.Error("the largest is no size")
	}
}

// A host that sends many small things lets the browser have its thread as
// one that sends much does.
func TestManySmallFramesYield(t *testing.T) {
	for name, one := range map[string][]byte{
		"data":    frame(frameData, []byte("x")),
		"nothing": frame(frameData),
		"program": programFrame(true, 0),
		"screens": frame(frameScreen, []byte("\x00\x50\x00\x18x")),
	} {
		yields := 0
		p := &ShownPane{ch: pretendChannel{r: bytes.NewReader(bytes.Repeat(one, 5000))}, yield: func() { yields++ }}
		buf := make([]byte, 1024)
		for {
			if _, _, err := p.ReadScreen(buf); err != nil {
				break
			}
		}
		if yields < 10 {
			t.Errorf("%s: it yielded %d times", name, yields)
		}
	}
}

// pretendChannel is a channel with nobody at the other end.
type pretendChannel struct {
	ssh.Channel
	r io.Reader
	w io.Writer
}

func (c pretendChannel) Write(p []byte) (int, error) { return c.w.Write(p) }

func (c pretendChannel) Read(p []byte) (int, error)                     { return c.r.Read(p) }
func (c pretendChannel) SendRequest(string, bool, []byte) (bool, error) { return true, nil }
func (c pretendChannel) Close() error                                   { return nil }

// Once a guest has opened too many channels, none of the rest is looked
// at.
func TestNoChannelIsOpenedPastTheLimit(t *testing.T) {
	r := newRig(t, false)
	conn, err := r.raw(r.linkSigner())
	if err != nil {
		t.Fatal(err)
	}
	c := newHostConn(context.Background(), r.host, &ssh.ServerConn{Conn: conn}, roleLink)
	c.opens = newLimit(1, 0)
	chans := make(chan ssh.NewChannel, 3)
	tried := 0
	for range 3 {
		chans <- pretendNew{func() { tried++ }}
	}
	close(chans)
	var running sync.WaitGroup
	c.channels(&running, chans)
	running.Wait()
	if tried != 1 {
		t.Errorf("%d were answered", tried)
	}
}

type pretendNew struct{ answered func() }

func (n pretendNew) Accept() (ssh.Channel, <-chan *ssh.Request, error) {
	n.answered()
	return nil, nil, errors.New("pretend")
}
func (n pretendNew) Reject(ssh.RejectionReason, string) error { n.answered(); return nil }
func (n pretendNew) ChannelType() string                      { return "session" }
func (n pretendNew) ExtraData() []byte                        { return nil }

// On a line of a megabit, with a program that floods, what the guest types
// is at the program at once, and what comes of it is on its screen soon.
func TestASlowLineIsLive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, true)
		dial := r.dial
		r.dial = func() net.Conn {
			c := dial().(*memConn)
			c.slow(125_000)
			return c
		}
		p := newProgram()
		term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: 80, Rows: 24}})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = term.Close() }()
		r.share("a", term)
		r.publish(terminals(true, "a"))
		pane, err := r.in(key(t)).Pane(context.Background(), "a")
		if err != nil {
			t.Fatal(err)
		}
		there, err := uiterm.New(uiterm.Config{Session: pane})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = there.Close() }()
		// Four times what the line carries, until it is interrupted.
		stop := make(chan struct{})
		var writing sync.WaitGroup
		writing.Go(func() {
			line := bytes.Repeat([]byte("a line of a flood that nobody reads\r\n"), 550)
			for {
				select {
				case <-stop:
					p.out <- []byte("\r\nstopped\r\n$ ")
					return
				case p.out <- line:
					time.Sleep(40 * time.Millisecond)
				}
			}
		})
		time.Sleep(4 * time.Second)
		for range 3 {
			was := len(p.typed())
			start := time.Now()
			_, _ = pane.Write([]byte("x"))
			for len(p.typed()) == was {
				time.Sleep(time.Millisecond)
			}
			if took := time.Since(start); took > 500*time.Millisecond {
				t.Errorf("a key took %v to the program", took)
			}
			time.Sleep(300 * time.Millisecond)
		}
		start := time.Now()
		close(stop)
		for !strings.Contains(there.Text(), "stopped") {
			time.Sleep(10 * time.Millisecond)
			if time.Since(start) > time.Minute {
				break
			}
		}
		took := time.Since(start)
		t.Logf("the flood was seen to stop after %v", took)
		if took > 3*time.Second {
			t.Errorf("the guest saw the flood stop after %v", took)
		}
		writing.Wait()
		_ = r.last.Close()
		r.stop()
		// For what is on the line to have come.
		time.Sleep(time.Minute)
	})
}

// A guest is sent no more than it has taken, but for a little.
func TestLittleIsOnItsWay(t *testing.T) {
	r := newRig(t, false)
	src := &recorder{}
	r.share("a", src)
	r.publish(terminals(true, "a"))
	k := key(t)
	_ = r.in(k).Close()
	conn, err := r.raw(signer(t, k))
	if err != nil {
		t.Fatal(err)
	}
	control, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(io.Discard, control) }()
	r.until("it is here", func() bool { return r.host.Status().Here })
	ch, reqs, err := conn.OpenChannel(chanPane, ssh.Marshal(openPane{"a"}))
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	f := src.follower(r)
	var mu sync.Mutex
	got := 0
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := ch.Read(buf)
			mu.Lock()
			got += n
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	read := func() int { mu.Lock(); defer mu.Unlock(); return got }
	for range 200 {
		f.Write(make([]byte, 16<<10))
		time.Sleep(time.Millisecond)
	}
	if n := read(); n == 0 || n > mostOnItsWay {
		t.Errorf("%d bytes came to a guest that says it has taken none", n)
	}
	// It is sent more as it says so, and as much as it says.
	was := read()
	if _, err := ch.SendRequest(reqTook, false, ssh.Marshal(took{uint32(was)})); err != nil {
		t.Fatal(err)
	}
	r.until("more comes", func() bool { return read() > was })
	// And one that says it has taken what it was not sent is hung up on.
	if _, err := ch.SendRequest(reqTook, false, ssh.Marshal(took{1 << 30})); err != nil {
		t.Fatal(err)
	}
	_ = conn.Wait()
}
