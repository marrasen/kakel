// Package guestlook is how a window shared with a link looks, as its host
// says it and its guest's web page reads it: how the panes stand in it,
// and the theme they are drawn in. Both ends are built of it, so what one
// writes the other reads.
//
// The host puts the two in a guest.State, as Layout and Theme. What the
// page is told besides, the panes and their titles, is the state's own:
// a pane's Icon is a name in look.KindIcons.
package guestlook
