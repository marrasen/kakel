package guestlook_test

import (
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/themes"
)

// all is true of every pane.
func all(string) bool { return true }

// window is the tree of a window with a split, tabs, and a share of it.
func window() *dock.Node {
	l := dock.NewLayout(true, "p1", "p2")
	g := l.GroupOf("p1").ID
	l.Add("p3", dock.Target{At: g, Side: dock.Right})
	l.Add("p4", dock.Target{At: "p3", Side: dock.Bottom})
	l.SetShare(l.Windows[0].Root.ID, 0.3)
	l.Show("p1")
	return l.Windows[0].Root
}

// A layout reads back as it was written, for a window of its own.
func TestALayoutReadsBackAsItWasWritten(t *testing.T) {
	root := window()
	raw, err := guestlook.Layout(root, all)
	if err != nil {
		t.Fatal(err)
	}
	got, err := guestlook.ReadLayout(raw)
	if err != nil {
		t.Fatal(err)
	}
	if w := got.Window(guestlook.LayoutWindow); w == nil || !reflect.DeepEqual(w.Root, root) {
		t.Fatalf("it came back as %s", raw)
	}
	if !got.Grouped {
		t.Error("its panes lost their groups")
	}
	if len(raw) > 1000 {
		t.Errorf("a window of four panes is %d bytes", len(raw))
	}
}

// What is not shown is in no part of what is written, and the rest is
// whole without it.
func TestAPaneNotShownIsLeftOutOfTheLayout(t *testing.T) {
	root := window()
	raw, err := guestlook.Layout(root, func(id string) bool { return id == "p2" || id == "p4" })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "p1") || strings.Contains(string(raw), "p3") {
		t.Errorf("written: %s", raw)
	}
	l, err := guestlook.ReadLayout(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := l.Shown(guestlook.LayoutWindow); !slices.Equal(got, []string{"p2", "p4"}) {
		t.Errorf("it shows %v", got)
	}
	if got := root.Items(); len(got) != 4 {
		t.Errorf("the window itself is left with %v", got)
	}
}

// An empty window is one at the other end too.
func TestAnEmptyWindowIsALayout(t *testing.T) {
	raw, err := guestlook.Layout(nil, all)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []json.RawMessage{raw, nil} {
		l, err := guestlook.ReadLayout(raw)
		if err != nil || l.Window(guestlook.LayoutWindow) == nil || len(l.Items()) != 0 {
			t.Errorf("%q reads as %+v, %v", raw, l, err)
		}
	}
}

// Whatever a layout says, what is read is whole, or refused.
func TestALayoutIsWholeHoweverItIsWritten(t *testing.T) {
	deep := strings.Repeat(`{"id":"#1","kind":"row","kids":[{"id":"p","kind":"item"},`, 100) + `{"id":"q","kind":"item"}` + strings.Repeat("]}", 100)
	wide := `{"id":"#1","kind":"group","kids":[` + strings.TrimSuffix(strings.Repeat(`{"id":"p","kind":"item"},`, 2000), ",") + "]}"
	for name, raw := range map[string]string{
		"not JSON": `{"id":`, "not a tree": `[1]`, "too deep": deep, "too many": wide,
		"too long a name": `{"kind":"item","id":"` + strings.Repeat("x", 200) + `"}`,
		"too large":       `{"kind":"item","id":"` + strings.Repeat("x", 300<<10) + `"}`,
	} {
		if l, err := guestlook.ReadLayout(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: read as %+v", name, l)
		} else if l.Window(guestlook.LayoutWindow) == nil {
			t.Errorf("%s: there is no window to show in its place", name)
		}
	}
	for name, raw := range map[string]string{
		"a pane twice":        `{"id":"#1","kind":"group","front":"p","kids":[{"id":"p","kind":"item"},{"id":"p","kind":"item"}]}`,
		"a kind of its own":   `{"id":"#1","kind":"group","kids":[{"id":"x","kind":"window"},{"id":"p","kind":"item"}]}`,
		"no tab in front":     `{"id":"#1","kind":"group","front":"gone","kids":[{"id":"p","kind":"item"}]}`,
		"a split of one":      `{"id":"#1","kind":"row","share":7,"kids":[{"id":"p","kind":"item"}]}`,
		"a split's odd name":  `{"id":"#<b>","kind":"group","kids":[{"id":"p","kind":"item"}]}`,
		"a split with no kid": `{"id":"#2","kind":"column","kids":[null,{"id":"p","kind":"item"}]}`,
		"a pane with kids":    `{"id":"p","kind":"item","kids":[{"id":"q","kind":"item"}]}`,
	} {
		l, err := guestlook.ReadLayout(json.RawMessage(raw))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got := l.Items(); !slices.Equal(got, []string{"p"}) {
			t.Errorf("%s: it holds %v", name, got)
		}
		if got := l.Shown(guestlook.LayoutWindow); !slices.Equal(got, []string{"p"}) {
			t.Errorf("%s: it shows %v", name, got)
		}
		again, err := json.Marshal(l.Window(guestlook.LayoutWindow).Root)
		if err != nil || strings.Contains(string(again), "<b>") || strings.Contains(string(again), "share") {
			t.Errorf("%s: it is %s, %v", name, again, err)
		}
	}
}

// Every theme kakel comes with makes the same at the other end, with the
// colours the theme editor changed in it, and what its file says of its
// shape.
func TestAThemeMakesTheSameAtTheOtherEnd(t *testing.T) {
	room, lively := 1.4, themes.MotionLively
	for i, th := range themes.Built() {
		if i == 0 {
			th.Edits = json.RawMessage(`{"kakel.faint": "#123456"}`)
			th.Shape, th.Motion = &themes.Shape{Room: &room}, lively
		}
		made, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := guestlook.Theme{Look: made, FontSize: 17, Font: "Cascadia Code"}.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) > 4<<10 {
			t.Errorf("%s is %d bytes, and goes with every state", th.Name, len(raw))
		}
		got, err := guestlook.ReadTheme(raw)
		if err != nil {
			t.Fatalf("%s: %v", th.Name, err)
		}
		if got.FontSize != 17 || got.Font != "Cascadia Code" {
			t.Errorf("%s is drawn at %v in %q", th.Name, got.FontSize, got.Font)
		}
		if got.Look.Palette != made.Palette {
			t.Errorf("%s has other colours in its terminals", th.Name)
		}
		if !reflect.DeepEqual(got.Look.Theme, made.Theme) || !reflect.DeepEqual(got.Look.Content, made.Content) {
			t.Errorf("%s is made into another theme", th.Name)
		}
	}
}

// Of the theme editor's changes a page keeps the colours, but for its
// own, whatever is said of the rest; what it says itself it draws in the
// theme without any.
func TestAPageKeepsOnlyTheColoursOfEdits(t *testing.T) {
	own := theme.Color("kakel.page.test", color.NRGBA{A: 0xff})
	// What each edit of the host's says is wrong with it.
	log.SetOutput(io.Discard)
	defer log.SetOutput(os.Stderr)
	base := themes.Built()[0]
	plain, err := look.Of(base)
	if err != nil {
		t.Fatal(err)
	}
	read := func(edits string) guestlook.Theme {
		t.Helper()
		th := base
		th.Edits = json.RawMessage(edits)
		made, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		// As it comes from a host that edits what it likes.
		raw, err := guestlook.Theme{Look: made, FontSize: 15}.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		got, err := guestlook.ReadTheme(raw)
		if err != nil {
			t.Fatalf("%s: %v", edits, err)
		}
		return got
	}
	extremes := []string{`1e9`, `-1e9`, `3e38`, `0`, `""`, `"#00000000"`, `null`, `[1e9]`, `{"response":1e9,"damping":0}`, `{"top":1e9,"left":-1e9}`, `true`}
	for _, info := range theme.Tokens() {
		colour := info.Kind == theme.KindColor || info.Kind == theme.KindForeground
		for _, v := range extremes {
			got := read(fmt.Sprintf(`{%q: %s, "no.such": 1, "kakel.faint": "#123456"}`, info.Key, v))
			if !reflect.DeepEqual(got.Look.Plain, plain.Plain) || !got.Readable() {
				t.Fatalf("%s as %s changes what the page is drawn in", info.Key, v)
			}
			if colour && info.Key != own.Key() {
				continue
			}
			for what, th := range map[string][2]theme.Theme{"the panes'": {got.Look.Content, plain.Content}, "the tabs'": {got.Look.Theme, plain.Theme}} {
				is, _ := th[0].Value(info.Key)
				was, _ := th[1].Value(info.Key)
				if info.Key != look.Faint.Key() && !reflect.DeepEqual(is, was) {
					t.Errorf("%s as %s: in %s theme it is %v", info.Key, v, what, is)
				}
			}
		}
	}
	got := read(`{"kakel.faint": "#123456", "ink": "#654321", "kakel.page.test": "#ffffff"}`)
	if c := look.Faint.In(got.Look.Content); c.R != 0x12 {
		t.Errorf("an edited colour is %v", c)
	}
	if c := widget.Ink.In(got.Look.Theme); c.R != 0x65 {
		t.Errorf("an edited colour is %v", c)
	}
	for _, raw := range []string{`[]`, `"x"`, `null`, `7`} {
		read(raw)
	}
}

// A theme whose words do not stand out from their ground is told, and all
// of kakel's do.
func TestAThemeThatCannotBeReadIsTold(t *testing.T) {
	for _, th := range themes.Built() {
		made, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		if !(guestlook.Theme{Look: made}).Readable() {
			t.Errorf("%s cannot be read", th.Name)
		}
	}
	for _, c := range [][2]string{{"#101010", "#101010"}, {"#303030", "#101010"}, {"#eeeeee", "#ffffff"}} {
		th := themes.Built()[0]
		th.FG, th.BG = c[0], c[1]
		made, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		if (guestlook.Theme{Look: made}).Readable() {
			t.Errorf("%s on %s can be read", c[0], c[1])
		}
	}
}

// Light and dark are told apart by the ground.
func TestALightThemeIsToldFromADarkOne(t *testing.T) {
	for _, c := range []struct {
		fg, bg string
		dark   bool
	}{{"#ffffff", "#101010", true}, {"#101010", "#ffffff", false}, {"#ffff00", "#0000aa", true}} {
		th := themes.Built()[0]
		th.FG, th.BG = c.fg, c.bg
		made, err := look.Of(th)
		if err != nil {
			t.Fatal(err)
		}
		if got := (guestlook.Theme{Look: made}).Dark(); got != c.dark {
			t.Errorf("%s on %s is dark: %v", c.fg, c.bg, got)
		}
	}
}

// What is not a theme is refused, and what it says past its limits is
// held to them.
func TestAThemeIsHeldToItsLimits(t *testing.T) {
	for name, raw := range map[string]string{
		"nothing": "", "not JSON": "{", "no colours": `{"theme":{"name":"x"}}`,
		"too large": `{"theme":{"name":"` + strings.Repeat("x", 200<<10) + `"}}`,
	} {
		if _, err := guestlook.ReadTheme(json.RawMessage(raw)); err == nil {
			t.Errorf("%s was read as a theme", name)
		}
	}
	made, err := look.Of(themes.Built()[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, size := range []float32{-1, 0, 1e9} {
		raw, err := guestlook.Theme{Look: made, FontSize: size, Font: strings.Repeat("x", 500)}.Bytes()
		if err != nil {
			t.Fatal(err)
		}
		got, err := guestlook.ReadTheme(raw)
		if err != nil || got.FontSize < guestlook.LeastFontSize || got.FontSize > guestlook.MostFontSize || got.Font != "" {
			t.Errorf("text of %v is %v in %d characters, %v", size, got.FontSize, len(got.Font), err)
		}
	}
	if _, err := (guestlook.Theme{}).Bytes(); err == nil {
		t.Error("no theme was written down as one")
	}
}
