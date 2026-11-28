package guestview

import (
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/look"
)

// Page is which page shows.
type Page uint8

// The pages, in the order they come.
const (
	// PageJoin asks for the guest's name.
	PageJoin Page = iota
	// PageWaiting waits for the host's answer, or, after a reload, for
	// the connection.
	PageWaiting
	// PageWindow is the shared window.
	PageWindow
	// PageOver says how it ended.
	PageOver
)

// State is what the page shows.
type State struct {
	Page Page

	// Name is what the guest calls itself, as first shown.
	Name string
	// Busy says a Join is on its way, and Trouble why the last one did
	// not get there.
	Busy    bool
	Trouble string

	// Lost says the connection is lost and being made again, and Why what
	// keeps it from being, where there is more to say.
	Lost bool
	Why  string

	// Over says how it ended.
	Over Over

	// HostName and the rest are the shared window's, once the guest is
	// in. They stay as they were while the connection is lost.
	HostName string
	CanType  bool
	Layout   dock.Layout
	Panes    []guest.Pane
	HostIn   string
	// Held are the terminals on the screen that are not shown, and why.
	Held map[string]Held
	// Shut are the terminals whose panes are not open, or have yet to
	// show their first screen: what is typed in one waits for that.
	Shut map[string]bool
	// Untyped counts, by pane, the times something typed was for nobody,
	// and Conn the connections there have been.
	Untyped map[string]int
	Conn    int

	// Theme is what the host's panes and their tabs are drawn in, with
	// the host's own colours, and Own what the page says itself is.
	// Themes counts the themes there have been.
	Theme  look.Themed
	Own    theme.Theme
	Themes int
}

// Over is a page that says how it ended.
type Over struct {
	Title, Text string
	// Act is what can be done about it, and Word the button for that.
	Act  Act
	Word string
}

// Held is why a terminal is not shown.
type Held uint8

// Why a terminal is not shown.
const (
	// HeldLarge is one a page cannot hold with the others.
	HeldLarge Held = iota + 1
	// HeldMany is one of too many at once.
	HeldMany
)

// Act is what a guest can do at the end.
type Act uint8

// What a guest can do at the end.
const (
	ActNone Act = iota
	// ActRetry tries once more.
	ActRetry
	// ActHere has the window here, and not in the other tab.
	ActHere
)

// Intents, from the page.
type (
	// Join joins as Name.
	Join struct{ Name string }
	// Leave leaves the share, for good.
	Leave struct{}
	// Do does what the page at the end offers.
	Do struct{ Act Act }
	// Shown says which panes are on the screen.
	Shown struct{ Panes []string }
	// In says which pane the guest is in.
	In struct{ Pane string }
	// Follows says whether the guest follows the host: while it does, it
	// is in whichever pane the host is in.
	Follows struct{ On bool }
)

// The topic the State is published on, and what else the view is told.
const topic = "guest"

// outputArrived tells the view that a program wrote.
type outputArrived struct{}

// notCopied tells the view that the browser kept a copy off the
// clipboard.
type notCopied struct{ why string }

// rang tells the view that a pane's bell rang.
type rang struct{ pane string }
