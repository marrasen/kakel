package relay

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/coder/websocket"
)

// The doors of a relay, after its secret.
const (
	// doorHost is where a served window registers its code and hears
	// of each window that wants it.
	doorHost = "host"
	// doorJoin, then the code, is where a window connecting comes in.
	doorJoin = "join"
	// doorAccept, then the ticket, is where a served window brings the
	// stream for one window connecting.
	doorAccept = "accept"
)

// dialWithin bounds reaching the relay and its answer: the connection,
// any proxy, TLS and the WebSocket handshake.
var dialWithin = 30 * time.Second

// reachErr says the relay could not be reached, and why.
func reachErr(r URL, err error) error {
	return fmt.Errorf("relay %s: %w", r.Host(), err)
}

// Join reaches the window served through a relay under link's code: the
// stream it returns is the window's, for the serve protocol.
func Join(ctx context.Context, link Link, d Dialer) (net.Conn, error) {
	ws, err := d.open(ctx, link.Relay.to(doorJoin+"/"+link.Code), nil)
	if err != nil {
		return nil, reachErr(link.Relay, err)
	}
	return stream(ws, linkAddr(link.Shown())), nil
}

// stream is a WebSocket as a connection, pinged while it is quiet, so
// a relay that has gone is noticed and a proxy sees it in use.
func stream(ws *websocket.Conn, far net.Addr) net.Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &wsConn{Conn: carried(ctx, ws), cancel: cancel, far: far}
}

// wsConn is a stream through a relay.
type wsConn struct {
	net.Conn
	cancel context.CancelFunc
	far    net.Addr
}

func (c *wsConn) Close() error {
	err := c.Conn.Close()
	c.cancel()
	return err
}

// RemoteAddr is the window at the other end, as the relay named it.
func (c *wsConn) RemoteAddr() net.Addr { return c.far }

// linkAddr is the other end of a stream through a relay.
type linkAddr string

func (linkAddr) Network() string  { return "relay" }
func (a linkAddr) String() string { return string(a) }
