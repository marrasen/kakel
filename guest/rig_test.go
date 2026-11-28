package guest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/ui"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// memConn is one end of a connection held in memory, which keeps what is
// written until it is read, as a socket does: a net.Pipe has both ends of
// an SSH handshake wait for each other.
type memConn struct {
	in, out *memHalf
}

type memHalf struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
	// lost drops what is written, as a line that is cut does.
	lost bool
	// rate is how many bytes a second it carries, zero for as many as come.
	rate int
}

func newHalf() *memHalf {
	h := &memHalf{}
	h.cond = sync.NewCond(&h.mu)
	return h
}

func memPipe() (a, b *memConn) {
	x, y := newHalf(), newHalf()
	return &memConn{in: x, out: y}, &memConn{in: y, out: x}
}

func (c *memConn) Read(p []byte) (int, error) {
	c.in.mu.Lock()
	defer c.in.mu.Unlock()
	for len(c.in.buf) == 0 {
		if c.in.closed {
			return 0, io.EOF
		}
		c.in.cond.Wait()
	}
	n := copy(p, c.in.buf)
	c.in.buf = c.in.buf[n:]
	c.in.cond.Broadcast()
	if rate := c.in.rate; rate > 0 {
		// Only the one that reads waits for it, with no lock held.
		c.in.mu.Unlock()
		time.Sleep(time.Duration(n) * time.Second / time.Duration(rate))
		c.in.mu.Lock()
	}
	return n, nil
}

func (c *memConn) Write(p []byte) (int, error) {
	c.out.mu.Lock()
	defer c.out.mu.Unlock()
	// A slow line has much room on the way, as a relay's and a system's
	// buffers are.
	most := 256 << 10
	if c.out.rate > 0 {
		most = 16 << 20
	}
	for len(c.out.buf) > most && !c.out.closed && !c.out.lost {
		c.out.cond.Wait()
	}
	if c.out.closed {
		return 0, io.ErrClosedPipe
	}
	if !c.out.lost {
		c.out.buf = append(c.out.buf, p...)
		c.out.cond.Broadcast()
	}
	return len(p), nil
}

func (c *memConn) Close() error {
	for _, h := range []*memHalf{c.in, c.out} {
		h.mu.Lock()
		// A line that is cut carries no news of the end either.
		if !h.lost || h == c.in {
			h.closed = true
		}
		if h == c.in {
			// Nobody reads what came.
			h.buf = nil
		}
		h.cond.Broadcast()
		h.mu.Unlock()
	}
	return nil
}

// slow carries what comes to this end and goes from it at rate.
func (c *memConn) slow(rate int) {
	for _, h := range []*memHalf{c.in, c.out} {
		h.mu.Lock()
		h.rate = rate
		h.mu.Unlock()
	}
}

// cut loses everything from now on, both ways.
func (c *memConn) cut() {
	for _, h := range []*memHalf{c.in, c.out} {
		h.mu.Lock()
		h.lost, h.buf = true, nil
		h.cond.Broadcast()
		h.mu.Unlock()
	}
}

type memAddr string

func (memAddr) Network() string  { return "mem" }
func (a memAddr) String() string { return string(a) }

func (*memConn) LocalAddr() net.Addr              { return memAddr("host") }
func (*memConn) RemoteAddr() net.Addr             { return memAddr("192.0.2.7:1234") }
func (*memConn) SetDeadline(time.Time) error      { return nil }
func (*memConn) SetReadDeadline(time.Time) error  { return nil }
func (*memConn) SetWriteDeadline(time.Time) error { return nil }

// memListener hands over the far ends of what is dialled.
type memListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newMemListener() *memListener {
	return &memListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

func (l *memListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *memListener) Close() error { l.once.Do(func() { close(l.done) }); return nil }

func (*memListener) Addr() net.Addr { return memAddr("host") }

// dial is a connection to the host, and an ended one when there is none.
func (l *memListener) dial() *memConn {
	here, there := memPipe()
	select {
	case l.conns <- there:
	case <-l.done:
		_ = there.Close()
	}
	return here
}

// rig is a host with what it is asked and told kept.
type rig struct {
	t    *testing.T
	host *Host
	dial func() net.Conn

	mu      sync.Mutex
	answer  chan bool
	asked   []Request
	opened  []string
	panes   map[string]PaneSource
	statuss []Status
	errs    []error
	serving sync.WaitGroup
	// last is the guest last let in.
	last *Session
}

// newRig serves a share, over TCP, or in memory for a test of time.
func newRig(t *testing.T, mem bool) *rig {
	t.Helper()
	r := &rig{t: t, answer: make(chan bool, 1), panes: map[string]PaneSource{}}
	host, err := NewHost(HostConfig{
		Ask: func(ctx context.Context, q Request) bool {
			r.mu.Lock()
			r.asked = append(r.asked, q)
			r.mu.Unlock()
			select {
			case yes := <-r.answer:
				return yes
			case <-ctx.Done():
				return false
			}
		},
		Open: func(id string) (PaneSource, error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.opened = append(r.opened, id)
			if src := r.panes[id]; src != nil {
				return src, nil
			}
			return nil, errors.New("no such pane")
		},
		OnChange: func(st Status) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.statuss = append(r.statuss, st)
		},
		OnError: func(err error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.errs = append(r.errs, err)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.host = host
	var ln net.Listener
	if mem {
		m := newMemListener()
		ln, r.dial = m, func() net.Conn { return m.dial() }
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		r.dial = func() net.Conn {
			nc, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				// As a host that is gone looks.
				a, b := memPipe()
				_ = b.Close()
				return a
			}
			return nc
		}
	}
	r.serving.Go(func() {
		if err := host.Serve(ln); err != nil {
			t.Errorf("serve: %v", err)
		}
	})
	t.Cleanup(r.stop)
	return r
}

func (r *rig) stop() {
	r.host.End(ReasonStopped)
	r.serving.Wait()
}

// key is a guest's key.
func key(t *testing.T) Key {
	t.Helper()
	k, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// guest dials with k.
func (r *rig) guest(k Key) (*Session, error) {
	return Dial(context.Background(), r.dial(), Options{Secret: r.host.Secret(), Key: k})
}

// in is a guest the host let in.
func (r *rig) in(k Key) *Session {
	r.t.Helper()
	s := r.joined(k)
	r.answer <- true
	r.until("the guest is let in", func() bool { return s.View().Phase == PhaseIn })
	r.last = s
	return s
}

// joined is a guest that waits.
func (r *rig) joined(k Key) *Session {
	r.t.Helper()
	s, err := r.guest(k)
	if err != nil {
		r.t.Fatalf("dial: %v", err)
	}
	r.t.Cleanup(func() { _ = s.Close() })
	if err := s.Join(context.Background(), "Guest", "a browser"); err != nil {
		r.t.Fatalf("join: %v", err)
	}
	return s
}

func (r *rig) until(what string, ok func() bool) {
	r.t.Helper()
	for end := time.Now().Add(10 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(end) {
			r.t.Fatalf("never: %s", what)
		}
	}
}

func (r *rig) ended(s *Session, want error) {
	r.t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		r.t.Fatalf("the guest was never told %v", want)
	}
	if err := s.Err(); !errors.Is(err, want) {
		r.t.Fatalf("the guest was told %v, want %v", err, want)
	}
}

func (r *rig) publish(st State) {
	r.t.Helper()
	if err := r.host.Publish(st); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) share(id string, src PaneSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.panes[id] = src
}

// terminals is a state of terminals by those ids.
func terminals(canType bool, ids ...string) State {
	st := State{HostName: "anna", CanType: canType}
	for _, id := range ids {
		st.Panes = append(st.Panes, Pane{ID: id, Title: id, Kind: KindTerminal, Cols: 80, Rows: 24})
	}
	return st
}

// raw is an SSH client with a key, as somebody's own program might be.
func (r *rig) raw(signer ssh.Signer) (ssh.Conn, error) {
	nc := r.dial()
	conn, chans, reqs, err := ssh.NewClientConn(nc, "", &ssh.ClientConfig{
		User: "guest", Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // the host is this test's own
	})
	if err != nil {
		_ = nc.Close()
		return nil, err
	}
	go ssh.DiscardRequests(reqs)
	go func() {
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "")
		}
	}()
	r.t.Cleanup(func() { _ = conn.Close() })
	return conn, nil
}

func (r *rig) linkSigner() ssh.Signer {
	r.t.Helper()
	s, err := r.host.Secret().linkKey()
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func signer(t *testing.T, k Key) ssh.Signer {
	t.Helper()
	s, err := ssh.NewSignerFromKey(k.private())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// program is a session a test plays the program of.
type program struct {
	out  chan []byte
	left []byte
	gone chan struct{}
	once sync.Once

	mu   sync.Mutex
	took []byte
}

func newProgram() *program { return &program{out: make(chan []byte, 64), gone: make(chan struct{})} }

func (p *program) Read(b []byte) (int, error) {
	if len(p.left) == 0 {
		select {
		case p.left = <-p.out:
		case <-p.gone:
			select {
			case p.left = <-p.out:
			default:
				return 0, io.EOF
			}
		}
	}
	n := copy(b, p.left)
	p.left = p.left[n:]
	return n, nil
}

func (p *program) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.took = append(p.took, b...)
	return len(b), nil
}

func (p *program) typed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return string(p.took)
}

func (p *program) Resize(int, int) error { return nil }
func (p *program) Wait() error           { <-p.gone; return nil }
func (p *program) Close() error          { p.once.Do(func() { close(p.gone) }); return nil }

// terminal is a terminal with a program of the test's.
func terminal(t *testing.T, cols, rows int) (*uiterm.Terminal, *program) {
	t.Helper()
	p := newProgram()
	term, err := uiterm.New(uiterm.Config{Session: p, Size: ui.Size{Cols: cols, Rows: rows}})
	if err != nil {
		t.Fatal(err)
	}
	term.Layout(ui.Size{Cols: cols, Rows: rows})
	t.Cleanup(func() { _ = term.Close() })
	return term, p
}

// say has the program write, and waits for the terminal to have it. The
// title says it has.
func say(r *rig, term *uiterm.Terminal, p *program, text, title string) {
	r.t.Helper()
	p.out <- fmt.Appendf(nil, "%s\x1b]2;%s\x07", text, title)
	r.until("the terminal takes "+title, func() bool { return term.Title() == title })
}
