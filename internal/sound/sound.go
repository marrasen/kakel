// Package sound plays kakel's sounds: the cues of buttons, menus and
// switches, and those of what happens on its own, as a connection lost
// or a bell. It opens the speakers only once a sound is wanted, and
// lets them rest while nothing plays.
package sound

import (
	"log"
	"sync"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
	"github.com/marrasen/gunim/audio/cues"
	"github.com/marrasen/gunim/audio/speaker"
)

// rest is how long the speakers stay open after the last sound, before
// they rest.
const rest = 3 * time.Second

// events are the cues of what happens on their own, which kakel plays
// itself, once it has decided to. Every other cue is the interface's.
var events = map[gunim.Cue]bool{
	gunim.CueConnected: true, gunim.CueDisconnected: true,
	gunim.CueDone: true, gunim.CueFailed: true, gunim.CueBell: true,
}

// Player plays cues as kakel's settings say. It implements
// gunim.CuePlayer.
type Player struct {
	// open opens the speakers; a test gives its own.
	open func(*audio.Mixer) (speakers, error)

	mu sync.Mutex
	// iface says the interface's cues sound, and wanted that some sound
	// does, so the speakers open.
	iface, wanted bool
	// mix, spk and cues play, once the speakers are open; opening says
	// they are being opened, and failed that they could not be.
	mix             *audio.Mixer
	spk             speakers
	cues            *cues.Player
	opening, failed bool
	resting         bool
	last            time.Time
	restTimer       *time.Timer
}

// speakers is what the player needs of the speakers.
type speakers interface {
	Suspend() error
	Resume() error
}

// New returns a player that plays nothing until Set says what to.
func New() *Player {
	return &Player{open: func(m *audio.Mixer) (speakers, error) {
		return speaker.Open(m, speaker.Options{Name: "kakel"})
	}}
}

// Set says whether the interface sounds, and whether any sound is
// wanted at all, which opens the speakers, in the background, the
// first time.
func (p *Player) Set(iface, any bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.iface = iface
	p.wanted = any || iface
	if !p.wanted || p.spk != nil || p.opening || p.failed {
		return
	}
	p.opening = true
	mix := audio.NewMixer()
	go func() {
		spk, err := p.open(mix)
		p.mu.Lock()
		defer p.mu.Unlock()
		p.opening = false
		if err != nil {
			p.failed = true
			log.Printf("kakel plays no sound: the speakers could not be opened: %v", err)
			return
		}
		p.mix, p.spk, p.cues = mix, spk, cues.New(mix)
	}()
}

// PlayCue implements gunim.CuePlayer: an interface cue plays where the
// interface sounds, and an event's always, as kakel plays those only
// where it was asked to. Nothing plays before the speakers are open.
func (p *Player) PlayCue(c gunim.Cue, pan float32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cues == nil || !p.wanted || !events[c] && !p.iface {
		return
	}
	if p.resting {
		if err := p.spk.Resume(); err != nil {
			return
		}
		p.resting = false
	}
	p.cues.PlayCue(c, pan)
	p.last = time.Now()
	if p.restTimer == nil {
		p.restTimer = time.AfterFunc(rest, p.rest)
	} else {
		p.restTimer.Reset(rest)
	}
}

// rest lets the speakers rest once nothing has played for a while.
func (p *Player) rest() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.spk == nil || p.resting || time.Since(p.last) < rest || p.mix.Playing() > 0 {
		return
	}
	if p.spk.Suspend() == nil {
		p.resting = true
	}
}

var _ gunim.CuePlayer = (*Player)(nil)
