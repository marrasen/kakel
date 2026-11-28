// Package guest is a kakel window shared with one person by a link: the
// protocol, and both its ends. The host's is in kakel, and the guest's in
// a web browser.
//
// # The host
//
// NewHost makes a share of one window, and Secret what its link carries.
// Serve takes connections from a listener, wherever they come from.
// Publish says how the window stands each time that changes, whether or
// not anybody is there. HostConfig's Ask says whether a guest may come in,
// and its Open gives the pane one asks for, checking it again each time.
// OnChange hears how the share stands, Status, the sizes the guest sets
// among it. End ends it.
//
//	h, err := guest.NewHost(guest.HostConfig{Ask: ask, Open: open, OnChange: changed})
//	link := base + "#" + h.Secret().String()
//	go h.Serve(listener)
//	h.Publish(guest.State{HostName: "anna", CanType: true, Panes: panes})
//	...
//	h.End(guest.ReasonStopped)
//
// A *uiterm.Terminal is a PaneSource as it is. Publish comes before a pane
// that left the window shows anything else: once it has returned, the
// guest is sent nothing more of it and types nothing more in it. A new
// theme is published before the terminals are given its colours, so that
// the guest's have them first.
//
// # The guest
//
// Dial, over a connection to the host, gives a Session. It says PhaseJoin
// for a link nobody has used, and Join joins. From then Changed and View
// follow the window. Pane opens one of its terminals, as a session for a
// terminal to be on. Follow says the guest is in whichever pane the host
// is in, and Focus that it is in one it picked. Leave ends the
// share for good; a lost connection, ErrLost, is dialled again with the
// same key, and comes back where it was, waiting or in the window.
//
//	s, err := guest.Dial(ctx, conn, guest.Options{Secret: secret, Key: key, Yield: yield})
//	if s.View().Phase == guest.PhaseJoin {
//		err = s.Join(ctx, name, about)
//	}
//	pane, err := s.Pane(ctx, id)
//
// A terminal on a pane has the host's size and no other, answers nothing
// the program asks, and is drawn scaled. Where the state says GuestSizes,
// Fit asks the host to size a pane's program to what fits it on the page. It is somebody else's program, so
// it is given no hooks that act on this machine: no clipboard for OSC 52,
// no links to open, no paths to find, no bell or title outside the page.
// With them nil a terminal does nothing of those. Copying and pasting by
// hand work as they do in any pane.
//
// A connection that ended under a terminal gives it an end of file. The
// pane opened after the next Dial is put under it with Restart, which
// keeps its history.
//
// A browser gives a Go program one thread, and has it back only when every
// goroutine waits. So the reading of a pane calls Options.Yield each 64 KB
// and waits for it, and the host sends a pane no more than 128 KB that the
// guest has not said it drew. Nothing here spins, and nothing that may
// be called from where a window is drawn waits, but Join, Leave and Pane,
// which are for goroutines of their own.
//
// # What a guest may do
//
// The two ends speak SSH, so whatever carries them can read and change
// nothing. The guest checks the host's key by the secret. The host takes
// the key the secret makes, until somebody joins with it; whoever comes
// with it after that is told it is used, and can do nothing else. Joining
// binds the guest's own key, which is the only way in from then. Nothing
// of the window is said until the host has let the guest in.
//
// A guest can open a channel for each terminal that the window's published
// state lists and Open still gives. Down it comes what that program draws;
// up it go the keys typed, which are dropped while the host does not let
// the guest type, and the size the pane has room for, which is dropped
// while the host does not let the guest set the size. A size is held to
// no more often than fitEvery, and the host's comes back as the guest
// closes the pane, as the choice or the window changes, and a while after
// the connection is lost. There is no request for a shell, a file, a
// tunnel, the clipboard, or anything outside the window, and everything
// that is not one of these is refused.
//
// What a guest has the host hold is bounded, in channels, frames and
// bytes, and what it has the host do, in time. A guest that is slow is
// sent the screen again rather than what it missed, and holds up nobody.
package guest
