package view

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/app"
)

// TestShotsOldSkillAsk writes frames of the question that offers to remove an earlier kakel's skill, as it opens.
func TestShotsOldSkillAsk(t *testing.T) {
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		t.Skip("KAKEL_SHOTS names no folder to write the screenshots to")
	}
	shot := func(name string) {
		gunimtest.WritePNG(t, filepath.Join(dir, name+".png"), gunimtest.Pixels(t, lastWindow))
	}
	win, _, publish := windowStageOf(t, geom.Sz(1100, 700))
	st := withDefaults(app.State{})
	publish(st)
	settle()
	st.Asks = []app.Ask{{ID: 1, Title: "Remove kakel's old skill?",
		Text: "kakel no longer writes a skill for agents, because its MCP server tells them how to work in panes." +
			" This one is left over from an earlier kakel and will not be updated:\n\n" +
			`C:\Users\me\.claude\skills\kakel\SKILL.md`,
		Yes: "Remove", No: "Keep"}}
	publish(st)
	for i := 1; i <= 4; i++ {
		frames(2)
		shot("oldskill-" + strconv.Itoa(i))
	}
	settle()
	shot("oldskill-open")
	if win.ask == nil {
		t.Fatal("the question did not open")
	}
}
