package app

import (
	"strings"
	"testing"

	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/kakel/winkeys"
)

func TestTheCommandLineIsReadAsGridtermReadsIt(t *testing.T) {
	o, err := ParseOptions([]string{"-font-size", "18", "-e", "top -d 1", "-scrollback", "900", "-shot", "until:$ key:ctrl+k shot:a.png"})
	if err != nil {
		t.Fatal(err)
	}
	if o.fontSize != 18 || !o.sizeSet || o.command != "top -d 1" || o.scrollback != 900 {
		t.Fatalf("read %+v", o)
	}
	for _, bad := range [][]string{
		{"-scrollback", "-1"},
		{"-shot", "until"},
		{"-shot", "key:ctrl+nosuchkey"},
		{"-shot", "require:ok"},
		{"-font", "a.ttf", "-font-family", "Mono"},
	} {
		if _, err := ParseOptions(bad); err == nil {
			t.Errorf("%q was taken", strings.Join(bad, " "))
		}
	}
}

func TestAChordIsPressedAsTheWindowHearsIt(t *testing.T) {
	press, err := winkeys.Parse("ctrl+shift+k")
	if err != nil {
		t.Fatal(err)
	}
	if press.Key != gi.KeyK || press.Mods != gi.ModControl|gi.ModShift {
		t.Fatalf("pressed %+v", press)
	}
}

func TestDashEOpensTheCommandInsteadOfAShell(t *testing.T) {
	a, _ := agentApp(t)
	for len(a.st.Panes) > 0 {
		a.remove(a.st.Panes[0].ID)
	}
	line := echoCommand("from-dash-e")
	a.opts.command = line
	if err := a.openFirst(); err != nil {
		t.Fatal(err)
	}
	if len(a.st.Panes) != 1 || !a.st.Panes[0].Command || a.st.Panes[0].Title != line {
		t.Fatalf("the first pane is %+v", a.st.Panes)
	}
}

// Font sizes run from 8 to 96 logical pixels, 6 to 72 points, as the
// old app took.
func TestFontSizesRunFrom8To96(t *testing.T) {
	for _, c := range []struct{ asked, got float32 }{{4, 8}, {15, 15}, {72, 72}, {96, 96}, {200, 96}} {
		if got := fontSizeIn(c.asked); got != c.got {
			t.Errorf("%v became %v, want %v", c.asked, got, c.got)
		}
	}
	o, err := ParseOptions([]string{"-font-size", "80"})
	if err != nil || o.fontSize != 80 {
		t.Fatalf("-font-size 80 read as %v, %v", o.fontSize, err)
	}
}
