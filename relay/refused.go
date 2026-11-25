package relay

import (
	"fmt"
	"time"
)

// Refused is why a relay turned a guest away. A browser tells its page
// nothing of an HTTP answer to a WebSocket, so the relay takes the
// WebSocket and closes it with this as its code. Each is a reason of its
// own to test for with errors.Is.
type Refused int

const (
	// RefusedBusy says as many as may are joining the share already.
	RefusedBusy Refused = 4001 + iota
	// RefusedAway says the share is known, and its host is not on the
	// relay now, or did not answer.
	RefusedAway
	// RefusedGone says the relay has no such share: it ended, or never
	// was.
	RefusedGone
	// RefusedTooMany says too many links the relay does not know were
	// asked for from the guest's address.
	RefusedTooMany
)

// Retry reports whether the same guest may be let through a moment
// later.
func (r Refused) Retry() bool { return r != RefusedGone }

// reason is what the relay gives as the WebSocket's reason.
func (r Refused) reason() string {
	switch r {
	case RefusedBusy:
		return "too many are joining at once"
	case RefusedAway:
		return "the host is not connected"
	case RefusedGone:
		return "this link has expired or was already used"
	case RefusedTooMany:
		return "too many tries"
	}
	return "refused"
}

func (r Refused) Error() string { return r.reason() }

// refusal is the Refused a WebSocket closed with code stands for, or nil.
func refusal(code int) error {
	if r := Refused(code); r >= RefusedBusy && r <= RefusedTooMany {
		return r
	}
	return nil
}

// joinedWord is what the relay tells a guest, as a text message, once it
// is joined to the host. Every message after it is the host's.
const joinedWord = "joined"

// joinWithin bounds a guest's join: reaching the relay, and the relay's
// wait for the host.
var joinWithin = time.Minute

// unjoined is an answer to a join that is no relay's.
func unjoined(l ShareLink) error {
	return fmt.Errorf("%s did not answer as a relay", l.Host())
}
