package app

import (
	"strconv"
	"testing"
)

// New terminals here are called after their shell, numbered in turn.
func TestNewTerminalsAreNumberedInTurn(t *testing.T) {
	a, _ := agentApp(t)
	for range 2 {
		if err := a.open("", Placement{}); err != nil {
			t.Fatal(err)
		}
	}
	// Called after their shell: the first by its name, the next by it
	// and a number.
	name := a.localShellName(nil)
	for i, p := range a.st.Panes {
		want := name
		if i > 0 {
			want += " " + strconv.Itoa(i+1)
		}
		if p.Title != want {
			t.Fatalf("pane %d is called %q, want %q", i, p.Title, want)
		}
	}
}
