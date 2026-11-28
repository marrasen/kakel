package guestview

import (
	"errors"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/relay"
)

// What the pages say. Those that end in a link used up ask for a new one,
// as the relay's own do.
const (
	askNew = "Ask for a new link."

	joinTitle = "Join a shared terminal window"
	joinSays  = "The host must allow you. The host sees your name, your browser and what you type."

	waitTitle = "Waiting for the host"
	waitSays  = "The window opens here when the host allows you."

	backTitle = "Reconnecting"
	backSays  = "The window opens here when the connection is back."

	lostTitle = "Connection lost"
	lostSays  = "Reconnecting…"

	notShared, tooLarge, tooManyPanes = "Not shared", "Too large to show", "Too many to show"

	canType, canWatch = "You can type", "You can only watch"

	// What was typed, pasted or copied, and was not.
	notTyped, notPasted, notCopiedTitle = "Not typed", "Not pasted", "Not copied"
	// Why it was not.
	typeAgain, lostWhy, watchWhy = "Type it again.", "The connection was lost.", "You can only watch."
	goneWhy, browserWhy          = "The pane was closed.", "The browser refused."
)

// overCancelled is a wait for the host that the guest called off.
var overCancelled = Over{Title: "Cancelled", Text: "To come back, ask for a new link."}

// hostOr is the host's name at the start of a sentence, or what stands
// for it.
func hostOr(host string) string {
	if host == "" {
		return "The host"
	}
	return host
}

// followWord is the button that has the guest follow the host again.
func followWord(host string) string {
	if host == "" {
		return "Follow the host"
	}
	return "Follow " + host
}

// overBroken is a link that is not whole.
var overBroken = Over{Title: "This link is incomplete", Text: "Open the whole link you were sent."}

// overGone is a share the relay does not know. joined says this guest has
// been let through the link before.
func overGone(joined bool) Over {
	if joined {
		return Over{Title: "Sharing stopped", Text: "The window is no longer shared."}
	}
	// In the relay's own words, which it says where it is the one that
	// answers.
	return Over{Title: "This link has expired or was already used", Text: askNew}
}

// overUnreached is a host that could not be reached for too long.
var overUnreached = Over{Title: "Couldn't reconnect", Text: "The host was not reached for too long.", Act: ActRetry, Word: "Retry"}

// overOf is the page for an end the host said, as host is called.
func overOf(why guest.Reason, host string) Over {
	switch why {
	case guest.ReasonRefused:
		return Over{Title: hostOr(host) + " refused you"}
	case guest.ReasonUnanswered:
		return Over{Title: hostOr(host) + " did not answer", Text: askNew}
	case guest.ReasonExpired:
		return Over{Title: "This link has expired", Text: askNew}
	case guest.ReasonUsed:
		return Over{Title: "This link was already used", Text: "Each link lets in one person. " + askNew}
	case guest.ReasonAway:
		return Over{Title: "You were disconnected", Text: "The connection was lost for too long. " + askNew}
	case guest.ReasonLeft:
		return Over{Title: "You left the window", Text: "To come back, ask for a new link."}
	case guest.ReasonWindowClosed:
		return Over{Title: "Sharing stopped", Text: hostOr(host) + " closed the window."}
	case guest.ReasonQuitting:
		return Over{Title: "Sharing stopped", Text: hostOr(host) + " closed kakel."}
	case guest.ReasonReplaced:
		return Over{Title: "Open in another tab", Text: "Use Here closes it there.", Act: ActHere, Word: "Use Here"}
	case guest.ReasonStopped:
	}
	return Over{Title: "Sharing stopped", Text: hostOr(host) + " stopped sharing the window."}
}

// ending is the page for an error there is no coming back from by trying
// again, and false for any other. joined says the guest has joined.
func ending(err error, host string, joined bool) (Over, bool) {
	var said *guest.EndedError
	var refused relay.Refused
	switch {
	case errors.As(err, &said):
		return overOf(said.Reason, host), true
	case errors.Is(err, guest.ErrWrongHost):
		return Over{Title: "This link could not be verified", Text: "Another kakel answered. " + askNew}, true
	case errors.Is(err, guest.ErrDenied):
		return Over{Title: "This link is not valid", Text: askNew}, true
	case errors.As(err, &refused) && !refused.Retry():
		return overGone(joined), true
	}
	return Over{}, false
}

// troubleOf says why the host was not reached, in a few words.
func troubleOf(err error) string {
	var refused relay.Refused
	if errors.As(err, &refused) {
		switch refused {
		case relay.RefusedBusy, relay.RefusedTooMany:
			return "Too many tries. Wait a minute, then retry."
		case relay.RefusedAway:
			return "The host is not connected."
		case relay.RefusedGone:
		}
	}
	return "Couldn't reach the host. Check the network, then retry."
}
