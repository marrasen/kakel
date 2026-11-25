package session

// Mirrors reports whether s shows a program that another terminal runs,
// as a pane in another kakel window does: that terminal answers what the
// program asks of it, such as the device's attributes or where the
// cursor is, so the one drawing s must not answer as well. A second
// answer would reach the program late, typed in among the user's input.
//
// A session says so with a Mirrors method.
func Mirrors(s Session) bool {
	m, ok := s.(interface{ Mirrors() bool })
	return ok && m.Mirrors()
}

// Screen is a whole screen, for a terminal to show in place of its own.
type Screen struct {
	// Cols and Rows are its size, and Draw is what draws it, as
	// vt.Terminal.Replay writes it.
	Cols, Rows int
	Draw       []byte

	// Started says that another program has been started in place of the
	// one that ended, and what was typed and not sent was for that one. A
	// Started with no size is no screen: there is nothing to show.
	Started bool
}

// ScreenReader is a session of a program whose screen another terminal
// has, at a size that is that terminal's to change, as a pane of a shared
// window is to its guest.
//
// ReadScreen is Read, in one order with the screens: it gives output, or
// a screen that stands in place of everything before it. Output after a
// screen lands on it. A terminal on such a session calls it rather than
// Read, has the size of the last screen, and never asks the session for
// another.
type ScreenReader interface {
	Session
	ReadScreen(p []byte) (n int, scr *Screen, err error)
}
