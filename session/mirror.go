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
