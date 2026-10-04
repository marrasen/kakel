package view

import (
	"sync"
	"testing"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
)

// checkboxesIn are the checkboxes under n, in order.
func checkboxesIn(n gunim.Node) []*widget.Checkbox {
	var out []*widget.Checkbox
	if c, ok := n.(*widget.Checkbox); ok {
		out = append(out, c)
	}
	if c, ok := n.(gunim.Composite); ok {
		for _, k := range c.Children() {
			out = append(out, checkboxesIn(k)...)
		}
	}
	return out
}

// The Settings dialog shows the sounds, the rings and the title bar as
// they are, and saves what was ticked.
func TestTheSettingsDialogSavesWhatIsTicked(t *testing.T) {
	win, _, publish := windowStage(t)
	st := twoPanes("p1", nil)
	st.Sounds = app.Alerts{Bell: true}
	st.Rings = app.Alerts{Connected: true, Lost: true, Finished: true, Bell: true, Other: true}
	publish(st)
	if !win.run("app.settings", lastUI) || win.dialog == nil {
		t.Fatal("Settings… opened no dialog")
	}
	boxes := checkboxesIn(win.dialog.Body)
	// Row by row: the interface's sound, then each event's sound and
	// rings, then the title bar.
	if len(boxes) != 12 {
		t.Fatalf("the dialog has %d checkboxes, want 12", len(boxes))
	}
	on := make([]bool, len(boxes))
	for i, b := range boxes {
		on[i] = b.On
	}
	want := []bool{false, false, true, false, true, false, true, true, true, false, true, false}
	for i := range want {
		if on[i] != want[i] {
			t.Fatalf("the checkboxes show %v, want %v", on, want)
		}
	}
	boxes[0].On = true  // the interface's sounds
	boxes[3].On = true  // a connection lost sounds
	boxes[8].On = false // no rings for the bell
	boxes[11].On = true // the system's title bar
	in, ok := win.dialog.OnAccept().(app.SaveLook)
	if !ok {
		t.Fatalf("Save sent %#v", win.dialog.OnAccept())
	}
	if in.Sounds != (app.Alerts{Interface: true, Lost: true, Bell: true}) ||
		in.Rings != (app.Alerts{Connected: true, Lost: true, Finished: true, Other: true}) || !in.SystemTitleBar {
		t.Fatalf("Save sent %+v", in)
	}
}

// cueRecorder writes down the cues a window plays.
type cueRecorder struct {
	mu   sync.Mutex
	cues []gunim.Cue
}

func (r *cueRecorder) PlayCue(c gunim.Cue, _ float32) {
	r.mu.Lock()
	r.cues = append(r.cues, c)
	r.mu.Unlock()
}

func (r *cueRecorder) take() []gunim.Cue {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.cues
	r.cues = nil
	return out
}

// Each event plays its sound only where the settings ask for it: a
// connection lost does, one made does not, and a bell in the pane in
// front does, where nothing else would say it rang.
func TestAnEventSoundsAsTheSettingsSay(t *testing.T) {
	_, _, publish := windowStage(t)
	rec := &cueRecorder{}
	lastWindow.SetCues(rec)
	st := twoPanes("p1", nil)
	st.Sounds = app.Alerts{Lost: true, Bell: true}
	publish(st)
	rec.take()

	st.Pings.Lost++
	st.Pings.Connected++
	publish(st)
	if got := rec.take(); len(got) != 1 || got[0] != gunim.CueDisconnected {
		t.Fatalf("a connection made and one lost played %v, want the lost one's alone", got)
	}
	st.Bells++
	publish(st)
	if got := rec.take(); len(got) != 1 || got[0] != gunim.CueBell {
		t.Fatalf("a bell in the pane in front played %v", got)
	}
	st.Sounds = app.Alerts{}
	st.Bells++
	st.Pings.Lost++
	publish(st)
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("with every sound off, %v played", got)
	}
}
