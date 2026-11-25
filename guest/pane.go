package guest

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/marrasen/kakel/session"
	uiterm "github.com/marrasen/kakel/ui/term"
)

// yieldEvery is how much of a pane's output is read between yields, and
// yieldEach what each thing read counts for besides its bytes.
const (
	yieldEvery = 64 << 10
	yieldEach  = 256

	// largeScreen is how large a screen is for only one such to be held at
	// a time, from when it is read until its pane is read again: all the
	// panes open may have one, and a page has no room for that.
	largeScreen = 1 << 20
)

// ShownPane is a pane of the shared window, open at the guest: a session
// for a terminal to be on.
//
// It hands over screens as well as output, so a terminal on it has the
// size of the host's, changed at the very place in the output the host's
// was. It goes on through the program ending and another being started in
// its place, and ends when the pane leaves the window, or the connection
// goes. A terminal it has ended under takes the one opened after, with
// Restart.
type ShownPane struct {
	ch    ssh.Channel
	over  chan struct{}
	yield func()
	large chan struct{}

	// What is left of the frame being read, how much has been read since
	// the last yield and since the host was told, and what is left of a
	// screen for Read.
	left, since, taken int
	draw               []byte

	mu    sync.Mutex
	ended bool
	// count is which program it is, as frameProgram counts, and next the
	// count that is to be once the terminal has heard of it.
	count, next uint32
	// holds says a large screen of the pane's is held, and closed that the
	// pane is.
	holds, closed bool

	closing sync.Once
}

var _ session.ScreenReader = (*ShownPane)(nil)

// Pane opens a pane of the window, a terminal by its ID. The error is
// ErrNotShared for one the host does not show.
func (s *Session) Pane(ctx context.Context, id string) (*ShownPane, error) {
	ch, reqs, err := openChannel(ctx, s.conn, s.gone, chanPane, ssh.Marshal(openPane{id}))
	var no *ssh.OpenChannelError
	switch {
	case ctx.Err() != nil && err != nil:
		return nil, ctx.Err()
	case errors.As(err, &no):
		return nil, fmt.Errorf("%w: %s", ErrNotShared, plain(no.Message, mostTitle))
	case err != nil:
		return nil, fmt.Errorf("%w: %w", ErrLost, err)
	}
	p := &ShownPane{ch: ch, over: make(chan struct{}), yield: s.yield, large: s.large}
	go func() {
		// Ends as the channel does.
		ssh.DiscardRequests(reqs)
		close(p.over)
	}()
	return p, nil
}

// Mirrors says that another terminal runs the program and answers what it
// asks: see session.Mirrors.
func (p *ShownPane) Mirrors() bool { return true }

// Ended reports whether the pane's program has ended, as of what has been
// read.
func (p *ShownPane) Ended() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ended
}

// ReadScreen implements session.ScreenReader.
func (p *ShownPane) ReadScreen(b []byte) (int, *session.Screen, error) {
	p.mu.Lock()
	p.count = p.next
	p.mu.Unlock()
	// What it was given is drawn.
	p.letGo()
	for {
		// Whatever was read before has been drawn, as it is asked for more.
		p.rest()
		if p.left > 0 && len(b) > 0 {
			break
		}
		kind, n, err := readHeader(p.ch, paneToGuest)
		if err != nil {
			return 0, nil, p.fault(err)
		}
		p.read(headerLen)
		switch kind {
		case frameData:
			p.left = n
		case frameScreen:
			return p.screen(n)
		case frameProgram:
			var body [programLen]byte
			if n != len(body) {
				return 0, nil, p.fault(errFrame)
			}
			if _, err := io.ReadFull(p.ch, body[:]); err != nil {
				return 0, nil, p.fault(err)
			}
			p.read(n)
			p.mu.Lock()
			p.ended = body[0] == 0
			p.next = binary.BigEndian.Uint32(body[1:])
			started := p.next != p.count
			p.mu.Unlock()
			if started {
				// The terminal throws away what it has not written yet,
				// which a write of the next count would send.
				return 0, &session.Screen{Started: true}, nil
			}
		}
	}
	n, err := p.ch.Read(b[:min(len(b), p.left)])
	p.left -= n
	p.read(n)
	if err != nil {
		err = p.fault(err)
	}
	return n, nil, err
}

// read counts n bytes read. Each time counts for what it takes besides, so
// that many small things yield as much does.
func (p *ShownPane) read(n int) {
	p.taken += n
	p.since += n + yieldEach
}

// rest lets the browser have its thread, and the host know what has been
// taken, when it is time for either.
func (p *ShownPane) rest() {
	if p.taken >= tookEvery {
		word := took{uint32(p.taken)}
		p.taken = 0
		// A connection that went says so when it is read next.
		_, _ = p.ch.SendRequest(reqTook, false, ssh.Marshal(word))
	}
	if p.since >= yieldEvery {
		p.since = 0
		p.yield()
	}
}

// screen reads the rest of a screen of n bytes.
func (p *ShownPane) screen(n int) (int, *session.Screen, error) {
	if n < 4 {
		return 0, nil, p.fault(errFrame)
	}
	if n > largeScreen && !p.hold() {
		return 0, nil, io.EOF
	}
	body := make([]byte, 0, min(n, 16<<10))
	for len(body) < n {
		have := len(body)
		body = append(body, make([]byte, min(n-have, 16<<10))...)
		got, err := io.ReadFull(p.ch, body[have:])
		if err != nil {
			return 0, nil, p.fault(err)
		}
		p.read(got)
		p.rest()
	}
	scr := &session.Screen{
		Cols: int(binary.BigEndian.Uint16(body)),
		Rows: int(binary.BigEndian.Uint16(body[2:])),
		Draw: body[4:],
	}
	if !goodSize(scr.Cols, scr.Rows) {
		return 0, nil, p.fault(errFrame)
	}
	// Making a screen takes its time, however little draws it.
	p.since = yieldEvery
	return 0, scr, nil
}

// hold waits until no other pane's large screen is held, and reports
// false for a pane closed meanwhile.
func (p *ShownPane) hold() bool {
	select {
	case p.large <- struct{}{}:
	case <-p.over:
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		<-p.large
		return false
	}
	p.holds = true
	return true
}

// letGo lets go of the pane's large screen, where one is held.
func (p *ShownPane) letGo() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.holds {
		p.holds = false
		<-p.large
	}
}

// fault is err as a terminal is told it: a pane that is shown no more has
// ended, however it was cut off, and a host that makes no sense has not.
func (p *ShownPane) fault(err error) error {
	p.letGo()
	if errors.Is(err, errFrame) {
		_ = p.Close()
		return err
	}
	return io.EOF
}

// Read reads the output and the screens as one stream, for a caller that
// minds no sizes.
func (p *ShownPane) Read(b []byte) (int, error) {
	for len(p.draw) == 0 {
		n, scr, err := p.ReadScreen(b)
		if scr == nil || err != nil {
			return n, err
		}
		p.draw = scr.Draw
	}
	n := copy(b, p.draw)
	p.draw = p.draw[n:]
	return n, nil
}

// Write types b at the host, which drops it while the guest may not type.
//
// It is for the program there is as Write is called. Once that has ended,
// the rest of b is for nobody, and Write is done with it.
func (p *ShownPane) Write(b []byte) (int, error) {
	const most = 16 << 10
	count := p.begun()
	for rest := b; len(rest) > 0; {
		if p.Ended() || p.begun() != count {
			return len(b), nil
		}
		n := uiterm.CutTyped(rest, most)
		if _, err := p.ch.Write(typedFrame(count, rest[:n])); err != nil {
			return len(b) - len(rest), err
		}
		rest = rest[n:]
	}
	return len(b), nil
}

// begun is which program the pane shows, as frameProgram counts.
func (p *ShownPane) begun() uint32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.count
}

// Resize does nothing: the size is the host's.
func (p *ShownPane) Resize(int, int) error { return nil }

// Wait waits for the pane to be shown no more. How its program ended is
// the host's to know.
func (p *ShownPane) Wait() error {
	<-p.over
	return nil
}

// Close stops showing the pane.
func (p *ShownPane) Close() error {
	p.closing.Do(func() {
		_ = p.ch.Close()
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		p.letGo()
	})
	return nil
}
