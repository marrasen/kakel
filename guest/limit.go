package guest

import (
	"context"
	"sync"
	"time"
)

// limit is how much of something one connection may do: so much at once,
// and so much a second after that.
type limit struct {
	mu    sync.Mutex
	burst float64
	rate  float64
	has   float64
	at    time.Time
}

func newLimit(burst, rate float64) *limit {
	return &limit{burst: burst, rate: rate, has: burst, at: time.Now()}
}

// fill gives back what time has. mu is held.
func (l *limit) fill() {
	now := time.Now()
	l.has = min(l.burst, l.has+now.Sub(l.at).Seconds()*l.rate)
	l.at = now
}

// try takes n if there is that much, and reports whether it did.
func (l *limit) try(n float64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fill()
	if l.has < n {
		return false
	}
	l.has -= n
	return true
}

// wait takes n, waiting for what was not there: the far end is read no
// faster than this, and SSH's own window holds it up. It returns sooner
// when ctx ends.
func (l *limit) wait(ctx context.Context, n float64) {
	l.mu.Lock()
	l.fill()
	l.has -= n
	owed := -l.has
	l.mu.Unlock()
	if owed <= 0 {
		return
	}
	t := time.NewTimer(time.Duration(owed / l.rate * float64(time.Second)))
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
