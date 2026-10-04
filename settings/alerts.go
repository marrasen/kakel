package settings

import "fmt"

// Alerts says, for each kind of event, whether kakel tells of it: by a
// sound, or by rings sent out around the window. Interface is the
// sounds of buttons, menus and switches, which only sounds have.
type Alerts struct {
	Interface bool `json:"interface"`
	// Connected and Lost are a connection made, and one that went by
	// itself.
	Connected bool `json:"connected"`
	Lost      bool `json:"lost"`
	// Finished is a long command, or a program, ending.
	Finished bool `json:"finished"`
	// Bell is a terminal's bell.
	Bell bool `json:"bell"`
	// Other is every other failure, and other work finished, such as a
	// copy.
	Other bool `json:"other"`
}

// The alerts of a user who has not chosen: rings for everything, as
// kakel always sent, and the bell's sound, which otherwise says nothing
// in the pane in front.
var (
	DefaultSounds = Alerts{Bell: true}
	DefaultRings  = Alerts{Connected: true, Lost: true, Finished: true, Bell: true, Other: true}
)

// Sounds are the events kakel plays a sound for.
func (s *Settings) Sounds() Alerts {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.Sounds == nil {
		return DefaultSounds
	}
	return *s.have.Sounds
}

// Rings are the events kakel sends rings out around the window for.
func (s *Settings) Rings() Alerts {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.have.Rings == nil {
		return DefaultRings
	}
	r := *s.have.Rings
	// The interface has no rings.
	r.Interface = false
	return r
}

// SystemTitleBar reports whether windows take the system's title bar
// and frame, as a window manager draws them, rather than kakel's own.
func (s *Settings) SystemTitleBar() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.have.SystemTitleBar
}

// PutLook keeps the events kakel tells of by sound and by rings, and
// whether windows take the system's title bar, and saves.
func (s *Settings) PutLook(sounds, rings Alerts, systemTitleBar bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.rereadLocked(); err != nil {
		return fmt.Errorf("%w: %w", ErrUnsaveable, err)
	}
	before := s.have
	s.have.Sounds, s.have.Rings, s.have.SystemTitleBar = &sounds, &rings, systemTitleBar
	if err := s.saveLocked(); err != nil {
		s.have = before
		return err
	}
	return nil
}
