package guest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// Options are what a guest dials with.
type Options struct {
	// Secret is what the link carries.
	Secret Secret

	// Key is the guest's own. It is kept for as long as the guest may come
	// back, and the same key each time is what makes it the same guest.
	Key Key

	// Yield is called now and then while a pane's output is read, and
	// returns once the browser has had its thread: see the package. Nil
	// for a program that has threads.
	Yield func()
}

// View is the share as its guest has it now.
type View struct {
	// Phase is PhaseJoin until Join, PhaseWaiting while the host is asked,
	// and PhaseIn with State the window, from when the host said yes.
	Phase Phase
	State State
}

// Session is a guest's end of one connection.
type Session struct {
	conn ssh.Conn
	// gone is closed once the connection has gone.
	gone  chan struct{}
	ch    ssh.Channel
	out   *latest
	key   Key
	yield func()
	// large is had by the pane whose large screen is being read, or drawn.
	large chan struct{}

	// changed has word when the view or the end is new.
	changed chan struct{}
	// moved is closed when the phase moves on, and over when it is over.
	moved, over chan struct{}

	mu   sync.Mutex
	view View
	err  error
	// left says the guest has left, however the connection ends then.
	left bool
}

// Dial says hello to the host over nc, and returns once it has said how
// the share stands: PhaseJoin for a link to join with, and PhaseWaiting or
// PhaseIn for a key that has joined before. A page dials as Join is
// clicked rather than as it is opened: a connection that does not join is
// soon hung up on.
//
// The error is ErrUsed, ErrExpired or another EndedError, ErrWrongHost,
// ErrDenied, or ErrLost for one worth another try. nc is closed unless
// there is a Session to do it.
func Dial(ctx context.Context, nc net.Conn, o Options) (*Session, error) {
	s, err := dial(ctx, nc, o)
	if err != nil {
		_ = nc.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return s, nil
}

func dial(ctx context.Context, nc net.Conn, o Options) (*Session, error) {
	if o.Key == (Key{}) {
		return nil, errors.New("a guest needs a key of its own")
	}
	own, err := ssh.NewSignerFromKey(o.Key.private())
	if err != nil {
		return nil, fmt.Errorf("the guest's key: %w", err)
	}
	link, err := o.Secret.linkKey()
	if err != nil {
		return nil, err
	}
	// Whatever is slow, the connection goes when the caller gives up.
	stop := context.AfterFunc(ctx, func() { _ = nc.Close() })
	defer stop()
	late := time.AfterFunc(dialWindow, func() { _ = nc.Close() })
	defer late.Stop()

	live := newHeard(nc)
	var wrong error
	conn, chans, reqs, err := ssh.NewClientConn(live, "", &ssh.ClientConfig{
		Config: algorithms(),
		User:   "guest",
		// Its own first, which the host knows once it has joined.
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(own, link)},
		HostKeyAlgorithms: keyAlgorithms,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			if !o.Secret.madeBy(key) {
				wrong = ErrWrongHost
			}
			return wrong
		},
		ClientVersion: version,
	})
	switch {
	case wrong != nil:
		return nil, wrong
	case err != nil && strings.Contains(err.Error(), "unable to authenticate"):
		return nil, ErrDenied
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrLost, err)
	}
	every := newLimit(requestsBurst, requestsRate)
	go discard(conn, every, reqs)
	go func() {
		// The host opens nothing.
		for nch := range chans {
			_ = nch.Reject(ssh.Prohibited, "a guest is asked for nothing")
		}
	}()
	gone := make(chan struct{})
	go func() {
		_ = conn.Wait()
		close(gone)
	}()
	ch, chReqs, err := openChannel(ctx, conn, gone, chanControl, nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %w", ErrLost, err)
	}
	go discard(conn, every, chReqs)

	s := &Session{
		conn: conn, gone: gone, ch: ch, out: newLatest(ch), key: o.Key, yield: o.Yield,
		large:   make(chan struct{}, 1),
		changed: make(chan struct{}, 1), moved: make(chan struct{}), over: make(chan struct{}),
	}
	if s.yield == nil {
		s.yield = func() {}
	}
	first := make(chan struct{})
	go s.listen(first)
	go s.out.run()
	alive, dead := context.WithCancel(context.Background())
	go stayAlive(alive, conn, live)
	go func() {
		<-gone
		dead()
		s.out.stop()
	}()
	select {
	case <-first:
		return s, nil
	case <-s.over:
		return nil, s.Err()
	}
}

// openChannel opens a channel on conn, and gives up when ctx ends or the
// connection has gone: a channel asked for in the moment the connection
// goes is never answered, not even with that.
func openChannel(ctx context.Context, conn ssh.Conn, gone <-chan struct{}, kind string, payload []byte) (ssh.Channel, <-chan *ssh.Request, error) {
	type opened struct {
		ch   ssh.Channel
		reqs <-chan *ssh.Request
		err  error
	}
	got := make(chan opened, 1)
	go func() {
		ch, reqs, err := conn.OpenChannel(kind, payload)
		got <- opened{ch, reqs, err}
	}()
	select {
	case o := <-got:
		return o.ch, o.reqs, o.err
	case <-gone:
		return nil, nil, io.EOF
	case <-ctx.Done():
		go func() {
			// Opened for nobody.
			select {
			case o := <-got:
				if o.err == nil {
					go ssh.DiscardRequests(o.reqs)
					_ = o.ch.Close()
				}
			case <-gone:
			}
		}()
		return nil, nil, ctx.Err()
	}
}

// listen takes what the host says until it is over. first is closed when
// the host has said how the share stands.
func (s *Session) listen(first chan struct{}) {
	var buf []byte
	for {
		kind, body, err := readFrame(s.ch, controlToGuest, buf)
		if err != nil {
			s.finish(fmt.Errorf("%w: %w", ErrLost, err))
			return
		}
		buf = body
		switch kind {
		case frameJoin:
			s.show(View{Phase: PhaseJoin})
		case frameWaiting:
			s.show(View{Phase: PhaseWaiting})
		case frameState:
			st, err := decodeState(body)
			if err != nil {
				s.finish(fmt.Errorf("%w: %w", ErrLost, err))
				return
			}
			s.show(View{Phase: PhaseIn, State: st})
		case frameEnded:
			var e ended
			if err := json.Unmarshal(body, &e); err != nil || e.Reason == "" {
				e.Reason = ReasonStopped
			}
			s.finish(&EndedError{Reason(plain(string(e.Reason), mostName))})
			return
		}
		if first != nil {
			close(first)
			first = nil
		}
	}
}

// show makes v the view. Nothing goes back to an earlier phase.
func (s *Session) show(v View) {
	s.mu.Lock()
	if s.view.Phase != PhaseOver && v.Phase >= s.view.Phase {
		if v.Phase != s.view.Phase {
			close(s.moved)
			s.moved = make(chan struct{})
		}
		s.view = v
	}
	s.mu.Unlock()
	nudge(s.changed)
}

// finish ends the session, with why it did. The first says it.
func (s *Session) finish(err error) {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return
	}
	if s.left {
		err = &EndedError{ReasonLeft}
	}
	s.err = err
	s.view.Phase = PhaseOver
	close(s.over)
	s.mu.Unlock()
	nudge(s.changed)
	s.out.stop()
	_ = s.conn.Close()
}

// View is the share now. Changed has word each time it is another, and
// when the session ends.
func (s *Session) View() View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.view
}

// Changed has word when there is another View, however many there were.
func (s *Session) Changed() <-chan struct{} { return s.changed }

// Done is closed when the session has ended, and Err is why: an
// EndedError the host said, or ErrLost for a connection that went, for
// another Dial with the same key.
func (s *Session) Done() <-chan struct{} { return s.over }

// Err is why the session ended, and nil while it has not.
func (s *Session) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// Join joins as name, with about the browser and the system, and returns
// once the host is being asked. From then the link is spent, and the key
// is how the guest comes back.
func (s *Session) Join(ctx context.Context, name, about string) error {
	if name = plainName(name); name == "" {
		return errors.New("a guest needs a name")
	}
	own, err := ssh.NewPublicKey(s.key.private().Public())
	if err != nil {
		return fmt.Errorf("the guest's key: %w", err)
	}
	body, err := json.Marshal(joining{Name: name, About: plain(about, mostAbout), Key: own.Marshal()})
	if err != nil {
		return fmt.Errorf("join: %w", err)
	}
	s.mu.Lock()
	phase, moved := s.view.Phase, s.moved
	s.mu.Unlock()
	if phase == PhaseJoin {
		if err := s.say(frame(frameName, body)); err != nil {
			select {
			case <-s.over:
				// The host's reason, which came before the write failed.
				return s.Err()
			case <-time.After(sayEndedIn):
				return fmt.Errorf("%w: %w", ErrLost, err)
			}
		}
		select {
		case <-moved:
		case <-s.over:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Err()
}

// say writes one frame, in turn with what out writes.
func (s *Session) say(frame []byte) error {
	s.out.writeMu.Lock()
	defer s.out.writeMu.Unlock()
	_, err := s.ch.Write(frame)
	return err
}

// Follow tells the host that the guest follows it, and is in whichever
// pane the host is in until Focus says otherwise. It does not wait.
func (s *Session) Follow() { s.out.put(frame(frameFollow)) }

// Focus tells the host which pane the guest picked, "" for none, and that
// it follows the host no more. It does not wait.
func (s *Session) Focus(id string) {
	if len(id) <= mostID {
		s.out.put(frame(frameFocus, []byte(id)))
	}
}

// Leave ends the share, as the guest's own end of it, and closes the
// session.
func (s *Session) Leave() {
	s.mu.Lock()
	s.left = true
	s.mu.Unlock()
	s.out.stop()
	if s.say(frame(frameLeave)) == nil {
		// The host says the last word.
		t := time.NewTimer(sayEndedIn)
		defer t.Stop()
		select {
		case <-s.over:
		case <-t.C:
		}
	}
	s.finish(&EndedError{ReasonLeft})
}

// Close closes the connection, and the share waits for the guest to come
// back.
func (s *Session) Close() error {
	s.finish(ErrLost)
	return nil
}
