package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/install"

	"github.com/marrasen/kakel/app"
)

// KAKEL_STRESS_WINDOWS=15m has kakel open the What's New window, leave
// it a moment, close it, and go again, for that long: to bring back a
// freeze seen once, as the second such window opened. Should the
// windows stop answering, gunim writes where everything was to
// gunim-hang-kakel-<pid>-<time>.txt in the temporary folder. The log
// says how many windows opened.

// stressEnv names the stress, and how long it runs.
const stressEnv = "KAKEL_STRESS_WINDOWS"

// The stress's pace: how long a window stays open, and the wait before
// the next.
const (
	stressOpen = 3 * time.Second
	stressGap  = 1500 * time.Millisecond
)

// stressWindows opens and closes What's New on ga until its time is up
// or ctx ends, when KAKEL_STRESS_WINDOWS asks for it.
func stressWindows(ctx context.Context, ga *gunim.App) {
	d, err := time.ParseDuration(os.Getenv(stressEnv))
	if err != nil || d <= 0 {
		return
	}
	say("%s: opening and closing What's New for %s", stressEnv, d)
	// A moment for kakel's own window to open first, as it had.
	select {
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return
	}
	end := time.Now().Add(d)
	n := 0
	for time.Now().Before(end) && ctx.Err() == nil {
		n++
		wctx, closeIt := context.WithCancel(ctx)
		start := time.Now()
		if err := install.ShowWhatsNew(wctx, ga, app.Installer(), ""); err != nil {
			say("%s: window %d didn't open: %v", stressEnv, n, err)
		} else if took := time.Since(start); took > time.Second {
			say("%s: window %d took %s to open", stressEnv, n, took.Round(time.Millisecond))
		}
		pause(ctx, stressOpen)
		// Closed as its context ends, as the program closing does.
		closeIt()
		pause(ctx, stressGap)
		if n%20 == 0 {
			say("%s: %d windows so far", stressEnv, n)
		}
	}
	say("%s: done, %d windows opened and closed with no freeze", stressEnv, n)
}

// say writes how the stress goes to kakel's log, which Help › Window
// Log shows, and to stderr, for a run started from a shell.
func say(format string, args ...any) {
	log.Printf(format, args...)
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}

// pause waits d, or until ctx ends.
func pause(ctx context.Context, d time.Duration) {
	select {
	case <-time.After(d):
	case <-ctx.Done():
	}
}
