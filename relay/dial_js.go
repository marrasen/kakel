package relay

import (
	"context"
	"errors"
	"net/http"

	"github.com/coder/websocket"
)

// Dialer is how a page in a browser reaches a relay: as the browser
// does, with its own proxies and its own checks of certificates.
type Dialer struct{}

// canPing says a stream can be pinged from this end. A browser answers
// pings by itself and sends none, so it is the other end that notices
// a page that has gone.
const canPing = false

// open opens a WebSocket to one of the relay's doors. A browser sends
// no headers of the program's own, so the doors that want one are not
// for it.
func (Dialer) open(ctx context.Context, to string, header http.Header) (*websocket.Conn, error) {
	if len(header) > 0 {
		return nil, errors.New("a browser cannot send the relay a header")
	}
	ctx, cancel := context.WithTimeout(ctx, dialWithin)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, to, nil)
	switch {
	case ctx.Err() != nil:
		if err == nil {
			// Opened just as its time ran out. A close waits for the
			// other end's.
			go func() { _ = ws.CloseNow() }()
		}
		return nil, ctx.Err()
	case err != nil:
		// A browser never says why, and the error names the URL, which
		// holds the relay's secret.
		return nil, errors.New("the connection failed")
	}
	return ws, nil
}
