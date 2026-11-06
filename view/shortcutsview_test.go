package view

import (
	"slices"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/keys"
	"github.com/marrasen/kakel/ui"
)

// helpStage opens the editor.
func helpStage(t *testing.T) (*Window, func(app.State), app.State) {
	t.Helper()
	win, _, publish := windowStageOf(t, geom.Sz(1100, 800))
	st := app.State{Panes: []app.Pane{{ID: "p1", Kind: app.KindHelp, Title: "Shortcuts and Commands"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"}
	publish(st)
	lastWindow.Frame(time.Second / 60)
	return win, publish, st
}

func lineOf(t *testing.T, p *helpPane, id string) *helpLine {
	t.Helper()
	for _, l := range p.lines {
		if l.id == id {
			return l
		}
	}
	t.Fatalf("no line for %s", id)
	return nil
}

// A key taken from another command asks first; one that costs something
// warns; a plain key and a key already on the row do neither.
func TestTheEditorJudgesWhatIsPressed(t *testing.T) {
	win, _, _ := helpStage(t)
	p := win.help
	closePane := lineOf(t, p, "pane.close")
	ctrl := gi.ModControl
	for _, c := range []struct {
		name    string
		press   gi.KeyPress
		refused bool
		same    bool
		button  string
	}{
		{name: "a chord taken", press: gi.KeyPress{Key: gi.KeyT, Mods: ctrl | gi.ModShift}, button: "Move Here"},
		{name: "its own chord", press: gi.KeyPress{Key: gi.KeyW, Mods: ctrl | gi.ModShift}, same: true},
		{name: "a plain letter", press: gi.KeyPress{Key: gi.KeyQ}, refused: true},
		{name: "a plain digit", press: gi.KeyPress{Key: gi.Key3}, refused: true},
		{name: "a key that types", press: gi.KeyPress{Key: gi.KeyQ, Mods: ctrl | gi.ModAlt, Typed: true}, refused: true},
		{name: "a key programs read", press: gi.KeyPress{Key: gi.KeyR, Mods: ctrl}, button: "Use Anyway"},
		{name: "AltGr's", press: gi.KeyPress{Key: gi.KeyQ, Mods: ctrl | gi.ModAlt}, button: "Use Anyway"},
		{name: "a free digit", press: gi.KeyPress{Key: gi.Key1, Mods: ctrl}},
		{name: "a free function key", press: gi.KeyPress{Key: gi.KeyF7}},
		{name: "a plain arrow", press: gi.KeyPress{Key: gi.KeyLeft}, button: "Use Anyway"},
		{name: "plain Delete", press: gi.KeyPress{Key: gi.KeyDelete}, button: "Use Anyway"},
		{name: "Alt and a digit", press: gi.KeyPress{Key: gi.Key2, Mods: gi.ModAlt}, button: "Use Anyway"},
		{name: "no key kakel has", press: gi.KeyPress{Key: gi.KeyKP5, Mods: ctrl}, refused: true},
	} {
		v := p.judge(closePane, c.press)
		if (v.refused != "") != c.refused || v.same != c.same || v.button != c.button {
			t.Errorf("%s came to %+v", c.name, v)
		}
	}
}

// Ctrl+1 is a key the window takes, once a file says what it runs.
func TestADigitRunsACommand(t *testing.T) {
	next, unknown := keymapOf([]keys.Change{{Chord: ui.Chord{Key: input.Key1, Mods: input.ModCtrl}, Command: "tab.next", Written: "ctrl+1"}})
	if len(unknown) > 0 {
		t.Fatal(unknown)
	}
	if id, ok := next.Lookup(ui.Chord{Key: input.Key1, Mods: input.ModCtrl}); !ok || id != "tab.next" {
		t.Fatalf("ctrl+1 runs %q", id)
	}
}

// A shortcut coming from the file grows in on its row, over frames, and
// one going shrinks out; the row says it has changed.
func TestChipsComeAndGoOverFrames(t *testing.T) {
	win, publish, st := helpStage(t)
	p := win.help
	l := lineOf(t, p, "conn.terminal")
	p.list.ScrollToKey(l.key, lastUI)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	n, ok := p.list.Row(l.key)
	if !ok {
		t.Fatal("the row isn't built")
	}
	r := n.(*shortcutRow)
	free := ui.Chord{Key: input.Key1, Mods: input.ModCtrl}
	st.Shortcuts = []keys.Change{{Chord: free, Command: "conn.terminal", Written: "ctrl+1"}}
	st.ShortcutsRead++
	publish(st)
	rc := r.chips[free]
	if rc == nil {
		t.Fatal("no chip for the new shortcut")
	}
	var widths []float32
	u := lastUI
	for range 40 {
		lastWindow.Frame(time.Second / 60)
		if b, ok := u.Bounds(rc.fold); ok {
			widths = append(widths, b.Size().W)
		}
	}
	if len(widths) < 10 || widths[1] >= widths[len(widths)-1] || widths[len(widths)-1] < 20 {
		t.Fatalf("the chip's widths were %v", widths)
	}
	if !p.changed(l) {
		t.Fatal("a command with a new shortcut doesn't say it changed")
	}
	st.Shortcuts = nil
	st.ShortcutsRead++
	publish(st)
	if r.chips[free] != nil {
		t.Fatal("the shortcut stayed")
	}
	lastWindow.Frame(time.Second / 60)
	if b, ok := u.Bounds(rc.fold); !ok || b.Size().W <= 0 {
		t.Fatal("the chip left at once")
	}
}

// Enter on +, a key pressed, and the file holds the change.
func TestAddingByKeyboardWritesTheFile(t *testing.T) {
	win, _, _ := helpStage(t)
	p := win.help
	l := lineOf(t, p, "conn.terminal")
	u := lastUI
	p.list.ScrollToKey(l.key, u)
	for range 60 {
		lastWindow.Frame(time.Second / 60)
	}
	n, _ := p.list.Row(l.key)
	r := n.(*shortcutRow)
	u.Focus(r.add)
	lastWindow.Frame(time.Second / 60)
	if p.under.selected != l.key {
		t.Fatalf("the row isn't lit: %q", p.under.selected)
	}
	lastWindow.Input(gi.KeyPress{Key: gi.KeyEnter})
	lastWindow.Input(gi.KeyRelease{Key: gi.KeyEnter})
	lastWindow.Frame(time.Second / 60)
	lastWindow.Frame(time.Second / 60)
	if r.listening == nil {
		t.Fatal("Enter on + isn't listening")
	}
	if u.Focused() != gunim.Node(r.listening) {
		t.Fatal("the input hasn't the keyboard")
	}
	lastWindow.Input(gi.KeyPress{Key: gi.Key1, Mods: gi.ModControl})
	lastWindow.Frame(time.Second / 60)
	if r.listening != nil {
		t.Fatal("still listening after a key")
	}
	found := false
	for len(lastWindow.Client().Intents()) > 0 {
		env := <-lastWindow.Client().Intents()
		if e, ok := env.Intent.(app.EditShortcut); ok && e.Op.Command == "conn.terminal" && e.Op.Chord == (ui.Chord{Key: input.Key1, Mods: input.ModCtrl}) {
			found = true
		}
	}
	if !found {
		t.Fatal("no change was sent for the file")
	}
}

// A file that can't be used turns editing off, and says why.
func TestAFileNotUsedTurnsEditingOff(t *testing.T) {
	win, publish, st := helpStage(t)
	st.ShortcutsProblem = "keys.json is not valid JSON."
	publish(st)
	if !win.help.banner.Open() || win.help.why.Text != st.ShortcutsProblem || !win.help.blocked {
		t.Fatalf("banner open %v saying %q, reset off %v", win.help.banner.Open(), win.help.why.Text, win.help.reset.Disabled)
	}
	st.ShortcutsProblem = ""
	publish(st)
	if win.help.banner.Open() {
		t.Fatal("the banner stayed")
	}
}

// The search narrows the rows by title, name, or key.
func TestTheSearchFindsByKey(t *testing.T) {
	win, _, _ := helpStage(t)
	p := win.help
	u := lastUI
	p.search.SetText("ctrl+shift+w", u)
	p.filter(u)
	var ids []string
	for _, k := range p.showing {
		if l := p.lines[k]; l.kind == lineCommand {
			ids = append(ids, l.id)
		}
	}
	if !slices.Equal(ids, []string{"pane.close"}) {
		t.Fatalf("the search found %v", ids)
	}
}

// Down goes from the search to the first row, the arrows walk the rows
// without the list jumping, and Up from the first goes back.
func TestTheArrowsWalkTheRows(t *testing.T) {
	win, _, _ := helpStage(t)
	p := win.help
	lastUI.Focus(p.search)
	lastWindow.Frame(time.Second / 60)
	press := func(k gi.Key) {
		lastWindow.Input(gi.KeyPress{Key: k})
		lastWindow.Input(gi.KeyRelease{Key: k})
		for range 3 {
			lastWindow.Frame(time.Second / 60)
		}
	}
	press(gi.KeyDown)
	if p.lines[p.under.selected] == nil || p.lines[p.under.selected].kind != lineLauncher {
		t.Fatalf("Down from the search lit %q", p.under.selected)
	}
	start := p.list.Offset()
	var ys []float32
	press(gi.KeyDown)
	for range 20 {
		lastWindow.Frame(time.Second / 60)
		ys = append(ys, p.under.y.Value())
	}
	if l := p.lines[p.under.selected]; l.id != "conn.terminal" {
		t.Fatalf("Down lit %+v", l)
	}
	if p.list.Offset() != start {
		t.Fatalf("the list moved from %v to %v with the row in view", start, p.list.Offset())
	}
	if ys[0] == ys[len(ys)-1] && ys[0] == 0 {
		t.Fatalf("the light never moved: %v", ys)
	}
	press(gi.KeyUp)
	press(gi.KeyUp)
	if lastUI.Focused() != gunim.Node(p.search) {
		t.Fatal("Up from the first row didn't reach the search")
	}
}

// A shortcut replaced onto another row flies there, over frames, from
// the row it left.
func TestAReplacedShortcutFlies(t *testing.T) {
	win, publish, st := helpStage(t)
	p := win.help
	chord := ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}
	rowOf := func(id string) *shortcutRow {
		n, ok := p.list.Row(lineOf(t, p, id).key)
		if !ok {
			t.Fatalf("%s isn't built", id)
		}
		return n.(*shortcutRow)
	}
	from, to := rowOf("conn.terminal"), rowOf("conn.files")
	start, ok := lastUI.Bounds(from.chips[chord].chip)
	if !ok {
		t.Fatal("the chip isn't drawn")
	}
	to.bind(chord, "conn.terminal", lastUI)
	st.Shortcuts = []keys.Change{{Chord: chord, Command: "conn.files", Written: chord.String()}}
	st.ShortcutsRead++
	publish(st)
	if !p.fly.live {
		t.Fatal("nothing took off")
	}
	own, _ := lastUI.Bounds(p)
	last := start.Min.Y - own.Min.Y
	moved := 0
	for range 90 {
		lastWindow.Frame(time.Second / 60)
		y := p.fly.at.Y
		if d := y - last; d > 30 || d < -30 {
			t.Fatalf("it jumped from %v to %v", last, y)
		}
		if y != last {
			moved++
		}
		last = y
	}
	if moved < 5 {
		t.Fatalf("it moved on %d frames", moved)
	}
	if p.fly.live {
		t.Fatal("it never landed")
	}
	dest, _ := lastUI.Bounds(to.chips[chord].chip)
	if d := last - (dest.Min.Y - own.Min.Y); d > 1 || d < -1 {
		t.Fatalf("it landed at %v, and the chip is at %v", last, dest.Min.Y-own.Min.Y)
	}
}

// The pane opens without the search lit, and typing anywhere is a search.
func TestTypingAnywhereSearches(t *testing.T) {
	win, _, _ := helpStage(t)
	p := win.help
	r := p.rowOf(lineOf(t, p, "conn.terminal").key)
	if r == nil {
		t.Fatal("no row")
	}
	lastUI.Focus(r.add)
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyC, Typed: true})
	lastWindow.Input(gi.TextInput{Text: "c"})
	lastWindow.Frame(time.Second / 60)
	if lastUI.Focused() != gunim.Node(p.search) || p.search.Text() != "c" {
		t.Fatalf("typing left the search holding %q", p.search.Text())
	}
}

// A key moved to a row above where it was leaves that row at once, so one
// chip shows at a time, whichever way it moves.
func TestAKeyMovedUpShowsOnce(t *testing.T) {
	win, publish, st := helpStage(t)
	p := win.help
	chord := ui.Chord{Key: input.KeyW, Mods: input.ModCtrl | input.ModShift}
	rowOf := func(id string) *shortcutRow {
		r := p.rowOf(lineOf(t, p, id).key)
		if r == nil {
			t.Fatalf("%s isn't built", id)
		}
		return r
	}
	from, to := rowOf("pane.close"), rowOf("conn.terminal")
	old := from.chips[chord]
	to.bind(chord, "pane.close", lastUI)
	st.Shortcuts = []keys.Change{{Chord: chord, Command: "conn.terminal", Written: chord.String()}}
	st.ShortcutsRead++
	publish(st)
	if !p.fly.live {
		t.Fatal("nothing took off")
	}
	if v := old.vis.t.Value(); v > 0.01 {
		t.Fatalf("the chip it left is still showing, at %v", v)
	}
	if p.flight != nil {
		t.Fatal("the flight is still waiting")
	}
}

// A change that fails leaves nothing waiting: the next is said as usual.
func TestAFailedMoveLeavesNothingBehind(t *testing.T) {
	win, publish, st := helpStage(t)
	p := win.help
	r := p.rowOf(lineOf(t, p, "conn.files").key)
	if r == nil {
		t.Fatal("no row")
	}
	r.bind(ui.Chord{Key: input.KeyT, Mods: input.ModCtrl | input.ModShift}, "conn.terminal", lastUI)
	free := ui.Chord{Key: input.Key1, Mods: input.ModCtrl}
	r.bind(free, "", lastUI)
	if p.flight != nil {
		t.Fatal("the failed move's flight stayed")
	}
	st.Shortcuts = []keys.Change{{Chord: free, Command: "conn.files", Written: "ctrl+1"}}
	st.ShortcutsRead++
	publish(st)
	if p.said != "" {
		t.Fatalf("still waiting to say %q", p.said)
	}
}

// Narrow, the groups are out of the keyboard's way, and draw nothing.
func TestANarrowPaneHasNoGroups(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(600, 500))
	publish(app.State{Panes: []app.Pane{{ID: "p1", Kind: app.KindHelp, Title: "Shortcuts and Commands"}}, Stage: &app.Box{Pane: "p1"}, Focus: "p1"})
	settle()
	p := win.help
	if !p.side.SkipFocus || p.focusTarget() != gunim.Node(p.search) || p.sideX > 0.5 {
		t.Fatalf("narrow: skip %v, sidebar at %v", p.side.SkipFocus, p.sideX)
	}
	lastUI.Focus(p.side)
	settle()
	if lastUI.Focused() != gunim.Node(p.search) {
		t.Fatalf("the keyboard stayed on the hidden groups: %T has it", lastUI.Focused())
	}
}

// The keyboard reaches a row scrolled far out of the list's reach.
func TestDownFromTheSearchReachesARowScrolledAway(t *testing.T) {
	win, _, _ := helpStage(t)
	p := win.help
	last := p.showing[len(p.showing)-1]
	p.list.ScrollToKey(last, lastUI)
	settle()
	lastUI.Focus(p.search)
	lastWindow.Frame(time.Second / 60)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyDown})
	settle()
	if p.under.selected == "" || p.lines[p.under.selected].kind != lineLauncher {
		t.Fatalf("Down lit %q", p.under.selected)
	}
}
