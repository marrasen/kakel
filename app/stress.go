package app

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marrasen/kakel/machines"
)

// KAKEL_STRESS=1h works kakel hard for that long, to bring out a freeze
// or a crash seen once and never again: workers that each, at random
// intervals, fast and slow, open and close windows, split and close
// panes, type commands into terminals, open Settings and the Servers
// pane, and change the theme and the font size, all at once. main adds
// the windows of gunim's own: What's New and About, and file managers.
// Should the windows stop answering, gunim writes where everything was
// to gunim-hang-*.txt in the temporary folder. The log says how far it
// got.

// StressEnv names the stress, and how long it runs.
const StressEnv = "KAKEL_STRESS"

// StressFor is how long KAKEL_STRESS asks the stress to run, and false
// when it asks for none.
func StressFor() (time.Duration, bool) {
	d, err := time.ParseDuration(os.Getenv(StressEnv))
	return d, err == nil && d > 0
}

// Stressf writes how the stress goes to kakel's log and to stderr.
func Stressf(format string, args ...any) {
	log.Printf(format, args...)
	fmt.Fprintf(os.Stderr, time.Now().Format("15:04:05 ")+format+"\n", args...)
}

// StressDoes reports whether the stress does what name names:
// everything, unless KAKEL_STRESS_ONLY lists, by commas, what to do
// alone: windows, splits, closes, typing, tools, files, looks, and, in
// main, about.
func StressDoes(name string) bool {
	only := os.Getenv(StressEnv + "_ONLY")
	return only == "" || slices.Contains(strings.Split(only, ","), name)
}

// StressPause waits a while, mostly a second or more, sometimes only
// tens of milliseconds, or until ctx ends, and reports whether ctx is
// still going.
func StressPause(ctx context.Context) bool {
	d := time.Duration(500+rand.IntN(3500)) * time.Millisecond
	if rand.IntN(10) < 3 {
		d = time.Duration(20+rand.IntN(180)) * time.Millisecond
	}
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

// stressMost is how many panes the stress lets open before it only
// closes.
const stressMost = 14

// stressCommands are what the stress types into terminals: output that
// scrolls, and what draws in colour.
var stressCommands = []string{
	"ls -la /usr/lib | head -200\r",
	"seq 1 2000\r",
	"echo kakel stress $RANDOM\r",
	"printf '\\033[31mred \\033[32mgreen \\033[34mblue\\033[0m\\n%.0s' $(seq 1 100)\r",
	"clear\r",
}

// stress works kakel hard until d has passed or ctx ends.
func (a *app) stress(ctx context.Context, d time.Duration) {
	ctx, stop := context.WithTimeout(ctx, d)
	defer stop()
	Stressf("%s: working kakel's windows and panes for %s", StressEnv, d)
	var done atomic.Int64
	// on runs fn on the program's goroutine, unless ctx ends first.
	on := func(fn func()) {
		select {
		case a.events <- func() {
			if !a.gone {
				fn()
				done.Add(1)
			}
		}:
		case <-ctx.Done():
		}
	}
	workers := map[string]func(){
		// Windows of their own, with a terminal.
		"windows": func() {
			on(func() {
				if len(a.st.Panes) < stressMost {
					a.newWindow(func() { a.handle(NewTerminal{}) })
				}
			})
		},
		// Splits, in whatever window is in front.
		"splits": func() {
			on(func() {
				if len(a.st.Panes) < stressMost && a.cur != nil && len(a.panesIn(a.cur)) > 0 {
					a.handle(SplitPane{Vertical: rand.IntN(2) == 0})
				}
			})
		},
		// Panes closed, one left open.
		"closes": func() {
			on(func() {
				if len(a.st.Panes) > 1 {
					p := a.st.Panes[rand.IntN(len(a.st.Panes))]
					a.closePane(p.ID)
				}
			})
		},
		// Commands typed into terminals.
		"typing": func() {
			on(func() {
				var terms []string
				for _, p := range a.st.Panes {
					if a.terminal(p.ID) != nil {
						terms = append(terms, p.ID)
					}
				}
				if len(terms) > 0 {
					id := terms[rand.IntN(len(terms))]
					a.terminal(id).Send([]byte(stressCommands[rand.IntN(len(stressCommands))]))
				}
			})
		},
		// Settings and the Servers pane, opened and gone to.
		"tools": func() {
			on(func() {
				if rand.IntN(2) == 0 {
					a.openSettings()
				} else {
					a.showServers()
				}
			})
		},
		// File manager panes, opened and closed.
		"files": func() {
			on(func() {
				if len(a.fmPanes) < 3 && rand.IntN(2) == 0 {
					dir := []string{"", os.TempDir(), "/usr/lib", "/"}[rand.IntN(4)]
					if err := a.filesOn(machines.Local, dir); err != nil {
						Stressf("%s: a file manager pane didn't open: %v", StressEnv, err)
					}
				} else {
					for id := range a.fmPanes {
						a.closePane(id)
						break
					}
				}
			})
		},
		// The theme and the font size, which every window draws again.
		"looks": func() {
			on(func() {
				if rand.IntN(3) == 0 && len(a.st.Themes) > 0 {
					a.handle(PickTheme{Name: a.st.Themes[rand.IntN(len(a.st.Themes))]})
					return
				}
				a.handle(FontSize{Step: []int{-1, 1, 1, -1, 0}[rand.IntN(5)]})
			})
		},
	}
	for name, w := range workers {
		if !StressDoes(name) {
			continue
		}
		go func() {
			for StressPause(ctx) {
				w()
			}
		}()
	}
	t := time.NewTicker(10 * time.Second)
	ticks := 0
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			Stressf("%s: done, %d things done to kakel's windows and panes with no freeze", StressEnv, done.Load())
			return
		case <-t.C:
			n := 0
			if s, ok := onApp(a, func() (int, error) { return len(a.st.Panes), nil }); ok == nil {
				n = s
			}
			var m runtime.MemStats
			// After a collection, so what is in use is what is live.
			runtime.GC()
			runtime.ReadMemStats(&m)
			Stressf("%s: %d things done so far, %d panes open, %d goroutines, Go %d MB in use of %d MB from the system, %s MB resident",
				StressEnv, done.Load(), n, runtime.NumGoroutine(), m.HeapAlloc>>20, m.Sys>>20, stressResident())
			ticks++
			stressProfile(ticks%6 == 0)
		}
	}
}

// stressProfile writes what the heap holds, and every goroutine, to
// kakel-stress-heap.pprof and kakel-stress-goroutines.txt in the
// temporary folder, every ten seconds over the last, for a leak to be found
// in.
func stressProfile(minute bool) {
	write := func(name string, fn func(f *os.File) error) {
		f, err := os.Create(filepath.Join(os.TempDir(), name))
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		if err := fn(f); err != nil {
			Stressf("%s: %s: %v", StressEnv, name, err)
		}
	}
	write("kakel-stress-heap.pprof", func(f *os.File) error { return pprof.WriteHeapProfile(f) })
	if minute {
		// One a minute kept too, for what grew between them.
		write(time.Now().Format("kakel-stress-heap-150405.pprof"), func(f *os.File) error { return pprof.WriteHeapProfile(f) })
	}
	write("kakel-stress-goroutines.txt", func(f *os.File) error { return pprof.Lookup("goroutine").WriteTo(f, 1) })
}

// stressResident is how much of kakel's memory is resident, in MB, as
// Linux counts it: Go's and everything else's, the GL driver's too. "?"
// elsewhere.
func stressResident() string {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return "?"
	}
	for _, l := range strings.Split(string(b), "\n") {
		if kb, ok := strings.CutPrefix(l, "VmRSS:"); ok {
			var n int
			if _, err := fmt.Sscan(strings.TrimSuffix(strings.TrimSpace(kb), " kB"), &n); err == nil {
				return fmt.Sprint(n >> 10)
			}
		}
	}
	return "?"
}
