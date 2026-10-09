package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/steps"
)

// sent records the events a script sends, written short.
type sent []string

func (s *sent) send(ev gi.Event) error {
	switch e := ev.(type) {
	case gi.KeyPress:
		*s = append(*s, fmt.Sprintf("press:%d:%d:typed=%v:char=%q", e.Key, e.Mods, e.Typed, e.Char))
	case gi.TextInput:
		*s = append(*s, "text:"+e.Text)
	case gi.KeyRelease:
		*s = append(*s, fmt.Sprintf("release:%d", e.Key))
	case gi.PointerMove:
		*s = append(*s, fmt.Sprintf("move:%v,%v", e.Pos.X, e.Pos.Y))
	case gi.PointerDown:
		*s = append(*s, fmt.Sprintf("down:%v,%v:b%d:c%d:m%d", e.Pos.X, e.Pos.Y, e.Button, e.Clicks, e.Mods))
	case gi.PointerUp:
		*s = append(*s, fmt.Sprintf("up:%v,%v", e.Pos.X, e.Pos.Y))
	case gi.Scroll:
		*s = append(*s, fmt.Sprintf("scroll:%v:%v", e.Delta.Y, e.Notches.Y))
	}
	return nil
}

// Space, as a keyboard sends it: its press, marked as typing, the space
// itself, and the release; with Control held it types nothing.
func TestAKeyArrivesAsAKeyboardSendsIt(t *testing.T) {
	var s sent
	if err := pressKey(s.send, gi.KeyPress{Key: gi.KeySpace}); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("press:%d:0:typed=true:char=' ' text:  release:%d", gi.KeySpace, gi.KeySpace)
	if got := strings.Join(s, " "); got != want {
		t.Fatalf("Space sent %q, want %q", got, want)
	}
	s = nil
	if err := pressKey(s.send, gi.KeyPress{Key: gi.KeyC, Mods: gi.ModControl}); err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 || !strings.Contains(s[0], "typed=false:char='c'") {
		t.Fatalf("Ctrl+C sent %q", s)
	}
	s = nil
	if err := pressKey(s.send, gi.KeyPress{Key: gi.KeyEnter}); err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 || !strings.Contains(s[0], "typed=false") {
		t.Fatalf("Enter sent %q", s)
	}
}

// Text is typed a key at a time, Shift held for a capital, and a
// character no key types arrives as text alone.
func TestTextIsTypedAKeyAtATime(t *testing.T) {
	var s sent
	if err := typeText(s.send, "Hé!"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(s, " ")
	want := fmt.Sprintf("press:%d:%d:typed=true:char='h' text:H release:%d text:é press:%d:%d:typed=true:char='1' text:! release:%d",
		gi.KeyH, gi.ModShift, gi.KeyH, gi.Key1, gi.ModShift, gi.Key1)
	if got != want {
		t.Fatalf("typing sent\n%q\nwant\n%q", got, want)
	}
}

// The pointer goes where a step points before it presses, and a drag
// moves the whole way with the button down.
func TestThePointerMovesThereFirst(t *testing.T) {
	var s sent
	click, _ := steps.Parse("dclick:ctrl+40,12")
	if err := point(context.Background(), s.send, click); err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("move:40,12 down:40,12:b0:c1:m%d up:40,12 down:40,12:b0:c2:m%d up:40,12", gi.ModControl, gi.ModControl)
	if got := strings.Join(s, " "); got != want {
		t.Fatalf("a double click sent %q, want %q", got, want)
	}
	s = nil
	drag, _ := steps.Parse("drag:0,0,120,60")
	if err := point(context.Background(), s.send, drag); err != nil {
		t.Fatal(err)
	}
	if len(s) != dragSteps+3 || s[1] != "down:0,0:b0:c1:m0" || s[len(s)-2] != "move:120,60" || s[len(s)-1] != "up:120,60" {
		t.Fatalf("a drag sent %q", s)
	}
	s = nil
	wheel, _ := steps.Parse("scroll:5,5,-2")
	if err := point(context.Background(), s.send, wheel); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(s, " "); got != "move:5,5 scroll:-80:-2" {
		t.Fatalf("a scroll sent %q", got)
	}
}
