package keys

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/ui"
)

var (
	chordT  = ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}
	chordW  = ui.Chord{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}
	chordJ  = ui.Chord{Key: input.KeyJ, Mods: input.ModCtrl | input.ModAlt}
	builtIn = []ui.Binding{{Chord: chordT, ID: "conn.terminal"}, {Chord: chordW, ID: "pane.close"}}
)

// The file only ever holds differences, whichever way a change came.
func TestApplyKeepsOnlyDifferences(t *testing.T) {
	have := Apply(nil, builtIn, Op{Chord: chordJ, Command: "pane.close"})
	if len(have) != 1 || have[0].Chord != chordJ || have[0].Command != "pane.close" {
		t.Fatalf("binding a free chord gave %v", have)
	}
	have = Apply(have, builtIn, Op{Chord: chordW, Unbind: true})
	if len(have) != 2 {
		t.Fatalf("taking a built-in away gave %v", have)
	}
	back := Apply(have, builtIn, Op{Chord: chordW, Command: "pane.close"})
	if len(back) != 1 || back[0].Chord != chordJ {
		t.Fatalf("putting a built-in back kept a line for it: %v", back)
	}
	if got := Apply(have, builtIn, Op{Command: "pane.close", Reset: true}); len(got) != 0 {
		t.Fatalf("resetting the command left %v", got)
	}
	taken := Apply(nil, builtIn, Op{Chord: chordW, Command: "conn.terminal"})
	if got := Apply(taken, builtIn, Op{Command: "pane.close", Reset: true}); len(got) != 0 {
		t.Fatalf("resetting a command whose key was taken left %v", got)
	}
	if got := Apply(have, builtIn, Op{ResetAll: true}); got != nil {
		t.Fatalf("resetting everything left %v", got)
	}
}

// A line for a command this kakel does not know stays through an edit.
func TestApplyKeepsWhatItDoesNotKnow(t *testing.T) {
	odd := []Change{{Chord: chordJ, Command: "from.later.kakel"}}
	got := Apply(odd, builtIn, Op{Chord: chordW, Unbind: true})
	if !slices.ContainsFunc(got, func(c Change) bool { return c.Command == "from.later.kakel" }) {
		t.Fatalf("an edit dropped the unknown line: %v", got)
	}
}

// Save writes what Load reads, over a file already there, with no
// temporary file left beside it.
func TestSaveAndLoadAgain(t *testing.T) {
	dir := t.TempDir()
	at := filepath.Join(dir, File)
	if err := Save(at, Apply(nil, builtIn, Op{Chord: chordW, Unbind: true})); err != nil {
		t.Fatal(err)
	}
	if err := Save(at, Apply(nil, builtIn, Op{Chord: chordJ, Command: "pane.close"})); err != nil {
		t.Fatal(err)
	}
	got, err := Load(at)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Chord != chordJ || got[0].Command != "pane.close" {
		t.Fatalf("read back %v", got)
	}
	names, _ := os.ReadDir(dir)
	if len(names) != 1 {
		t.Fatalf("the folder holds %v", names)
	}
}

// The digits can be shortcuts with a modifier, and not alone.
func TestDigitsBindOnlyWithAModifier(t *testing.T) {
	if err := Bindable(ui.Chord{Key: input.Key1, Mods: input.ModCtrl}); err != nil {
		t.Fatal(err)
	}
	if err := Bindable(ui.Chord{Key: input.Key9}); err == nil {
		t.Fatal("a plain digit was taken")
	}
}
