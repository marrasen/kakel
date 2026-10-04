package term

import (
	"testing"
	"time"

	"github.com/marrasen/kakel/ui"
)

// mirrorSession is a fake session showing a program another terminal
// runs and answers for, as a pane in another kakel window does.
type mirrorSession struct{ *fakeSession }

func (mirrorSession) Mirrors() bool { return true }

// A terminal answers what a program asks of it, unless its session
// mirrors another terminal, which has answered: a second answer reached
// the program late, typed in front of what the user typed next.
func TestAMirrorAnswersNothing(t *testing.T) {
	const ask = "\x1b[c\x1b[6n\x1b[>q"
	for _, mirror := range []bool{false, true} {
		f := newFakeSession()
		cfg := Config{Session: f, Program: "kakel test"}
		if mirror {
			cfg.Session = mirrorSession{f}
		}
		term, err := New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		term.Layout(ui.Size{Cols: 40, Rows: 5})
		f.feed(t, term, ask)
		// Answers go out on a goroutine of their own.
		time.Sleep(50 * time.Millisecond)
		sent := f.sentText()
		switch {
		case mirror && sent != "":
			t.Errorf("a mirror answered %q", sent)
		case !mirror && sent == "":
			t.Error("a terminal of its own answered nothing")
		}
		_ = term.Close()
	}
}
