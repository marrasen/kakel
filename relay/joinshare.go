//go:build !js

package relay

import (
	"context"
	"net"

	"github.com/coder/websocket"
)

// JoinShare reaches the host of a shared window: the stream it returns
// is the host's. The error is a Refused, as errors.As finds it, when it
// was the relay that turned the guest away.
func JoinShare(ctx context.Context, link ShareLink, d Dialer) (net.Conn, error) {
	ws, err := d.open(ctx, link.join(), nil)
	if err != nil {
		return nil, err
	}
	rctx, cancel := context.WithTimeout(ctx, joinWithin)
	defer cancel()
	kind, word, err := ws.Read(rctx)
	if err != nil {
		_ = ws.CloseNow()
		if why := refusal(int(websocket.CloseStatus(err))); why != nil {
			return nil, why
		}
		return nil, err
	}
	if kind != websocket.MessageText || string(word) != joinedWord {
		_ = ws.CloseNow()
		return nil, unjoined(link)
	}
	return stream(ws, linkAddr(link.Shown())), nil
}
