package guestview

import (
	"context"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/session"
)

// Session is a guest's end of a connection to its host, as guest.Session
// is.
type Session interface {
	View() guest.View
	Changed() <-chan struct{}
	Done() <-chan struct{}
	Err() error
	Join(ctx context.Context, name, about string) error
	// Pane opens a pane the window shows, by its ID.
	Pane(ctx context.Context, id string) (session.ScreenReader, error)
	Focus(id string)
	Leave()
	Close() error
}

// Dialed is s as a Session.
func Dialed(s *guest.Session) Session { return dialed{s} }

type dialed struct{ *guest.Session }

func (d dialed) Pane(ctx context.Context, id string) (session.ScreenReader, error) {
	p, err := d.Session.Pane(ctx, id)
	if err != nil {
		// Not a nil pane in an interface that is not nil.
		return nil, err
	}
	return p, nil
}

// Keep is what a tab keeps of its share, so that a reload comes back as
// the same guest.
type Keep interface {
	// Load is the key the tab has joined with, if it kept one.
	Load() (guest.Key, bool)
	// Save keeps the key. Where it cannot, a reload does not come back.
	Save(key guest.Key)
	// Joined says the link is used up, so a tab that kept the key needs
	// nothing more of its address. It may be said more than once.
	Joined()
	// Clear forgets the key.
	Clear()
}

// Config is what the page is made with.
type Config struct {
	// Broken says the link is not whole, so there is nothing to join, and
	// Ended that it is not as this tab has been to its end.
	Broken, Ended bool

	// Dial reaches the host, as the guest with key. Its error is a
	// relay.Refused or what guest.Dial says, as errors.As and errors.Is
	// find them, and any other one is worth another try.
	Dial func(ctx context.Context, key guest.Key) (Session, error)

	// Keep is what the tab keeps of the share.
	Keep Keep

	// Name is what the guest called itself last, and SaveName keeps what
	// it calls itself now. About is the browser and the system.
	Name     string
	SaveName func(name string)
	About    string

	// Wake has word when a connection that was lost may be back, as when
	// the machine wakes or the network returns.
	Wake <-chan struct{}

	// Theme is what the page is drawn in until the host's is known, and
	// the window is opened in. Still has the host's animate nothing, as
	// the guest asked of the system.
	Theme look.Themed
	Still bool

	// CanFill says the page can fill the screen, Mac that its keys are a
	// Mac's, and Touch that a finger is what it is mostly worked with.
	CanFill, Mac, Touch bool

	// Shells, when set, is where the window's terminals are held, for a
	// test to read.
	Shells *screen.Shells

	// Drawn is called as the first frame is drawn, and OnState with each
	// state the page is to show. OnShown is called in the window once it
	// shows one.
	Drawn   func()
	OnState func(State)
	OnShown func(State)
}
