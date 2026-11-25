// Package guestview is the page a guest opens a shared window in: the
// page that asks for a name, the wait for the host's answer, the window
// itself, and the page that says how it ended. The web client shows it
// in a browser, and the tests in a window of their own.
//
// It has the two halves a gunim program has. Run is the program's: it
// joins, opens the panes that are shown, comes back when the connection
// is lost, and publishes a State. The view shows the State and sends
// back what the guest does, as intents. What the two need of where they
// run, as what a tab keeps and how the host is reached, is Config's.
package guestview
