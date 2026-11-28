package guest

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// What one connection may do, so that a guest who is let in, and one who
// is not, can keep this machine no busier than somebody working would.
const (
	// streamsAtOnce is how many panes may be open at once, the ones being
	// closed counted.
	streamsAtOnce = 64

	// Panes opened, and other channels asked for: all of a window's at
	// once, and then a few a second. One past it is hung up on.
	opensBurst, opensRate = 160, 8

	// Requests, which are only ever answered with no.
	requestsBurst, requestsRate = 64, 4

	// Frames, and bytes of them, read from the guest: a paste of a
	// megabyte goes straight through. Past it the guest is read slower.
	framesBurst, framesRate = 400, 200
	bytesBurst, bytesRate   = 4 << 20, 1 << 20
)

// hostConn is one connection to a share, at the host.
type hostConn struct {
	h    *Host
	conn *ssh.ServerConn
	role role
	// ctx ends with the connection, or the share.
	ctx context.Context

	opens, requests, frames, bytes *limit
	// fileAsks and fileBytes are those of its files channels, all of them.
	fileAsks, fileBytes *limit

	// out writes the control channel, from when it is open. opened says
	// it is, and is closed then; heard is closed once nothing more comes
	// on it.
	out           *latest
	opened, heard chan struct{}

	// Of h.mu: streams are the panes it is shown, and closing those the
	// guest has closed, which may be typed in yet. streaming is how many
	// channels there are for panes, closed ones that have not gone among
	// them.
	streams   map[string]*stream
	closing   map[*stream]struct{}
	streaming int
	// filing is how many files channels there are, and handles how many
	// files and folders they have open.
	filing, handles int
}

func newHostConn(ctx context.Context, h *Host, conn *ssh.ServerConn, r role) *hostConn {
	return &hostConn{
		h: h, conn: conn, role: r, ctx: ctx,
		opens:    newLimit(opensBurst, opensRate),
		requests: newLimit(requestsBurst, requestsRate),
		frames:   newLimit(framesBurst, framesRate),
		bytes:    newLimit(bytesBurst, bytesRate),
		fileAsks: newLimit(fileAsksBurst, fileAsksRate), fileBytes: newLimit(fileBytesBurst, fileBytesRate),
		out:    newLatest(nil),
		opened: make(chan struct{}), heard: make(chan struct{}),
		streams: map[string]*stream{}, closing: map[*stream]struct{}{},
	}
}

// run serves the connection until it has gone, and everything it was
// shown has stopped.
func (c *hostConn) run(live *heard, chans <-chan ssh.NewChannel, reqs <-chan *ssh.Request) {
	ctx, cancel := context.WithCancel(c.ctx)
	c.ctx = ctx
	// One that proves a key and then says nothing is hung up on.
	idle := time.AfterFunc(joinWindow, func() {
		c.h.mu.Lock()
		waiting := c.h.cur != c
		c.h.mu.Unlock()
		if waiting {
			_ = c.conn.Close()
		}
	})
	defer idle.Stop()
	var running sync.WaitGroup
	running.Go(func() { discard(c.conn, c.requests, reqs) })
	running.Go(func() { stayAlive(ctx, c.conn, live) })
	running.Go(func() { c.channels(&running, chans) })
	_ = c.conn.Wait()
	cancel()
	c.out.stop()
	c.withdraw()
	running.Wait()
}

// fail hangs up on a connection that broke the rules.
func (c *hostConn) fail(err error) {
	c.h.report(fmt.Errorf("the guest at %s was disconnected: %w", c.conn.RemoteAddr(), err))
	_ = c.conn.Close()
}

// channels answers what the guest opens: one control channel, panes, and
// the folder.
func (c *hostConn) channels(running *sync.WaitGroup, chans <-chan ssh.NewChannel) {
	controlled := false
	for nch := range chans {
		if !c.opens.try(1) {
			c.fail(errors.New("it opened channels without end"))
			// Nor is anything that has come already looked at. The
			// connection goes, and takes it along.
			break
		}
		switch kind := nch.ChannelType(); {
		case kind == chanControl && !controlled:
			controlled = true
			ch, reqs, err := nch.Accept()
			if err != nil {
				continue
			}
			running.Go(func() { discard(c.conn, c.requests, reqs) })
			c.out.ch = ch
			close(c.opened)
			running.Go(func() { c.out.run() })
			running.Go(func() { c.control(ch) })
		case kind == chanPane:
			var want openPane
			if err := ssh.Unmarshal(nch.ExtraData(), &want); err != nil || want.ID == "" || len(want.ID) > mostID {
				_ = nch.Reject(ssh.Prohibited, "that is not a pane")
				continue
			}
			s, err := c.reserve(want.ID)
			if err != nil {
				_ = nch.Reject(ssh.Prohibited, err.Error())
				continue
			}
			running.Go(func() { c.show(s, nch) })
		case kind == chanFiles:
			if err := c.reserveFiles(); err != nil {
				_ = nch.Reject(ssh.Prohibited, err.Error())
				continue
			}
			ch, reqs, err := nch.Accept()
			if err != nil {
				c.releaseFiles()
				continue
			}
			running.Go(func() { discard(c.conn, c.requests, reqs) })
			running.Go(func() { c.files(ch) })
		default:
			_ = nch.Reject(ssh.UnknownChannelType, "this is a shared kakel window, and has no such channel")
		}
	}
	for range chans {
		// Read, or the connection would wait for it.
	}
}

// control reads the control channel until it ends.
func (c *hostConn) control(ch ssh.Channel) {
	defer close(c.heard)
	// The connection is nothing without it.
	defer func() { _ = ch.Close() }()
	why, replaced := c.h.greet(c)
	if replaced != nil {
		go replaced.end(ReasonReplaced)
	}
	if why != "" {
		go c.end(why)
	}
	var buf []byte
	for {
		kind, body, err := readFrame(ch, controlToHost, buf)
		if err != nil {
			if errors.Is(err, errFrame) {
				c.fail(err)
			} else {
				_ = c.conn.Close()
			}
			return
		}
		buf = body
		c.frames.wait(c.ctx, 1)
		c.bytes.wait(c.ctx, float64(len(body)))
		if why != "" {
			continue
		}
		switch kind {
		case frameName:
			j, key, err := decodeJoining(body)
			if err == nil {
				why, err = c.h.join(c, j, key)
			}
			if err != nil {
				c.fail(err)
				return
			}
			if why != "" {
				go c.end(why)
			}
		case frameFocus:
			c.h.focused(c, string(body))
		case frameFollow:
			c.h.followed(c)
		case frameLeave:
			c.h.leave(c)
		}
	}
}

// end tells the guest why its connection ends, waits a moment for it to
// hear, and hangs up. What it is shown stops first.
func (c *hostConn) end(why Reason) {
	c.withdraw()
	defer func() { _ = c.conn.Close() }()
	select {
	case <-c.opened:
	default:
		return
	}
	body, err := marshalEnded(why)
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if c.out.last(body) == nil {
			// The guest closes its end once it has the word.
			<-c.heard
		}
	}()
	t := time.NewTimer(sayEndedIn)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
	}
}

// withdraw stops everything the connection is shown.
func (c *hostConn) withdraw() {
	c.h.mu.Lock()
	gone := c.withdrawLocked(func(string) bool { return true })
	c.h.mu.Unlock()
	for _, s := range gone {
		s.end()
	}
}

// withdrawLocked takes the panes which names off what the connection is
// shown, for the caller to end once it has let go of h.mu.
func (c *hostConn) withdrawLocked(which func(id string) bool) []*stream {
	var gone []*stream
	for id, s := range c.streams {
		if which(id) {
			delete(c.streams, id)
			gone = append(gone, s)
		}
	}
	for s := range c.closing {
		if which(s.id) {
			delete(c.closing, s)
			gone = append(gone, s)
		}
	}
	return gone
}

// reserve makes room for a pane the guest may be shown.
func (c *hostConn) reserve(id string) (*stream, error) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	switch {
	case c.h.phase != PhaseIn || c.h.cur != c:
		// The same word as for a pane that is not there: a guest that
		// waits learns nothing of which are.
		return nil, ErrNotShared
	case !c.h.panes[id]:
		return nil, ErrNotShared
	case c.streams[id] != nil:
		return nil, errors.New("that pane is open already")
	case c.streaming >= streamsAtOnce:
		return nil, errors.New("too many panes are open")
	}
	s := newStream(c, id)
	c.streams[id] = s
	c.streaming++
	return s, nil
}

// release gives back what reserve took, once the channel has gone.
func (c *hostConn) release(s *stream) {
	s.end()
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	c.streaming--
	delete(c.closing, s)
	if c.streams[s.id] == s {
		delete(c.streams, s.id)
	}
}

// closed takes a pane the guest has closed off what it is shown, for it to
// open again.
func (c *hostConn) closed(s *stream) {
	c.h.mu.Lock()
	defer c.h.mu.Unlock()
	if c.streams[s.id] == s {
		delete(c.streams, s.id)
		c.closing[s] = struct{}{}
	}
}

// show opens a pane the guest asked for, and carries it until one end or
// the other is done.
func (c *hostConn) show(s *stream, nch ssh.NewChannel) {
	defer c.release(s)
	src, err := c.h.cfg.Open(s.id)
	if err != nil {
		_ = nch.Reject(ssh.Prohibited, ErrNotShared.Error())
		c.h.report(fmt.Errorf("the guest could not be shown a pane: %w", err))
		return
	}
	if s.isDone() {
		// It left the window meanwhile.
		_ = nch.Reject(ssh.Prohibited, ErrNotShared.Error())
		return
	}
	ch, reqs, err := nch.Accept()
	if err != nil {
		return
	}
	s.carry(src, ch, reqs)
}
