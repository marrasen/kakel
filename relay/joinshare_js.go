package relay

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

// JoinShare reaches the host of a shared window, from its own page: the
// stream it returns is the host's. The error is a Refused, as errors.As
// finds it, when it was the relay that turned the guest away.
func JoinShare(ctx context.Context, link ShareLink, _ Dialer) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, joinWithin)
	defer cancel()
	c, err := newPageConn(link)
	if err != nil {
		return nil, err
	}
	select {
	case <-c.joined:
		return c, nil
	case <-c.over:
	case <-ctx.Done():
		_ = c.Close()
		return nil, ctx.Err()
	}
	select {
	case <-c.joined:
		// Joined, and closed right after: it is for its reader to find.
		return c, nil
	default:
		return nil, c.why
	}
}

// mostHeld is how much a page holds of what it has been sent and has
// not read. The protocol above holds each end to less than this.
const mostHeld = 32 << 20

// mostUnsent is how much of what a page writes may wait in the browser
// before a write waits too.
const mostUnsent = 4 << 20

// pageConn is a page's stream over the browser's WebSocket. It is
// written here rather than as the websocket package's, which in a
// browser does not say what code a stream was closed with, and that is
// how a relay says why.
type pageConn struct {
	ws   js.Value
	link ShareLink
	// funcs are what the WebSocket calls, released once it has closed.
	funcs map[string]js.Func
	// joined is closed once the relay says the page is joined, and over
	// once the stream has ended, for the reason why.
	joined, over chan struct{}
	why          error

	mu sync.Mutex
	// held is what came and has not been read, and bytes how much that
	// is.
	held  [][]byte
	bytes int
	// changed is closed, and made again, when there is something new to
	// read of, an end or a new deadline among them.
	changed chan struct{}
	// late is closed once the time to read by has passed, by timer.
	late  chan struct{}
	timer *time.Timer
}

// newPageConn opens the WebSocket to link's join door. The URL is built
// of the page's own and has no secret.
func newPageConn(link ShareLink) (_ *pageConn, err error) {
	defer func() {
		// The browser throws on a URL it will not open.
		if r := recover(); r != nil {
			jsErr, ok := r.(js.Error)
			if !ok {
				panic(r)
			}
			err = jsErr
		}
	}()
	// http becomes ws, and https wss.
	ws := js.Global().Get("WebSocket").New("ws" + strings.TrimPrefix(link.join(), "http"))
	ws.Set("binaryType", "arraybuffer")
	c := &pageConn{ws: ws, link: link, funcs: map[string]js.Func{},
		joined: make(chan struct{}), over: make(chan struct{}),
		changed: make(chan struct{}), late: make(chan struct{})}
	c.on("message", func(e js.Value) { c.message(e.Get("data")) })
	c.on("close", func(e js.Value) {
		code := e.Get("code").Int()
		err := refusal(code)
		switch {
		case err != nil:
		case code == 1000 || code == 1001:
			err = io.EOF
		default:
			err = errors.New("the connection was lost")
		}
		c.end(err)
		for _, f := range c.funcs {
			f.Release()
		}
	})
	return c, nil
}

// on has the WebSocket call do at an event. An error is followed by a
// close, which says all that is said.
func (c *pageConn) on(event string, do func(js.Value)) {
	f := js.FuncOf(func(_ js.Value, args []js.Value) any {
		do(args[0])
		return nil
	})
	c.funcs[event] = f
	c.ws.Set("on"+event, f)
}

// message takes a message: the relay's word first, then the host's.
func (c *pageConn) message(data js.Value) {
	text := data.Type() == js.TypeString
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.joined:
	default:
		if text && data.String() == joinedWord {
			close(c.joined)
		} else {
			c.endLocked(unjoined(c.link))
			c.ws.Call("close")
		}
		return
	}
	if text {
		c.endLocked(errors.New("the relay sent what is not the host's"))
		c.ws.Call("close")
		return
	}
	array := js.Global().Get("Uint8Array").New(data)
	b := make([]byte, array.Length())
	js.CopyBytesToGo(b, array)
	if c.bytes+len(b) > mostHeld {
		c.endLocked(errors.New("more was sent than is held"))
		c.ws.Call("close")
		return
	}
	if len(b) > 0 {
		c.held = append(c.held, b)
		c.bytes += len(b)
		c.wakeLocked()
	}
}

// end ends the stream for a reason, the first one given.
func (c *pageConn) end(why error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endLocked(why)
}

// endLocked is end, with c.mu held.
func (c *pageConn) endLocked(why error) {
	select {
	case <-c.over:
		return
	default:
	}
	c.why = why
	close(c.over)
	c.wakeLocked()
}

// wakeLocked has a read look again, with c.mu held.
func (c *pageConn) wakeLocked() {
	close(c.changed)
	c.changed = make(chan struct{})
}

// Read reads what the host sent, all of it before the end is told of.
func (c *pageConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		changed, late := c.changed, c.late
		select {
		case <-late:
			c.mu.Unlock()
			return 0, os.ErrDeadlineExceeded
		default:
		}
		if len(p) == 0 {
			c.mu.Unlock()
			return 0, nil
		}
		if len(c.held) > 0 {
			n := copy(p, c.held[0])
			if n < len(c.held[0]) {
				c.held[0] = c.held[0][n:]
			} else {
				c.held[0] = nil
				c.held = c.held[1:]
			}
			c.bytes -= n
			c.mu.Unlock()
			return n, nil
		}
		select {
		case <-c.over:
			c.mu.Unlock()
			return 0, c.why
		default:
		}
		c.mu.Unlock()
		select {
		case <-changed:
		case <-late:
		}
	}
}

// Write sends p to the host, waiting while the browser has much unsent.
func (c *pageConn) Write(p []byte) (int, error) {
	for {
		select {
		case <-c.over:
			return 0, net.ErrClosed
		default:
		}
		if c.ws.Get("bufferedAmount").Int() < mostUnsent {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(p) == 0 {
		return 0, nil
	}
	array := js.Global().Get("Uint8Array").New(len(p))
	js.CopyBytesToJS(array, p)
	c.ws.Call("send", array)
	return len(p), nil
}

// Close leaves the host.
func (c *pageConn) Close() error {
	c.end(net.ErrClosed)
	// With no code, as the browser takes it at any time: while it is
	// opening too.
	c.ws.Call("close")
	return nil
}

// SetReadDeadline sets when a read gives up, and none for the zero time.
func (c *pageConn) SetReadDeadline(at time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	late := make(chan struct{})
	c.late = late
	if !at.IsZero() {
		if in := time.Until(at); in > 0 {
			c.timer = time.AfterFunc(in, func() { close(late) })
		} else {
			close(late)
		}
	}
	c.wakeLocked()
	return nil
}

// SetWriteDeadline does nothing: the browser takes what is written at
// once.
func (c *pageConn) SetWriteDeadline(time.Time) error { return nil }

// SetDeadline is SetReadDeadline.
func (c *pageConn) SetDeadline(at time.Time) error { return c.SetReadDeadline(at) }

// LocalAddr is the page.
func (c *pageConn) LocalAddr() net.Addr { return linkAddr("page") }

// RemoteAddr is the host, as its link is shown.
func (c *pageConn) RemoteAddr() net.Addr { return linkAddr(c.link.Shown()) }
