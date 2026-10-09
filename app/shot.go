package app

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/steps"
	"github.com/marrasen/kakel/winkeys"
)

// -shot drives the window through a short script and writes what is on
// screen to PNG files: for looking at the pixels, which a test cannot
// check. The script is the steps package's, on one line:
//
//	wait:250         wait a quarter of a second
//	until:$          wait until that text arrives on the focused pane
//	key:ctrl+k       press a chord, spelled the way a keymap spells it
//	type:hello       type text, a character at a time
//	shot:out.png     write the window to a file
//	click:40,120     click there, in the window's logical pixels; also
//	                 rclick:, mclick:, dclick:, move:, drag:x,y,x2,y2,
//	                 scroll:x,y,notches, down: and up:, with keys held
//	                 as click:ctrl+40,120
//
// Keys and the pointer arrive as a keyboard and a mouse send them: a
// key that types sends its press, the text and its release, and a
// click moves the pointer there first.
//
// The window closes when the script ends. A script whose until ran out
// fails the run, so nothing reads last time's images as new ones.

// longestUntil is how long an until step waits before the script gives
// up.
const longestUntil = 30 * time.Second

// stepGap is the time left after each step, so what it did is drawn
// before the next one looks.
const stepGap = 50 * time.Millisecond

// parseShot reads a screenshot script, refusing what it cannot do
// before a window opens.
func parseShot(script string) ([]steps.Step, error) {
	list, err := steps.ParseLine(script)
	if err != nil {
		return nil, err
	}
	for i, step := range list {
		switch step.Kind {
		case steps.Key:
			if _, err := winkeys.Parse(step.Chord); err != nil {
				return nil, fmt.Errorf("step %d, %q: %w", i+1, step, err)
			}
		case steps.Until:
			if step.Text == "" {
				return nil, fmt.Errorf("step %d, %q: a bare until waits for the"+
					" shell to say a command has finished, which a screenshot"+
					" script has no way to ask about. Give it the text to wait for",
					i+1, step)
			}
		case steps.Require, steps.Fail:
			return nil, fmt.Errorf("step %d, %q: the guards are for a list of steps"+
				" sent to a pane, where there is somebody to tell that it stopped",
				i+1, step)
		}
	}
	return list, nil
}

// runShot drives the window through the script, on a goroutine of its
// own, and closes the window at the end. Why it gave up, if it did, is
// kept in shotErr for the run to end with.
func (a *app) runShot(list []steps.Step) {
	err := a.shoot(a.ctx, list)
	a.events <- func() {
		a.shotErr = err
		a.exitNow()
	}
}

// shoot runs the steps.
func (a *app) shoot(ctx context.Context, list []steps.Step) error {
	typed := false
	// before is the pane as it was just before the last key or text
	// went in, and fresh says the step before this one put it there:
	// an answer can arrive before the next step looks, and what was
	// typed before that key is on the pane already.
	before, fresh := "", false
	now := func() string {
		s, _ := onApp(a, func() (string, error) { return a.paneNow(), nil })
		return s
	}
	pause := func(d time.Duration) error {
		select {
		case <-time.After(d):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	// front is the client of the newest window open, which a key, text
	// or a shot goes to: one a step opened, as a pane in a window of its
	// own does, takes the steps after it. The newest rather than the one
	// with the keyboard, as a screen with no window manager gives none
	// the keyboard.
	front := func() gunim.Client {
		c, _ := onApp(a, func() (gunim.Client, error) {
			if live := a.liveWins(); len(live) > 0 {
				return live[len(live)-1].c, nil
			}
			return a.c, nil
		})
		return c
	}
	for _, step := range list {
		switch step.Kind {
		case steps.Wait:
			if err := pause(step.Wait); err != nil {
				return err
			}
		case steps.Until:
			// What the pane held as the step began does not count, so
			// the step waits for the text to arrive. Before anything is
			// typed there is nothing it could be an answer to, and what
			// is there already counts.
			was := ""
			switch {
			case fresh:
				was = before
			case typed:
				was = now()
			}
			for deadline := time.Now().Add(longestUntil); ; {
				if strings.Contains(agent.AddedSince(was, now()), step.Text) {
					break
				}
				if time.Now().After(deadline) {
					return fmt.Errorf("until:%s: nothing said it in %s, and the steps after it were not run", step.Text, longestUntil)
				}
				if err := pause(stepGap); err != nil {
					return err
				}
			}
		case steps.Key:
			press, err := winkeys.Parse(step.Chord)
			if err != nil {
				return err
			}
			before = now()
			// As a keyboard sends it: a key that types, as Space, sends
			// its press, its text and its release.
			if err := pressKey(sendTo(ctx, front()), press); err != nil {
				return err
			}
			typed = true
		case steps.Type:
			before = now()
			if err := typeText(sendTo(ctx, front()), step.Text); err != nil {
				return err
			}
			typed = true
		case steps.Click, steps.Move, steps.Drag, steps.Scroll, steps.Down, steps.Up:
			if err := point(ctx, sendTo(ctx, front()), step); err != nil {
				return err
			}
		case steps.Shot:
			img, err := front().Shot(ctx)
			if err != nil {
				return fmt.Errorf("shot:%s: %w", step.Text, err)
			}
			f, err := os.Create(step.Text)
			if err != nil {
				return err
			}
			if err := errors.Join(png.Encode(f, img), f.Close()); err != nil {
				return fmt.Errorf("shot:%s: %w", step.Text, err)
			}
		}
		fresh = step.Kind == steps.Key || step.Kind == steps.Type
		if err := pause(stepGap); err != nil {
			return err
		}
	}
	return nil
}

// sendTo hands events to the window c, as its driver would.
func sendTo(ctx context.Context, c gunim.Client) func(gi.Event) error {
	return func(ev gi.Event) error { return c.Input(ctx, ev) }
}

// paneNow is what the focused pane shows, for an until step to watch:
// empty when the focus is on no terminal.
func (a *app) paneNow() string {
	t := a.terminal(a.st.Focus)
	if t == nil {
		return ""
	}
	return t.ReadLines(t.Size().Rows).Text
}
