package sound

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/audio"
)

// fakeSpeakers counts its rests and wakes.
type fakeSpeakers struct{ suspended, resumed atomic.Int32 }

func (f *fakeSpeakers) Suspend() error { f.suspended.Add(1); return nil }
func (f *fakeSpeakers) Resume() error  { f.resumed.Add(1); return nil }

// player returns a player whose speakers are fake, and opened.
func player(t *testing.T, iface, any bool) (*Player, *fakeSpeakers) {
	t.Helper()
	spk := &fakeSpeakers{}
	opened := make(chan struct{})
	p := &Player{open: func(*audio.Mixer) (speakers, error) { defer close(opened); return spk, nil }}
	p.Set(iface, any)
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("the speakers were not opened")
	}
	// Taken once the opening goroutine has held the lock.
	for range 100 {
		p.mu.Lock()
		ready := p.cues != nil
		p.mu.Unlock()
		if ready {
			return p, spk
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("the player never got its speakers")
	return nil, nil
}

// playing is how many sounds the player's mixer plays.
func playing(p *Player) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.mix.Playing()
}

func TestTheInterfaceSoundsOnlyWhereAsked(t *testing.T) {
	p, _ := player(t, false, true)
	p.PlayCue(gunim.CuePress, 0)
	if n := playing(p); n != 0 {
		t.Fatalf("a button sounded with the interface's sounds off: %d playing", n)
	}
	// An event's cue sounds: kakel plays it only where it was asked to.
	p.PlayCue(gunim.CueBell, 0)
	if n := playing(p); n != 1 {
		t.Fatalf("the bell made %d sounds, want 1", n)
	}
	p.Set(true, true)
	p.PlayCue(gunim.CuePress, 0)
	if n := playing(p); n != 2 {
		t.Fatalf("with the interface's sounds on, %d play, want 2", n)
	}
	p.Set(false, false)
	p.PlayCue(gunim.CueBell, 0)
	if n := playing(p); n != 2 {
		t.Fatalf("with every sound off, a bell still sounded: %d playing", n)
	}
}

func TestNothingOpensTheSpeakersUntilASoundIsWanted(t *testing.T) {
	opened := false
	p := &Player{open: func(*audio.Mixer) (speakers, error) { opened = true; return &fakeSpeakers{}, nil }}
	p.Set(false, false)
	p.PlayCue(gunim.CueBell, 0)
	time.Sleep(20 * time.Millisecond)
	if opened {
		t.Fatal("the speakers opened with every sound off")
	}
}

func TestTheSpeakersRestOnceQuietAndWakeForASound(t *testing.T) {
	p, spk := player(t, false, true)
	p.PlayCue(gunim.CueBell, 0)
	// The speakers take what plays until it has ended, as real ones do.
	buf := make([]float32, 4096)
	for range 1000 {
		if playing(p) == 0 {
			break
		}
		p.mu.Lock()
		p.mix.Mix(buf)
		p.mu.Unlock()
	}
	if playing(p) != 0 {
		t.Fatal("the bell never ended")
	}
	// Rest as if the time since had passed.
	p.mu.Lock()
	p.last = time.Now().Add(-2 * rest)
	p.mu.Unlock()
	p.rest()
	if spk.suspended.Load() != 1 {
		t.Fatal("the speakers did not rest")
	}
	// Another cue: the same one again so soon would be let pass.
	p.PlayCue(gunim.CueDone, 0)
	if spk.resumed.Load() != 1 || playing(p) != 1 {
		t.Fatal("a sound did not wake the speakers")
	}
}
