// Package steps is the one vocabulary for driving a pane: what to type,
// what to press, how long to wait, and what to wait for.
//
// Two things read it. An agent sends a list of steps to the MCP server
// to work in a pane; the -shot flag drives the window through a script
// to take screenshots. They were two grammars for the same idea, and a
// step learned in one is a step learned in both.
//
// Not every step means something to both. A screenshot script cannot
// wait for a command to finish, and an agent has nowhere to put a
// image, so each refuses the steps it cannot do -- by name, rather
// than by quietly doing nothing.
//
// A step is a word, a colon, and the rest of the line:
//
//	type:hello       type text, letter for letter
//	key:ctrl+c       press a chord, spelled the way a keymap spells it
//	wait:250         wait that many milliseconds, whatever happens
//	until:$          wait until that text arrives
//	until            wait until whatever is running finishes
//	require:$        go on only if the pane says this now
//	fail:E325        stop if the pane says this now
//	shot:out.png     write what is on screen to a file
//
// And the pointer, at a place in the window in its logical pixels, as a
// screenshot shows it at a scale of 1, with a chord's modifiers in
// front where they are held, as ctrl+40,120:
//
//	click:40,120         press and let go the primary button there
//	rclick:40,120        the secondary button, for a context menu
//	mclick:40,120        the middle button
//	dclick:40,120        two clicks, as a double click
//	move:40,120          move the pointer there, pressing nothing
//	drag:40,120,300,200  press at the first place, move to the second
//	                     in steps, and let go there
//	scroll:40,120,-3     turn the wheel there, by notches: down is
//	                     negative
//	down:40,120          press the primary button there and hold it
//	up:300,200           let it go there
//
// Down, moves and waits, and up make a drag that stops on its way, as
// to hold a tab over another until it opens.
//
// Only a screenshot script has a window to point at: an agent works in
// a pane, and refuses these.
//
// Everything after the first colon is the argument, whole: a colon in
// what is typed or waited for is part of it, which is what lets
// "type:echo a:b" and "until:error: 404" mean what they read as.
package steps

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Kind is what a step does.
type Kind uint8

const (
	// Type puts text into the pane, letter for letter.
	Type Kind = iota + 1

	// Key presses one chord.
	Key

	// Wait is a plain delay: nothing is watched and nothing can end it
	// early.
	Wait

	// Until watches the pane. With text it ends when that text arrives;
	// with none it ends when what is running finishes.
	Until

	// Shot writes what is on screen to a file. Only a screenshot script
	// has anywhere to put one.
	Shot

	// Require stops the list unless the pane says this now.
	Require

	// Fail stops the list if the pane says this now.
	Fail

	// Click presses and lets go a mouse button, Clicks times, at a
	// place in the window.
	Click

	// Move moves the pointer to a place in the window.
	Move

	// Drag presses the primary button at one place, moves to another
	// and lets go there.
	Drag

	// Scroll turns the wheel at a place in the window.
	Scroll

	// Down presses the primary button at a place and holds it, and Up
	// lets it go.
	Down
	Up
)

// Button is a mouse button a Click presses.
type Button uint8

// The buttons.
const (
	Primary Button = iota
	Secondary
	Middle
)

// Step is one thing to do.
type Step struct {
	Kind Kind

	// Text is what Type types, what Until waits for, what Require and
	// Fail look for, and where Shot writes. It is empty for a bare
	// Until, which waits on the pane rather than on any particular
	// words.
	Text string

	// Chord is what Key presses, as it was written. It is not checked
	// here: a screenshot script spells chords the way a keymap does and
	// an agent is held to a shorter list, so whoever runs the step says
	// which names it knows.
	Chord string

	// Wait is how long a Wait step waits.
	Wait time.Duration

	// At is where a pointer step happens, in the window's logical
	// pixels, To where a drag ends, and Notches how far a scroll turns
	// the wheel, down negative.
	At, To  Point
	Notches float32
	// Button and Clicks are what a click presses, and how many times.
	Button Button
	Clicks int
	// Mods are the modifiers held through a pointer step, as a chord
	// names them: ctrl, shift, alt, super.
	Mods []string
}

// Point is a place in the window, in its logical pixels.
type Point struct{ X, Y float32 }

// What a list of steps may not go past.
//
// A cap on each, because they run out differently: a thousand short
// steps is a script nobody wrote by hand, and one step carrying a
// megabyte is a pane being filled rather than typed into.
const (
	// MostSteps is how many steps one list may hold.
	MostSteps = 64

	// MostText is how many bytes all the typing in one list may come to.
	MostText = 8192

	// LongestWait is the longest a wait step may ask for. The same
	// bound a wait for the pane has, so neither way of asking parks a
	// window for an afternoon.
	LongestWait = 5 * time.Minute
)

// Parse reads one step.
func Parse(step string) (Step, error) {
	word, arg, hasArg := strings.Cut(step, ":")
	switch word {
	case "type":
		if arg == "" {
			return Step{}, fmt.Errorf("%q types nothing: put what to type after the colon", step)
		}
		return Step{Kind: Type, Text: arg}, nil
	case "key":
		if arg == "" {
			return Step{}, fmt.Errorf("%q presses nothing: put the key after the colon", step)
		}
		return Step{Kind: Key, Chord: arg}, nil
	case "shot":
		if arg == "" {
			return Step{}, fmt.Errorf("%q writes nowhere: put the file after the colon", step)
		}
		return Step{Kind: Shot, Text: arg}, nil
	case "wait":
		ms, err := strconv.Atoi(arg)
		if err != nil || ms < 0 {
			return Step{}, fmt.Errorf("%q is not a number of milliseconds", step)
		}
		got := time.Duration(ms) * time.Millisecond
		if got > LongestWait {
			return Step{}, fmt.Errorf("%q waits longer than %s, which is the longest a step may wait",
				step, LongestWait)
		}
		return Step{Kind: Wait, Wait: got}, nil
	case "require", "fail":
		if arg == "" {
			return Step{}, fmt.Errorf("%q looks for nothing: put the text after the colon", step)
		}
		kind := Require
		if word == "fail" {
			kind = Fail
		}
		return Step{Kind: kind, Text: arg}, nil
	case "click", "rclick", "mclick", "dclick":
		return pointer(word, arg, step)
	case "move", "drag", "scroll", "down", "up":
		return pointer(word, arg, step)
	case "until":
		// A bare "until" waits on the pane rather than on words, so it
		// is the one step that needs no colon. "until:" with nothing
		// after it is the same thing said clumsily, and is taken as it
		// reads.
		_ = hasArg
		return Step{Kind: Until, Text: arg}, nil
	}
	return Step{}, fmt.Errorf("%q is not a step; they are type:, key:, wait:, until,"+
		" require:, fail:, shot:, click:, rclick:, mclick:, dclick:, move:, drag:, scroll:, down: and up:", step)
}

// modNames are the modifiers a pointer step may hold.
var modNames = map[string]bool{"ctrl": true, "shift": true, "alt": true, "super": true}

// pointer reads a pointer step: word, then its argument, a chord's
// modifiers and the numbers it takes, as ctrl+40,120.
func pointer(word, arg, step string) (Step, error) {
	var mods []string
	nums := arg
	if i := strings.LastIndexByte(arg, '+'); i >= 0 {
		nums = arg[i+1:]
		for _, m := range strings.Split(arg[:i], "+") {
			m = strings.ToLower(strings.TrimSpace(m))
			if !modNames[m] {
				return Step{}, fmt.Errorf("%q holds %q, and the keys a pointer step may hold are ctrl, shift, alt and super", step, m)
			}
			mods = append(mods, m)
		}
	}
	want := map[string]int{"drag": 4, "scroll": 3}[word]
	if want == 0 {
		want = 2
	}
	var vals []float32
	for _, f := range strings.Split(nums, ",") {
		v, err := strconv.ParseFloat(strings.TrimSpace(f), 32)
		if err != nil {
			vals = nil
			break
		}
		vals = append(vals, float32(v))
	}
	if len(vals) != want {
		shape := map[int]string{2: "x,y", 3: "x,y,notches", 4: "x,y,x2,y2"}[want]
		return Step{}, fmt.Errorf("%q wants %s, numbers in the window's pixels", step, shape)
	}
	out := Step{At: Point{vals[0], vals[1]}, Mods: mods}
	switch word {
	case "click", "rclick", "mclick", "dclick":
		out.Kind, out.Clicks = Click, 1
		switch word {
		case "rclick":
			out.Button = Secondary
		case "mclick":
			out.Button = Middle
		case "dclick":
			out.Clicks = 2
		}
	case "move":
		out.Kind = Move
	case "down":
		out.Kind = Down
	case "up":
		out.Kind = Up
	case "drag":
		out.Kind, out.To = Drag, Point{vals[2], vals[3]}
	case "scroll":
		out.Kind, out.Notches = Scroll, vals[2]
	}
	return out, nil
}

// ParseAll reads a list of steps, one per string, and refuses a list
// that is past what a list may hold.
func ParseAll(list []string) ([]Step, error) {
	if len(list) == 0 {
		return nil, fmt.Errorf("no steps")
	}
	if len(list) > MostSteps {
		return nil, fmt.Errorf("%d steps, and %d is the most a list may hold", len(list), MostSteps)
	}
	out := make([]Step, 0, len(list))
	typed := 0
	for i, one := range list {
		step, err := Parse(one)
		if err != nil {
			return nil, fmt.Errorf("step %d: %w", i+1, err)
		}
		if step.Kind == Type {
			typed += len(step.Text)
		}
		out = append(out, step)
	}
	if typed > MostText {
		return nil, fmt.Errorf("the steps type %d bytes, and %d is the most one list may type",
			typed, MostText)
	}
	return out, nil
}

// ParseLine reads a script written as one line, with the steps
// separated by spaces.
//
// It is how a command line hands in a script, where a list of strings
// has nowhere to live. Nothing typed or waited for can hold a space
// then, which is the price of writing it on one line: ParseAll takes
// the same steps with their spaces intact.
func ParseLine(script string) ([]Step, error) {
	return ParseAll(strings.Fields(script))
}

// String writes a step the way it was read, for an answer that has to
// name the step it stopped at.
func (s Step) String() string {
	switch s.Kind {
	case Type:
		return "type:" + s.Text
	case Key:
		return "key:" + s.Chord
	case Wait:
		return "wait:" + strconv.FormatInt(s.Wait.Milliseconds(), 10)
	case Until:
		if s.Text == "" {
			return "until"
		}
		return "until:" + s.Text
	case Shot:
		return "shot:" + s.Text
	case Require:
		return "require:" + s.Text
	case Fail:
		return "fail:" + s.Text
	case Click, Move, Drag, Scroll, Down, Up:
		return s.pointerString()
	}
	return "an unknown step"
}

// pointerString writes a pointer step the way it was read.
func (s Step) pointerString() string {
	word := map[Kind]string{Move: "move", Drag: "drag", Scroll: "scroll", Down: "down", Up: "up"}[s.Kind]
	if s.Kind == Click {
		word = map[Button]string{Primary: "click", Secondary: "rclick", Middle: "mclick"}[s.Button]
		if s.Clicks == 2 {
			word = "dclick"
		}
	}
	num := func(v float32) string { return strconv.FormatFloat(float64(v), 'f', -1, 32) }
	arg := num(s.At.X) + "," + num(s.At.Y)
	switch s.Kind {
	case Drag:
		arg += "," + num(s.To.X) + "," + num(s.To.Y)
	case Scroll:
		arg += "," + num(s.Notches)
	}
	if len(s.Mods) > 0 {
		arg = strings.Join(s.Mods, "+") + "+" + arg
	}
	return word + ":" + arg
}
