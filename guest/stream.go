package guest

import (
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	uiterm "github.com/marrasen/kakel/ui/term"
)

const (
	// mostBehind is how much output is held for a guest that does not take
	// it as fast as it comes. Past it what is held is thrown away, and the
	// screen is sent instead, once there is room on the way.
	mostBehind = 1 << 20

	// Output that has waited lateAfter, when there is more than lateMost of
	// it, is thrown away the same: on a slow line the screen is there
	// sooner than what led to it.
	lateAfter = 250 * time.Millisecond
	lateMost  = 64 << 10

	// mostOnItsWay is how much is sent that the guest has not said it took.
	// SSH's own window is two megabytes, which a slow line is long over.
	mostOnItsWay = 128 << 10

	// pieceMost is how much goes into one write.
	pieceMost = 16 << 10

	// closedFor is how long what was typed in a pane may go on being typed
	// once the guest has closed it.
	closedFor = time.Second

	// screenEvery is how often at most a screen is sent again: drawing one
	// holds up the terminal it is of.
	screenEvery = 50 * time.Millisecond
)

// stream is one pane as it is sent to the guest, and what the guest types
// in it.
//
// The terminal hands it what its program writes, and never waits: at most
// one screen and mostBehind of output are held here.
type stream struct {
	c  *hostConn
	id string

	// ready says there is something to write, or room to.
	ready chan struct{}
	// quit is closed when it is done.
	quit chan struct{}

	mu   sync.Mutex
	src  PaneSource
	stop func()
	// lane is what is typed on for the program there is, and nil while
	// there is none. The next has another, so nothing typed for one is
	// handed to the other.
	lane uiterm.Lane
	// screen is one waiting to be sent, as a frame, and data the output
	// after it.
	screen, data []byte
	// The output that came up to since has been sent once behind is zero:
	// it is how much of data that is.
	behind int
	since  time.Time
	// stale says neither is any use: the screen has to be asked for. large
	// says the screen is one no guest is sent, so neither is its output.
	stale, large bool
	// ended says the program has ended, and count how many have been
	// started in another's place. said and told are what the guest was
	// last told of those.
	ended, said bool
	count, told uint32
	// onWay is what was sent that the guest has not said it took.
	onWay int
	done  bool
}

func newStream(c *hostConn, id string) *stream {
	return &stream{c: c, id: id, ready: make(chan struct{}, 1), quit: make(chan struct{})}
}

func (s *stream) isDone() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done
}

// end stops the pane: once it has returned, nothing its program writes is
// taken, and nothing the guest typed that it was not given is typed. It
// may be called more than once.
func (s *stream) end() {
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.screen, s.data = nil, nil
	stop, lane := s.stop, s.lane
	s.mu.Unlock()
	close(s.quit)
	if lane != nil {
		lane.Close()
	}
	if stop != nil {
		// The terminal's lock is taken to be forgotten, and it holds that
		// as it calls here.
		stop()
	}
}

// Screen implements uiterm.Follower, as the next four do.
func (s *stream) Screen(cols, rows int, p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return
	}
	s.screen, s.data, s.behind, s.stale = nil, nil, 0, false
	s.large = !goodSize(cols, rows) || len(p) > mostScreen
	if s.large {
		// More than a guest takes: it is shown nothing more until the pane
		// is smaller.
		return
	}
	var size [4]byte
	binary.BigEndian.PutUint16(size[:], uint16(cols))
	binary.BigEndian.PutUint16(size[2:], uint16(rows))
	s.screen = frame(frameScreen, size[:], p)
	nudge(s.ready)
}

func (s *stream) Write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done || s.stale || s.large {
		return
	}
	if len(s.data)+len(p) > mostBehind || s.lateLocked() {
		s.staleLocked()
		return
	}
	s.data = append(s.data, p...)
	if s.behind == 0 {
		s.behind, s.since = len(s.data), time.Now()
	}
	nudge(s.ready)
}

// lateLocked reports whether there is much output, and some of it has
// waited long.
func (s *stream) lateLocked() bool {
	return len(s.data) > lateMost && s.behind > 0 && time.Since(s.since) > lateAfter
}

func (s *stream) Stale() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.done {
		s.staleLocked()
	}
}

func (s *stream) staleLocked() {
	s.screen, s.data, s.behind, s.stale = nil, nil, 0, true
	nudge(s.ready)
}

func (s *stream) Ended() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
	if s.lane != nil {
		s.lane.Close()
		s.lane = nil
	}
	nudge(s.ready)
}

func (s *stream) Started() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = false
	s.count++
	s.openLaneLocked()
	nudge(s.ready)
}

// openLaneLocked opens the lane for a program that runs.
func (s *stream) openLaneLocked() {
	if s.lane == nil && !s.ended && !s.done {
		s.lane = s.src.Lane()
	}
}

// carry sends the pane down ch, and types what comes back, until either
// end is done.
func (s *stream) carry(src PaneSource, ch ssh.Channel, reqs <-chan *ssh.Request) {
	s.mu.Lock()
	s.src = src
	s.mu.Unlock()
	stop := src.Follow(s)
	s.mu.Lock()
	s.stop = stop
	s.openLaneLocked()
	done := s.done
	s.mu.Unlock()
	if done {
		// Ended before there was anything to stop.
		stop()
	}
	var both sync.WaitGroup
	both.Go(func() {
		s.hear(reqs)
		s.c.closed(s)
		// The guest has closed the pane. What it typed just before is still
		// to be typed, but not a paste that would take long.
		if s.pause(closedFor) {
			s.end()
		}
	})
	both.Go(func() {
		s.send(ch)
		// Not waited for: a guest that takes nothing would hold it up, and
		// its connection ends it.
		_ = ch.Close()
	})
	both.Go(func() {
		err := s.receive(ch)
		s.end()
		if errors.Is(err, errFrame) {
			s.c.fail(err)
		}
	})
	both.Wait()
}

// hear takes the guest's word of what it took. It comes beside what is
// typed, which may be long in going, and not behind it.
func (s *stream) hear(reqs <-chan *ssh.Request) {
	for req := range reqs {
		var word took
		switch {
		case req.Type != reqTook:
			if !s.c.requests.try(1) {
				s.c.fail(errors.New("it asked for things without end"))
			}
		case ssh.Unmarshal(req.Payload, &word) != nil, !s.took(int(word.Bytes)):
			s.c.fail(errFrame)
		}
		if req.WantReply {
			_ = req.Reply(false, nil)
		}
	}
}

// took notes that the guest took n of what was sent, and reports whether
// that much was. A guest says so for no less than tookEvery, so it says no
// more than goes with what it is sent.
func (s *stream) took(n int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < tookEvery || n > s.onWay {
		return false
	}
	s.onWay -= n
	nudge(s.ready)
	return true
}

// piece is the frame to send next, and nil when there is none. asks says
// the screen has to be asked for first.
func (s *stream) piece() (b []byte, asks bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.done:
		return nil, false
	case s.stale, s.screen == nil && s.lateLocked():
		s.staleLocked()
		return nil, true
	case s.told != s.count:
		// Started again: before what the new program writes, whether or
		// not the guest was told of an end.
		s.told, s.said = s.count, false
		return programFrame(true, s.count), false
	case s.screen != nil:
		b, s.screen = s.screen, nil
		return b, false
	case len(s.data) > 0:
		n := min(len(s.data), pieceMost)
		b = frame(frameData, s.data[:n])
		s.behind = max(0, s.behind-n)
		if s.data = s.data[n:]; len(s.data) == 0 {
			s.data = nil
		}
		return b, false
	case s.ended && !s.said:
		// After all that it wrote.
		s.said = true
		return programFrame(false, s.count), false
	}
	return nil, false
}

// send writes what there is to the guest until the pane is done.
func (s *stream) send(ch ssh.Channel) {
	var sent time.Time
	for {
		// Room first, so that what is sent then is the latest there is.
		if !s.room(headerLen + pieceMost) {
			return
		}
		b, asks := s.piece()
		switch {
		case asks:
			if !s.pause(screenEvery - time.Since(sent)) {
				return
			}
			// Draws it into s.screen, unless the pane was done meanwhile.
			s.src.CatchUp(s)
			sent = time.Now()
		case b == nil:
			select {
			case <-s.quit:
				return
			case <-s.ready:
			}
		case !s.put(ch, b):
			return
		}
	}
}

// room waits until n may be sent, and reports whether the pane is still
// to be shown.
func (s *stream) room(n int) bool {
	for {
		s.mu.Lock()
		done, ok := s.done, s.onWay+n <= mostOnItsWay
		s.mu.Unlock()
		if done || ok {
			return !done
		}
		select {
		case <-s.quit:
			return false
		case <-s.ready:
		}
	}
}

// put writes p a piece at a time, as there is room for them, and no more
// of it once the pane is done. It reports whether all of it went.
func (s *stream) put(ch ssh.Channel, p []byte) bool {
	for len(p) > 0 {
		n := min(len(p), pieceMost)
		if !s.room(n) {
			return false
		}
		s.mu.Lock()
		s.onWay += n
		s.mu.Unlock()
		if _, err := ch.Write(p[:n]); err != nil {
			return false
		}
		p = p[n:]
	}
	return true
}

// pause waits for d, and reports whether it was not cut short.
func (s *stream) pause(d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-s.quit:
		return false
	}
}

// receive types what the guest sends, until it has sent all it will.
func (s *stream) receive(ch io.Reader) error {
	var buf []byte
	for {
		_, body, err := readFrame(ch, paneToHost, buf)
		if err != nil {
			return err
		}
		buf = body
		s.c.frames.wait(s.c.ctx, 1)
		s.c.bytes.wait(s.c.ctx, float64(len(body)))
		s.typed(splitTyped(body))
	}
}

// typed types p, when the guest may, and waits for the program to take
// what was typed before when there is too much of it: SSH's window holds
// the guest up then, and nobody else.
//
// What the guest may not type is dropped, and is no mistake of the
// guest's: it may have been let until just now, or have typed for a
// program that ended since, count.
func (s *stream) typed(count uint32, p []byte) {
	for len(p) > 0 {
		n, lane := s.typeSome(count, p)
		if lane == nil {
			return
		}
		if p = p[n:]; len(p) == 0 {
			return
		}
		select {
		case <-s.quit:
			return
		case <-lane.Room():
		}
	}
}

// typeSome types as much of p as there is room for, on lane, which is nil
// when it may not be typed.
func (s *stream) typeSome(count uint32, p []byte) (n int, lane uiterm.Lane) {
	h := s.c.h
	h.typeMu.RLock()
	defer h.typeMu.RUnlock()
	s.mu.Lock()
	if h.canType && !s.done && count == s.count {
		lane = s.lane
	}
	s.mu.Unlock()
	if lane == nil {
		return 0, nil
	}
	// A lane whose program ended just now takes nothing.
	if n = lane.Type(p); n > 0 {
		h.typed(s.id)
	}
	return n, lane
}

// dropTyped throws away what the guest typed that the program has not
// been given.
func (s *stream) dropTyped() {
	s.mu.Lock()
	lane := s.lane
	s.mu.Unlock()
	if lane != nil {
		lane.Drop()
	}
}
