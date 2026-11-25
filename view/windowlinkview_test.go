package view

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/geom"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/internal/sessiontest"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/vt"
)

// linkStage is a window of two terminals, shared as links says.
func linkStage(t *testing.T, links ...app.WindowLink) (win *Window, st app.State, publish func(app.State)) {
	t.Helper()
	win, sh, publish := windowStageOf(t, geom.Sz(900, 600))
	quiet := screen.Hooks{Output: func() {}, Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {}}
	for _, id := range []string{"p1", "p2"} {
		sh.Set(id, screen.Open(sessiontest.New(), vt.DefaultPalette(), quiet))
		t.Cleanup(func() { _ = sh.Get(id).T.Close() })
	}
	st = app.State{Panes: []app.Pane{{ID: "p1", Title: "one"}, {ID: "p2", Title: "two"}}, Focus: "p1", Marks: look.MarksOf(vt.DefaultPalette()),
		Layout: boxLayout(&app.Box{ID: "s", Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}}), Links: links,
		LinkSetup: app.LinkSetup{Name: "Anna", Relay: "https://relay.example.com/k/0123456789abcdef0123456789"}}
	publish(st)
	settle()
	return win, st, publish
}

// asks is a share whose guest asks to come in.
func asks(id, name string) app.WindowLink {
	return app.WindowLink{ID: id, CanType: true, Stands: app.LinkAsking, Guest: name, From: "203.0.113.7", About: "Firefox on Linux"}
}

// isIn is a share whose guest is in a pane.
func isIn(id, name, pane string) app.WindowLink {
	l := asks(id, name)
	l.Stands, l.In, l.Since = app.LinkIn, pane, time.Now()
	return l
}

// sentIntents are the intents the window has sent, taken away.
func sentIntents() []gunim.Intent {
	var out []gunim.Intent
	for len(lastWindow.Client().Intents()) > 0 {
		out = append(out, (<-lastWindow.Client().Intents()).Intent)
	}
	return out
}

// answers are the answers about guests among the intents sent.
func answers() []app.AnswerGuest {
	var out []app.AnswerGuest
	for _, in := range sentIntents() {
		if a, ok := in.(app.AnswerGuest); ok {
			out = append(out, a)
		}
	}
	return out
}

// hit presses a key, and lets a few frames go by.
func hit(k gi.Key) {
	lastWindow.Input(gi.KeyPress{Key: k, Time: time.Now()})
	frames(2)
}

// pause lets d go by with nothing done.
func pause(d time.Duration) {
	for ; d > 0; d -= time.Second / 20 {
		lastWindow.Frame(time.Second / 20)
	}
}

// Keys typed on as the question opens answer nothing, and those after
// that land on Refuse.
func TestAStrayKeyNeverLetsAGuestIn(t *testing.T) {
	for _, k := range []gi.Key{gi.KeyEnter, gi.KeyKPEnter, gi.KeySpace, gi.KeyEscape, gi.KeyY, gi.KeyTab} {
		win, st, publish := linkStage(t)
		st.Links = []app.WindowLink{asks("l1", "Bob")}
		publish(st)
		if win.guestAsk == nil {
			t.Fatal("a guest asks, and nothing is asked")
		}
		hit(k)
		lastWindow.Input(gi.TextInput{Text: "y", Time: time.Now()})
		hit(k)
		if got := answers(); len(got) != 0 || lastUI.Presence(win.guestAsk.d) == gunim.Exiting {
			t.Errorf("%v as the question opened answered %+v", k, got)
		}
		// Typing goes on: it goes on waiting.
		for range 4 {
			pause(askWait / 2)
			hit(gi.KeyA)
		}
		hit(k)
		if got := answers(); len(got) != 0 {
			t.Errorf("%v while typing went on answered %+v", k, got)
		}
		pause(askWait * 2)
		hit(k)
		settle()
		for _, a := range answers() {
			if a.Allow {
				t.Errorf("%v let the guest in", k)
			}
		}
	}
}

// Tab and then Space, typed right after one another, let nobody in,
// whichever way the keyboard goes round the question; Allow does once it
// has had the keyboard a moment, with nothing pressed.
func TestTheKeyboardLetsAGuestInOnlyOnceOnAllow(t *testing.T) {
	for _, first := range []bool{false, true} {
		for _, shift := range []gi.Mods{0, gi.ModShift} {
			for _, k := range []gi.Key{gi.KeySpace, gi.KeyEnter} {
				win, st, publish := linkStage(t)
				st.Links = []app.WindowLink{asks("l1", "Bob")}
				publish(st)
				d := win.guestAsk.d
				d.DefaultFirst = first
				pause(askWait * 2)
				tabs := 0
				for lastUI.Focused() != d.OK() {
					if tabs++; tabs > 3 {
						t.Fatal("Tab never comes to Allow")
					}
					lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Mods: shift, Time: time.Now()})
					frames(2)
				}
				for range 3 {
					hit(k)
					pause(askWait * 8 / 10)
				}
				if got := answers(); len(got) != 0 || lastUI.Presence(d) == gunim.Exiting {
					t.Errorf("OK first %v, Shift %v: %v after %d tabs answered %+v", first, shift != 0, k, tabs, got)
				}
				pause(askWait * 12 / 10)
				hit(k)
				if got := answers(); len(got) != 1 || !got[0].Allow {
					t.Errorf("OK first %v, Shift %v: %v on Allow, a moment later, answered %+v", first, shift != 0, k, got)
				}
			}
		}
	}
}

// clickOn clicks the middle of a node, in the window.
func clickOn(t *testing.T, n gunim.Node) {
	t.Helper()
	box, ok := lastUI.Bounds(n)
	if !ok {
		t.Fatalf("%T is not on screen", n)
	}
	lastWindow.Input(gi.PointerMove{Pos: box.Center(), Time: time.Now()})
	lastWindow.Input(gi.PointerDown{Pos: box.Center(), Button: gi.ButtonPrimary, Clicks: 1, Time: time.Now()})
	lastWindow.Input(gi.PointerUp{Pos: box.Center(), Button: gi.ButtonPrimary, Time: time.Now()})
	frames(2)
}

// A click on the question before it works does nothing, and has it wait
// anew, wherever it lands; after that one on Allow allows.
func TestClicksOnTheQuestion(t *testing.T) {
	win, st, publish := linkStage(t)
	st.Links = []app.WindowLink{asks("l1", "Bob")}
	publish(st)
	settle() // Not a second yet, as the frames go by.
	g := win.guestAsk
	pause(askWait)
	// It works now: begin anew, as a window that gets the keyboard back.
	lastWindow.Input(gi.WindowFocusLost{Time: time.Now()})
	frames(2)
	if g.ready {
		t.Fatal("in a window that lost the keyboard the question works")
	}
	pause(3 * askWait)
	if g.ready {
		t.Fatal("it waited while the window was without the keyboard")
	}
	lastWindow.Input(gi.WindowFocusGained{Time: time.Now()})
	frames(2)
	was := g.may.Selected()
	for _, n := range []gunim.Node{g.d.OK(), g.may, g.d.Cancel(), g.form} {
		pause(askWait * 8 / 10)
		clickOn(t, n)
		if got := answers(); len(got) != 0 || g.ready || g.may.Selected() != was {
			t.Fatalf("a click on %T answered %+v, chose %d", n, got, g.may.Selected())
		}
	}
	// Less than the wait after the last click, as one double click is.
	pause(askWait * 8 / 10)
	clickOn(t, g.d.OK())
	if got := answers(); len(got) != 0 {
		t.Fatalf("a click soon after another answered %+v", got)
	}
	pause(askWait * 12 / 10)
	clickOn(t, g.d.OK())
	if got := answers(); len(got) != 1 || !got[0].Allow {
		t.Errorf("a click on Allow answered %+v", got)
	}
}

// Enter in the question answers no, wherever in it the keyboard is.
func TestEnterInTheQuestionRefuses(t *testing.T) {
	for _, tabs := range []int{0, 1, 2} {
		win, st, publish := linkStage(t)
		st.Links = []app.WindowLink{asks("l1", "Bob")}
		publish(st)
		pause(askWait * 2)
		if got := win.guestAsk.d.Cancel(); lastUI.Focused() != got {
			t.Fatalf("the keyboard is on %T", lastUI.Focused())
		}
		for range tabs {
			lastWindow.Input(gi.KeyPress{Key: gi.KeyTab, Time: time.Now()})
			frames(2)
		}
		onAllow := lastUI.Focused() == win.guestAsk.d.OK()
		pause(askWait * 12 / 10)
		hit(gi.KeyEnter)
		for _, a := range answers() {
			if a.Allow != onAllow {
				t.Errorf("after %d tabs, Enter answered %+v", tabs, a)
			}
		}
	}
}

// Allow lets the guest do what the question says it can, as the user
// left it, and is asked once.
func TestAllowingAGuest(t *testing.T) {
	win, st, publish := linkStage(t)
	st.Links = []app.WindowLink{asks("l1", "Bob")}
	publish(st)
	pause(askWait * 2)
	win.guestAsk.may.SetSelected(mayIndex(false), lastUI)
	sentIntents()
	allow := win.guestAsk.d.OK()
	lastUI.Focus(allow)
	hit(gi.KeyEnter)
	if got := answers(); len(got) != 1 || got[0] != (app.AnswerGuest{ID: "l1", Allow: true}) {
		t.Fatalf("it answered %+v", got)
	}
	// A state from before the answer was taken asks nothing again.
	publish(st)
	settle()
	if win.guestAsk != nil && lastUI.Presence(win.guestAsk.d) != gunim.Exiting {
		t.Error("the guest is asked about again")
	}
}

// Two guests are asked about one after the other, and a question whose
// guest has gone goes.
func TestGuestsAreAskedAboutInTurn(t *testing.T) {
	win, st, publish := linkStage(t)
	st.Links = []app.WindowLink{asks("l1", "Bob"), asks("l2", "Maria")}
	publish(st)
	settle()
	if win.guestAsk == nil || win.guestAsk.id != "l1" {
		t.Fatalf("it asks %+v", win.guestAsk)
	}
	st.Links = st.Links[1:]
	publish(st)
	settle()
	publish(st)
	if win.guestAsk == nil || win.guestAsk.id != "l2" {
		t.Fatalf("with the first gone it asks %+v", win.guestAsk)
	}
	st.Links = nil
	publish(st)
	settle()
	if win.guestAsk != nil || len(answers()) != 0 {
		t.Error("with both gone it still asks")
	}
}

// The question waits for another dialog, and the one the link was made
// in gives way to it. One about another window's guest is not asked here.
func TestWhereAGuestIsAskedAbout(t *testing.T) {
	win, st, publish := linkStage(t)
	win.rename(lastUI)
	settle()
	st.Links = []app.WindowLink{asks("l1", "Bob")}
	publish(st)
	if win.guestAsk != nil {
		t.Fatal("it asks over another dialog")
	}
	win.dialog.Close(lastUI)
	settle()
	// With nothing said to the program, or by it.
	if win.guestAsk == nil {
		t.Fatal("the dialog closed, and nothing is asked")
	}
	win.guestAsk.d.Close(lastUI)
	win.guestAsk = nil
	settle()

	st.Links = nil
	publish(st)
	win.linkDialog(lastUI)
	settle()
	st.Links = []app.WindowLink{asks("l3", "Bob")}
	publish(st)
	if win.guestAsk == nil || win.dialog != win.guestAsk.d {
		t.Fatal("the dialog the link was made in kept the question back")
	}

	win, st, publish = linkStage(t)
	other := asks("l1", "Bob")
	other.Window = 7
	st.Links = []app.WindowLink{other}
	publish(st)
	if win.guestAsk != nil || len(win.chips.shown()) != 0 {
		t.Error("another window's guest is asked about here")
	}
}

// A guest who asks while another program has the keyboard is told of
// outside the window, once.
func TestAGuestWhoAsksUnseenIsSaidOutside(t *testing.T) {
	win, st, publish := linkStage(t)
	lastWindow.Input(gi.WindowFocusLost{Time: time.Now()})
	frames(2)
	st.Links = []app.WindowLink{asks("l1", "Bob")}
	publish(st)
	publish(st)
	n := 0
	for _, in := range sentIntents() {
		if in == (app.GuestUnseen{ID: "l1"}) {
			n++
		}
	}
	if n != 1 || win.guestAsk == nil {
		t.Errorf("it was said %d times", n)
	}
}

// The chip says what there is to know at a glance: who asks, or else who
// is in, or that the window is shared. What a guest calls itself reads as
// no words of kakel's.
func TestWhatTheLinkChipSays(t *testing.T) {
	link := app.WindowLink{ID: "a", Stands: app.LinkWaiting}
	over := app.WindowLink{ID: "b", Stands: app.LinkEnded}
	away := isIn("c", "Maria", "")
	away.Stands = app.LinkAway
	for _, c := range []struct {
		links []app.WindowLink
		want  []string
	}{
		{nil, nil},
		{[]app.WindowLink{over}, nil},
		{[]app.WindowLink{link, over}, []string{"Shared"}},
		{[]app.WindowLink{link, link}, []string{"Shared", "2"}},
		{[]app.WindowLink{link, isIn("d", "Bob", "p1")}, []string{"Bob"}},
		{[]app.WindowLink{away}, []string{"Maria"}},
		{[]app.WindowLink{away, isIn("d", "Bob", "p1")}, []string{"2"}},
		{[]app.WindowLink{isIn("d", "Bob", "p1"), asks("e", "Eve")}, []string{"Eve", "asks"}},
		{[]app.WindowLink{asks("e", "Eve"), asks("f", "Bob")}, []string{"2", "ask"}},
		{[]app.WindowLink{isIn("d", "Shared", "p1")}, []string{"Guest: Shared"}},
		{[]app.WindowLink{isIn("d", "2 guests", "p1")}, []string{"Guest: 2 guests"}},
		{[]app.WindowLink{asks("d", "Nobody")}, []string{"Guest: Nobody asks"}},
	} {
		win, _, _ := linkStage(t, c.links...)
		var got string
		for _, chip := range win.chips.shown() {
			got += chip.text
		}
		if (got == "") != (c.want == nil) {
			t.Errorf("of %+v the chip says %q", c.links, got)
		}
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("of %+v the chip says %q, want %q in it", c.links, got, want)
			}
		}
	}
}

// A chip grows in, and one that says something else comes as the one
// before goes, so nothing on the bar jumps.
func TestBarChipsComeAndGoOverFrames(t *testing.T) {
	win, st, publish := linkStage(t)
	var widths []float32
	measure := func(n int) {
		for range n {
			lastWindow.Frame(time.Second / 60)
			widths = append(widths, win.chips.wide)
		}
	}
	steps := func(what string) {
		t.Helper()
		for i := 1; i < len(widths); i++ {
			if d := widths[i] - widths[i-1]; d > 40 || d < -40 {
				t.Errorf("%s, the chips went from %v to %v wide in a frame", what, widths[i-1], widths[i])
			}
		}
		widths = nil
	}
	st.Links = []app.WindowLink{{ID: "a", Stands: app.LinkWaiting}}
	if err := lastWindow.Client().Publish(app.WindowTopic, st); err != nil {
		t.Fatal(err)
	}
	measure(60)
	if last := widths[len(widths)-1]; last < 40 {
		t.Fatalf("the chip is %v wide", last)
	}
	steps("coming")
	st.Links = []app.WindowLink{isIn("a", "Bartholomew", "p1")}
	if err := lastWindow.Client().Publish(app.WindowTopic, st); err != nil {
		t.Fatal(err)
	}
	measure(60)
	steps("saying something else")
	st.Links = nil
	if err := lastWindow.Client().Publish(app.WindowTopic, st); err != nil {
		t.Fatal(err)
	}
	measure(60)
	if last := widths[len(widths)-1]; last != 0 {
		t.Errorf("gone, the chips are %v wide", last)
	}
	steps("going")
	publish(st)
}

// On a bar with little room the chip that says a guest asks stays, cut,
// where the rest make way.
func TestAGuestWhoAsksIsSaidOnANarrowBar(t *testing.T) {
	win, _, publish := windowStageOf(t, geom.Sz(480, 400))
	st := splitTabs()
	st.Layout = boxLayout(&app.Box{ID: "s1", Share: 0.5, A: &app.Box{Pane: "p1"}, B: &app.Box{Pane: "p2"}})
	st.Serving = app.Serving{On: true}
	st.Links = []app.WindowLink{asks("l1", "Bartholomew of the Long Name")}
	st.Links[0].Window = st.Window
	publish(st)
	win.guestAsk.d.Close(lastUI)
	win.answered["l1"] = true
	settle()
	if !win.chips.limited {
		t.Fatal("the bar has room enough")
	}
	shown := map[string]float32{}
	for _, s := range win.chips.order {
		shown[s.text] = s.on.Value()
		if s.must && (s.box.Size().W < 40 || s.box.Max.X > win.chips.room+2*chipLeast) {
			t.Errorf("the chip is at %v, of %v", s.box, win.chips.room)
		}
	}
	if win.chips.on.Value() < 0.99 || shown["Guest: Bartholomew of the Long Name asks"] < 0.99 || shown["Remote Access"] > 0.01 {
		t.Errorf("the chips show %v, the bar %v", shown, win.chips.on.Value())
	}
}

// However fast the chips change, the one that says something now is
// there.
func TestAChipThatComesIsNeverLeftOut(t *testing.T) {
	b := newChipBar()
	for i := range 3 * mostChips {
		text := strings.Repeat("x", i+1)
		b.show([]chip{{text: text}, {text: "stays"}})
		var got []string
		for _, c := range b.shown() {
			got = append(got, c.text)
		}
		if !slices.Equal(got, []string{text, "stays"}) {
			t.Fatalf("after %d changes the bar has %q", i, got)
		}
	}
}

// guestRings counts the rings drawn round panes in the guest's colour,
// and how strong the strongest is.
func guestRings() (n int, most uint8) {
	c := look.Guest.Get(lastUI.Theme())
	for _, op := range lastWindow.Offscreen().Ops() {
		if r, ok := op.(*paint.RRectOp); ok && r.Stroke.Width == markWidth && r.Stroke.Color.R == c.R && r.Stroke.Color.G == c.G && r.Stroke.Color.B == c.B {
			n++
			most = max(most, r.Stroke.Color.A)
		}
	}
	return n, most
}

// Each pane a guest sees has a ring, which fades in and out, and the one
// the guest is in has a brighter one, and the guest's name on its tab.
func TestAPaneSaysWhichGuestIsInIt(t *testing.T) {
	win, st, publish := linkStage(t, app.WindowLink{ID: "l1", Stands: app.LinkWaiting})
	if n, _ := guestRings(); n != 0 {
		t.Fatal("a link nobody opened has the panes ringed")
	}
	st.Links = []app.WindowLink{isIn("l1", "Bob", "p1")}
	publish(st)
	_, early := guestRings()
	settle()
	n, late := guestRings()
	if n != 2 || early >= late {
		t.Errorf("there are %d rings, as strong as %d at first and %d after", n, early, late)
	}
	named := func(id string) string { return win.tabFace(id).Badge }
	// drawn is how wide pane id's tab is: its badge has room in it.
	drawn := func(id string) float32 {
		r, _ := win.dock.TabRect(id)
		return r.Size().W
	}
	if named("p1") != "Bob" || named("p2") != "" {
		t.Errorf("p1's tab has %q, and p2's %q", named("p1"), named("p2"))
	}
	with, without := drawn("p1"), drawn("p2")
	// Elsewhere, the name goes along.
	st.Links = []app.WindowLink{isIn("l1", "Bob", "p2")}
	publish(st)
	settle()
	if named("p1") != "" || named("p2") != "Bob" {
		t.Error("the name stayed where the guest was")
	}
	if drawn("p1") >= with || drawn("p2") <= without {
		t.Errorf("the tabs went from %v and %v to %v and %v", with, without, drawn("p1"), drawn("p2"))
	}
	// Two are both named, and one who left is not.
	st.Links = []app.WindowLink{isIn("l1", "Bob", "p2"), isIn("l2", "", "p2")}
	publish(st)
	if got := named("p2"); !strings.Contains(got, "Bob") || got == "Bob" {
		t.Errorf("with two in it, p2's tab has %q", got)
	}
	st.Links[0].Stands = app.LinkAway
	publish(st)
	if got := named("p2"); strings.Contains(got, "Bob") {
		t.Errorf("one who is away is named: %q", got)
	}
	st.Links = nil
	publish(st)
	settle()
	if n, _ := guestRings(); n != 0 || win.glowing || named("p2") != "" {
		t.Error("with nobody there the panes are ringed, or named")
	}
}

// Making a link asks for it with what is typed, and turns the dialog to
// the links; a relay that is none is said, and asks nothing.
func TestMakingALink(t *testing.T) {
	win, st, publish := linkStage(t)
	win.run("share.link", lastUI)
	settle()
	b := win.linkBox
	if b == nil || b.listing {
		t.Fatal("the dialog does not offer to make a link")
	}
	b.relay.SetText("nonsense", lastUI)
	sentIntents()
	hit(gi.KeyEnter)
	settle()
	if got := sentIntents(); slices.ContainsFunc(got, func(in gunim.Intent) bool { _, ok := in.(app.ShareWindow); return ok }) || b.wrong.Text == "" || !b.wrongFold.Open() || b.listing {
		t.Fatalf("with no relay it sent %+v", got)
	}
	b.relay.SetText(" "+st.LinkSetup.Relay+" ", lastUI)
	b.may.SetSelected(mayIndex(false), lastUI)
	hit(gi.KeyEnter)
	want := app.ShareWindow{Window: win.winID, Name: "Anna", Relay: st.LinkSetup.Relay}
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(want)) {
		t.Fatalf("it sent %+v", got)
	}
	if !b.listing || lastUI.Presence(b.d) == gunim.Exiting {
		t.Fatal("the dialog did not turn to the links")
	}
	// A state from before it was tried leaves it there; tried and come to
	// nothing, it turns back.
	publish(st)
	settle()
	if !b.listing {
		t.Error("it turned back before the link was tried")
	}
	st.LinkSetup.Tries++
	publish(st)
	settle()
	if b.listing {
		t.Error("with no link made it lists none")
	}
	// The other way: a link comes, and goes on to a guest.
	hit(gi.KeyEnter)
	st.LinkSetup.Tries++
	st.Links = []app.WindowLink{{ID: "l1", Stands: app.LinkWaiting, Link: "https://relay.example.com/s/x/#y", Until: time.Now().Add(time.Minute)}}
	publish(st)
	settle()
	if !b.listing || len(b.list.Keys()) != 1 {
		t.Fatalf("it lists %v", b.list.Keys())
	}
	// Enter is not Create's here: it closes.
	sentIntents()
	hit(gi.KeyEnter)
	if got := sentIntents(); lastUI.Presence(b.d) != gunim.Exiting || slices.ContainsFunc(got, func(in gunim.Intent) bool { _, ok := in.(app.ShareWindow); return ok }) {
		t.Errorf("Enter on the links sent %+v", got)
	}
}

// Opened while there are links, the dialog lists them; the one chip opens
// it too, and what can be done with a link is the link's own.
func TestTheLinksAreListed(t *testing.T) {
	waits := app.WindowLink{ID: "l1", Stands: app.LinkWaiting, Link: "https://relay.example.com/s/x/#y", Until: time.Now().Add(time.Minute)}
	win, st, publish := linkStage(t, waits, isIn("l2", "Bob", "p1"))
	win.chips.order[0].do(lastUI)
	settle()
	b := win.linkBox
	if b == nil || !b.listing {
		t.Fatal("the chip does not list the links")
	}
	card := func(key string) *linkCard {
		n, ok := b.list.Row(widget.Key(key))
		if !ok {
			t.Fatalf("no %s in %v", key, b.list.Keys())
		}
		return n.(*linkCard)
	}
	sentIntents()
	for _, btn := range []*widget.Button{card("l1:link").copyIt, card("l2:guest").stop} {
		lastUI.Focus(btn)
		hit(gi.KeyEnter)
	}
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.CopyLink{ID: "l1"})) || !slices.Contains(got, gunim.Intent(app.StopLink{ID: "l2"})) {
		t.Errorf("the buttons sent %+v", got)
	}
	// Its guest in, a link is not there to copy.
	st.Links = []app.WindowLink{isIn("l1", "Eve", "p1"), st.Links[1]}
	publish(st)
	settle()
	if c := card("l1:guest"); c.linkRow.Open() || !slices.Contains(b.Focusables(), gunim.Node(c.stop)) {
		t.Error("a link that is used can be copied")
	}
	lastUI.Focus(card("l1:guest").may)
	lastWindow.Input(gi.KeyPress{Key: gi.KeyRight, Time: time.Now()})
	frames(2)
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.SetLinkMay{ID: "l1"})) {
		t.Errorf("only watching sent %+v", got)
	}
}

// The commands of sharing with a link: Stop Sharing Window stops this
// window's, and is there to choose while there is one.
func TestStopSharingWindow(t *testing.T) {
	win, st, publish := linkStage(t)
	if win.applies("share.stop") {
		t.Error("a window not shared can be stopped being")
	}
	sentIntents()
	win.run("share.stop", lastUI)
	if got := sentIntents(); len(got) != 0 {
		t.Errorf("it sent %+v", got)
	}
	st.Links = []app.WindowLink{{ID: "l1", Stands: app.LinkWaiting}}
	publish(st)
	if !win.applies("share.stop") {
		t.Error("a window shared cannot be stopped being")
	}
	win.run("share.stop", lastUI)
	if got := sentIntents(); !slices.Contains(got, gunim.Intent(app.StopLinks{Window: win.winID})) {
		t.Errorf("it sent %+v", got)
	}
}

// With no web client in the build, the dialog says so and asks nothing.
func TestABuildWithNoWebClientSaysSo(t *testing.T) {
	win, st, publish := linkStage(t)
	st.LinkSetup.NoClient = true
	publish(st)
	win.linkDialog(lastUI)
	settle()
	sentIntents()
	if win.linkBox != nil || len(win.dialog.Buttons()) != 1 {
		t.Fatal("it offers to make a link")
	}
	hit(gi.KeyEnter)
	for _, in := range sentIntents() {
		if _, ok := in.(app.ShareWindow); ok {
			t.Error("it asked for a link")
		}
	}
}
