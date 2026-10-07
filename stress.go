package main

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/install"

	"github.com/marrasen/kakel/app"
)

// The windows of gunim's own that KAKEL_STRESS opens and closes, beside
// what the app does to its windows and panes: What's New and About,
// several at once, each left open a moment or a while, and closed as
// its context ends, as the program closing closes them.

// stressWorkers is how many go at once.
const stressWorkers = 3

// stressWindows opens and closes What's New and About on ga until the
// stress's time is up or ctx ends, when KAKEL_STRESS asks for it.
func stressWindows(ctx context.Context, ga *gunim.App) {
	d, ok := app.StressFor()
	if !ok || !app.StressDoes("about") {
		return
	}
	ctx, stop := context.WithTimeout(ctx, d)
	defer stop()
	// A moment for kakel's own window to open first.
	if !app.StressPause(ctx) {
		return
	}
	done := make(chan int)
	for i := range stressWorkers {
		go func() {
			n := 0
			for app.StressPause(ctx) {
				n++
				wctx, closeIt := context.WithCancel(ctx)
				start := time.Now()
				var err error
				if rand.IntN(2) == 0 {
					err = install.ShowWhatsNew(wctx, ga, app.Installer(), "")
				} else {
					err = install.ShowAbout(wctx, ga, app.Installer(), install.About{})
				}
				if err != nil {
					app.Stressf("%s: window %d.%d didn't open: %v", app.StressEnv, i, n, err)
				} else if took := time.Since(start); took > time.Second {
					app.Stressf("%s: window %d.%d took %s to open", app.StressEnv, i, n, took.Round(time.Millisecond))
				}
				app.StressPause(ctx)
				closeIt()
			}
			done <- n
		}()
	}
	n := 0
	for range stressWorkers {
		n += <-done
	}
	app.Stressf("%s: done, %d What's New and About windows opened and closed with no freeze", app.StressEnv, n)
}
