package guest

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// Each end asks the other every aliveEvery whether it is there, and takes
// it for gone when nothing at all has come from it for goneAfter. Anything
// counts, not only the answer: on a slow line the question waits behind
// what was sent before it, and the far end taking that is heard meanwhile.
const (
	aliveEvery = 15 * time.Second
	goneAfter  = 40 * time.Second
)

// heard is a connection that knows when something last came over it.
type heard struct {
	net.Conn
	at atomic.Int64
}

func newHeard(nc net.Conn) *heard {
	h := &heard{Conn: nc}
	h.at.Store(time.Now().UnixNano())
	return h
}

func (h *heard) Read(p []byte) (int, error) {
	n, err := h.Conn.Read(p)
	if n > 0 {
		h.at.Store(time.Now().UnixNano())
	}
	return n, err
}

// stayAlive asks until ctx ends, and closes conn when the far end is gone.
func stayAlive(ctx context.Context, conn ssh.Conn, h *heard) {
	t := time.NewTicker(aliveEvery)
	defer t.Stop()
	var asking atomic.Bool
	var asked sync.WaitGroup
	defer asked.Wait()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if time.Since(time.Unix(0, h.at.Load())) > goneAfter {
			_ = conn.Close()
			return
		}
		if asking.CompareAndSwap(false, true) {
			// The answer may not come, so it is not waited for here.
			asked.Go(func() {
				defer asking.Store(false)
				_, _, _ = conn.SendRequest(reqAlive, true, nil)
			})
		}
	}
}

// discard answers the requests of a connection or a channel with no, and
// closes conn when there are more of them than every allows. Unread, a
// few of them would stop the connection.
func discard(conn ssh.Conn, every *limit, reqs <-chan *ssh.Request) {
	for req := range reqs {
		if !every.try(1) {
			_ = conn.Close()
			continue
		}
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}

// latest writes frames to one channel, and only the latest of those put
// while it was writing: a far end on a slow line is behind by one, and
// whoever puts one never waits.
type latest struct {
	ch ssh.Channel

	// writeMu orders the writes, and is held across taking what to write,
	// so a last word is the last.
	writeMu sync.Mutex

	mu sync.Mutex
	// next is the latest of those put, and also the latest of those put
	// aside.
	next, also []byte
	stopped    bool
	wake       chan struct{}
}

func newLatest(ch ssh.Channel) *latest { return &latest{ch: ch, wake: make(chan struct{}, 1)} }

// put leaves a frame to be written.
func (l *latest) put(frame []byte) {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.next = frame
	l.mu.Unlock()
	nudge(l.wake)
}

// putAside leaves a frame to be written, of a kind of its own: the latest
// of these does not stand in place of the latest put.
func (l *latest) putAside(frame []byte) {
	l.mu.Lock()
	if l.stopped {
		l.mu.Unlock()
		return
	}
	l.also = frame
	l.mu.Unlock()
	nudge(l.wake)
}

// stop writes no more of what was put.
func (l *latest) stop() {
	l.mu.Lock()
	l.stopped, l.next, l.also = true, nil, nil
	l.mu.Unlock()
	nudge(l.wake)
}

// run writes until it is stopped or a write fails.
func (l *latest) run() {
	for range l.wake {
		l.writeMu.Lock()
		l.mu.Lock()
		next, also, stopped := l.next, l.also, l.stopped
		l.next, l.also = nil, nil
		l.mu.Unlock()
		var err error
		for _, f := range [][]byte{next, also} {
			if f != nil && err == nil {
				_, err = l.ch.Write(f)
			}
		}
		l.writeMu.Unlock()
		if stopped || err != nil {
			return
		}
	}
}

// last stops, and writes frame as the last there is.
func (l *latest) last(frame []byte) error {
	l.stop()
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	if _, err := l.ch.Write(frame); err != nil {
		return err
	}
	return l.ch.CloseWrite()
}

// nudge leaves word on a channel of one, where one word is as good as two.
func nudge(wake chan struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}
