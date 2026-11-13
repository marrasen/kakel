package app

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/input"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/ui/term"
)

// markingShell is a shell Shell Setup teaches to mark its prompts: bash,
// with none of the user's startup files, or on Windows the Command
// Prompt, which says its folder at each prompt.
func markingShell(t *testing.T) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		return []string{"cmd.exe"}
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash here")
	}
	return []string{bash, "--norc", "--noprofile"}
}

// bashWith is bash reading rc as its startup file, which Shell Setup
// teaches as it teaches any bash. A test of one skips where there is no
// bash, and on Windows, where bash is not the shell a user starts.
func bashWith(t *testing.T, rc string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bash is not the Windows shell")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash here")
	}
	file := filepath.Join(t.TempDir(), "rc")
	if err := os.WriteFile(file, []byte(rc+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{bash, "--rcfile", file, "--noprofile"}
}

// holds runs what the app's goroutines send it for d, as its loop
// would, and fails if ok stops holding in that time.
func holds(t *testing.T, a *app, what string, d time.Duration, ok func() bool) {
	t.Helper()
	end := time.After(d)
	for {
		if !ok() {
			t.Fatalf("%s stopped holding", what)
		}
		select {
		case f := <-a.events:
			f()
		case <-time.After(10 * time.Millisecond):
		case <-end:
			return
		}
	}
}

// linesOf waits for pane id's terminal to show each of lines on a line
// of its own, as a command prints it, and reports the row each is on.
func linesOf(t *testing.T, a *app, id string, lines ...string) []int {
	t.Helper()
	var at []int
	waitFor(t, a, "the pane to show "+strings.Join(lines, ", "), func() bool {
		tm := a.terminal(id)
		if tm == nil {
			return false
		}
		rows := strings.Split(tm.AllText(), "\n")
		at = at[:0]
		for _, line := range lines {
			i := slices.IndexFunc(rows, func(r string) bool { return strings.TrimSpace(r) == line })
			if i < 0 {
				return false
			}
			at = append(at, i)
		}
		return true
	})
	return at
}

// onlyPane is the one pane window w holds once it has started.
func onlyPane(t *testing.T, a *app, w *ownWin) Pane {
	t.Helper()
	waitFor(t, a, "the pane to start", func() bool { return len(waitingIn(a, w)) == 0 })
	ps := a.panesIn(w)
	if len(ps) != 1 {
		t.Fatalf("the window holds %+v", ps)
	}
	return ps[0]
}

// A terminal's start lines are typed into its shell as the layout opens
// it, at its prompts, one after another, each run by the shell. Saved
// again, the layout keeps them.
func TestStartLinesAreTypedAtTheShellsPrompts(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = true
	kept := keep(t, a, alone("Marked", settings.SavedPane{Kind: settings.LayoutTerminal, Command: markingShell(t),
		Start: []string{"echo ONE", "echo TWO"}}))

	p := onlyPane(t, a, opens(t, a, opened, kept.ID))
	at := linesOf(t, a, p.ID, "ONE", "TWO")
	if at[0] >= at[1] {
		t.Fatalf("TWO ran on row %d, before ONE on row %d", at[1], at[0])
	}
	if n := a.terminal(p.ID).LinesLeft(); n != 0 {
		t.Fatalf("%d lines left to type", n)
	}

	again, _, _, err := a.saveLayout(SaveLayout{Window: a.ownerOf(p.ID).id, Name: "Marked", Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Panes["p1"].Start; !slices.Equal(got, []string{"echo ONE", "echo TWO"}) {
		t.Fatalf("saved over, the pane keeps the lines %q", got)
	}
}

// With Shell Setup off, the shell says nothing of its prompt, and each
// line is typed once it has been quiet a moment.
func TestStartLinesWithoutShellSetupAreTypedOnceQuiet(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	kept := keep(t, a, alone("Quiet", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell(),
		Start: []string{"echo ONE", "echo TWO"}}))

	p := onlyPane(t, a, opens(t, a, opened, kept.ID))
	if at := linesOf(t, a, p.ID, "ONE", "TWO"); at[0] >= at[1] {
		t.Fatalf("TWO ran on row %d, before ONE on row %d", at[1], at[0])
	}
}

// A server that could not be reached as the layout opened gets its
// lines once Retry opens its shell.
func TestRetryTypesTheStartLines(t *testing.T) {
	a, opened := openingApp(t)
	refused(t, a, "far")
	kept := keep(t, a, alone("Far", settings.SavedPane{Kind: settings.LayoutTerminal, Host: "far", HostID: "far", Profile: LoginShell,
		Start: []string{"first-line", "second-line"}}))

	w := opens(t, a, opened, kept.ID)
	waitFor(t, a, "far to fail", func() bool {
		left := waitingIn(a, w)
		return len(left) == 1 && a.st.Waits[left[0].ID].Failed != ""
	})
	srv := servers(t, a, "up")
	host, port := srv["up"].Host()
	if err := a.book.Put(remote.Host{Name: "far", Address: host, Port: port, User: "tester"}, "far"); err != nil {
		t.Fatal(err)
	}
	a.handle(RetryWaiting{Pane: waitingIn(a, w)[0].ID})
	asked := 0
	waitFor(t, a, "far's shell", func() bool { answering(a, &asked); return len(waitingIn(a, w)) == 0 })

	// The test server echoes what it is sent, a return and all, so the
	// second line is written over the first.
	p := a.panesIn(w)[0]
	if !textIn(t, a, p.ID, "first-line") || !textIn(t, a, p.ID, "second-line") {
		t.Fatal("the lines were not typed")
	}
}

// A command pane is typed nothing into, and keeps no lines; a shell
// started again in its pane, once the one the layout opened has ended,
// is typed nothing into either, but the pane keeps its lines to save.
func TestNoStartLinesForACommandOrAShellStartedAgain(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	// As written by hand, the command's lines and all.
	l := alone("Hand",
		settings.SavedPane{Kind: settings.LayoutCommand, Line: waitCommand(60), Start: []string{"echo NO"}},
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell(), Start: []string{"exit", "echo AFTER"}},
	)
	l.ID = "hand"
	before := opened.Load()
	if err := a.openLayoutOf(l); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the layout's window", func() bool { return opened.Load() == before+1 && len(a.panesIn(layoutWindow(a))) == 2 })
	w := layoutWindow(a)
	waitFor(t, a, "the panes to start", func() bool { return len(waitingIn(a, w)) == 0 })
	cmd, term := a.panesIn(w)[0], a.panesIn(w)[1]

	if n := a.terminal(cmd.ID).LinesLeft(); n != 0 || a.startLines[cmd.ID] != nil {
		t.Fatalf("the command has %d lines to type, and keeps %q", n, a.startLines[cmd.ID])
	}

	// exit ends the shell, and with it the line after.
	waitFor(t, a, "the shell to exit", func() bool { return a.terminal(term.ID).Exited() })
	if n := a.terminal(term.ID).LinesLeft(); n != 0 {
		t.Fatalf("%d lines left once the shell ended", n)
	}
	if err := a.startAgain(term.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, a, "the shell to start again", func() bool { return !a.terminal(term.ID).Exited() })
	if n := a.terminal(term.ID).LinesLeft(); n != 0 {
		t.Fatalf("%d lines to type into the shell started again", n)
	}
	if !slices.Equal(a.startLines[term.ID], []string{"exit", "echo AFTER"}) {
		t.Fatalf("the pane keeps %q", a.startLines[term.ID])
	}
}

// A terminal through another kakel window is typed nothing into, as it
// opens anew or is watched again, and keeps its lines to save.
func TestStartLinesThroughAWindowAreKept(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	start := []string{"tmux attach"}
	id := keep(t, b, alone("Desk", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal, Start: start}))).ID

	check := func(round string, w *ownWin) {
		t.Helper()
		p := b.panesIn(w)[0]
		if n := b.terminal(p.ID).LinesLeft(); n != 0 {
			t.Fatalf("%s: %d lines to type here", round, n)
		}
		for _, far := range farIDs(b, w) {
			if n := a.terminal(far).LinesLeft(); n != 0 {
				t.Fatalf("%s: %d lines to type there", round, n)
			}
		}
		again, _, _, err := b.saveLayout(SaveLayout{Window: w.id, Name: "Desk", Replace: true})
		if err != nil {
			t.Fatal(err)
		}
		if got := again.Panes["p1"].Start; !slices.Equal(got, start) {
			t.Fatalf("%s: saved over, the pane keeps the lines %q", round, got)
		}
	}
	w := openLanded(t, a, b, opened, id)
	check("opened", w)
	first := farIDs(b, w)

	closeLanded(a, b, w)
	w = openLanded(t, a, b, opened, id)
	if got := farIDs(b, w); !slices.Equal(got, first) {
		t.Fatalf("watches %v, want %v again", got, first)
	}
	check("watched again", w)
}

// A bash with a PROMPT_COMMAND of its own, as starship, direnv and
// history -a give it, gets its start lines too, and stands at its prompt
// as one running nothing.
func TestStartLinesWithAPromptCommandOfTheShellsOwn(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = true
	kept := keep(t, a, alone("Own", settings.SavedPane{Kind: settings.LayoutTerminal,
		Command: bashWith(t, "PROMPT_COMMAND='history -a'"), Start: []string{"echo ONE", "echo TWO"}}))

	p := onlyPane(t, a, opens(t, a, opened, kept.ID))
	if at := linesOf(t, a, p.ID, "ONE", "TWO"); at[0] >= at[1] {
		t.Fatalf("TWO ran on row %d, before ONE on row %d", at[1], at[0])
	}
	tm := a.terminal(p.ID)
	waitFor(t, a, "the shell to stand at its prompt", func() bool { return tm.LinesLeft() == 0 && !tm.RunningAProgram() })
}

// A line still running holds back the next, however long the shell's
// prompt takes to work out, longer than the net under the lines waits
// too, and when the prompt marks itself as well: the first line is typed
// at the prompt after Shell Setup's, and the second is not typed into
// cat. The net's waits are cut short here, so it would act while this
// watches.
func TestARunningStartLineHoldsBackTheNext(t *testing.T) {
	defer term.SetStartLineNets(250*time.Millisecond, 250*time.Millisecond)()
	for name, rc := range map[string]string{
		"a prompt at once":                    "",
		"a slow prompt":                       `PS1='$(sleep 0.4)\$ '`,
		"a prompt slower than the net":        `PS1='$(sleep 0.6)\$ '`,
		"a prompt that marks itself":          `PS1='\[\e]133;A\a\]$(sleep 0.4)\$ '`,
		"a marked prompt slower than the net": `PS1='\[\e]133;A\a\]$(sleep 0.6)\$ '`,
	} {
		t.Run(name, func(t *testing.T) {
			a, opened := openingApp(t)
			a.st.ShellSetup = true
			kept := keep(t, a, alone("Cat", settings.SavedPane{Kind: settings.LayoutTerminal,
				Command: bashWith(t, rc), Start: []string{"cat", "echo TWO"}}))

			p := onlyPane(t, a, opens(t, a, opened, kept.ID))
			tm := a.terminal(p.ID)
			waitFor(t, a, "cat to run", func() bool { return tm.RunningAProgram() && strings.Contains(tm.AllText(), "cat") })
			holds(t, a, "one line left while cat runs", 1500*time.Millisecond, func() bool {
				return tm.LinesLeft() == 1 && !strings.Contains(tm.AllText(), "TWO")
			})
		})
	}
}

// A start line that asks for a password, in a shell that marks its
// prompts, is answered by the user, and the line after it is typed once
// the shell is back.
func TestAStartLineAskingForAPasswordIsAnswered(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = true
	kept := keep(t, a, alone("Ask", settings.SavedPane{Kind: settings.LayoutTerminal,
		Command: bashWith(t, ""), Start: []string{"read -rsp 'Password: ' x", "echo GOT-$x"}}))

	p := onlyPane(t, a, opens(t, a, opened, kept.ID))
	tm := a.terminal(p.ID)
	waitFor(t, a, "the question", func() bool { return strings.HasSuffix(strings.TrimSpace(tm.AllText()), "Password:") })
	for _, r := range "pw" {
		_, _ = tm.HandleKey(input.Event{Kind: input.Text, Rune: r, NormalText: true})
	}
	_, _ = tm.HandleKey(input.Event{Kind: input.KeyPress, Key: input.KeyEnter})
	linesOf(t, a, p.ID, "GOT-pw")
}

// Lines given in Start Lines… to a layout open in a window are the
// lines its terminal keeps: saved over the layout from that window, they
// stand, and so do lines given after a window was saved as a layout.
func TestLinesGivenInTheDialogAreKeptBySavingOver(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	kept := keep(t, a, alone("Shop",
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()},
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell(), Start: []string{"echo KEEP"}}))
	w := opens(t, a, opened, kept.ID)
	waitFor(t, a, "the panes to start", func() bool { return len(waitingIn(a, w)) == 0 })

	saveOver := func(want map[string][]string) {
		t.Helper()
		// As the dialog sends it, with the layout as it stood when opened.
		again, _, _, err := a.saveLayout(SaveLayout{Window: w.id, Name: "Shop", Replace: true})
		if err != nil {
			t.Fatal(err)
		}
		for slot, lines := range want {
			if got := again.Panes[slot].Start; !slices.Equal(got, lines) {
				t.Fatalf("saved over, %s keeps the lines %q, want %q", slot, got, lines)
			}
		}
	}
	a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"npm run dev"}}})
	if len(a.st.Notices) != 0 {
		t.Fatalf("saving the lines said %+v", a.st.Notices)
	}
	saveOver(map[string][]string{"p1": {"npm run dev"}, "p2": {"echo KEEP"}})

	a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"npm test"}, "p2": nil}})
	saveOver(map[string][]string{"p1": {"npm test"}, "p2": nil})
}

// slotPanes are the panes of window w by the slot of the layout each
// was opened from, or saved into.
func slotPanes(t *testing.T, a *app, w *ownWin) map[string]string {
	t.Helper()
	waitFor(t, a, "the panes to start", func() bool { return len(waitingIn(a, w)) == 0 })
	out := map[string]string{}
	for _, p := range a.panesIn(w) {
		if from, ok := a.linesFrom[p.ID]; ok {
			out[from.slot] = p.ID
		}
	}
	return out
}

// Layout Shop, open in two windows: the second, its first terminal
// closed, is saved over Shop, which names its second terminal p1. Lines
// the dialog gives p1 then reach that terminal alone: the first window
// was opened from Shop as it was before, so its terminals keep what they
// have, and the first window saved over Shop puts no line on either.
func TestAWindowOfALayoutSavedOverElsewhereKeepsItsLines(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	term := settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}
	kept := keep(t, a, alone("Shop", term, term))
	w1 := opens(t, a, opened, kept.ID)
	one := slotPanes(t, a, w1)
	w2 := opens(t, a, opened, kept.ID)
	two := slotPanes(t, a, w2)

	a.remove(two["p1"])
	if _, _, _, err := a.saveLayout(SaveLayout{Window: w2.id, Name: "Shop", Replace: true}); err != nil {
		t.Fatal(err)
	}
	a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"run-b"}}})
	if got := a.startLines[two["p2"]]; !slices.Equal(got, []string{"run-b"}) {
		t.Fatalf("the saved window's terminal was given %q", got)
	}
	for slot, id := range one {
		if got := a.startLines[id]; got != nil {
			t.Fatalf("the other window's %s was given %q", slot, got)
		}
	}
	again, _, _, err := a.saveLayout(SaveLayout{Window: w1.id, Name: "Shop", Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if a1, b1 := again.Panes["p1"].Start, again.Panes["p2"].Start; a1 != nil || b1 != nil {
		t.Fatalf("saved over, the terminals keep %q and %q", a1, b1)
	}
}

// A layout removed, and its ID given to another layout saved under its
// name: the new layout's lines reach its own window alone, and a window
// of the one removed keeps the lines it had. Undo of a removal brings
// the layout back as it was, so the dialog reaches its window again.
func TestARemovedLayoutsIDLinksNoPane(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	term := settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}
	shop := keep(t, a, alone("Shop", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell(), Start: []string{"echo OLD"}}))
	other := keep(t, a, alone("Other", term))
	old := slotPanes(t, a, opens(t, a, opened, shop.ID))["p1"]

	// Removed and brought back: the window is the layout's again.
	a.handle(RemoveLayout{ID: shop.ID})
	a.handle(a.st.Notices[len(a.st.Notices)-1].On.(RestoreLayout))
	a.handle(SaveStartLines{Layout: layoutNamed(t, a, "Shop"), Lines: map[string][]string{"p1": {"echo BACK"}}})
	if got := a.startLines[old]; !slices.Equal(got, []string{"echo BACK"}) {
		t.Fatalf("brought back by Undo, the window's terminal was given %q", got)
	}

	// Removed, and its ID given to another layout.
	a.handle(RemoveLayout{ID: shop.ID})
	a.st.Notices = nil
	w := opens(t, a, opened, other.ID)
	mine := slotPanes(t, a, w)["p1"]
	again, _, _, err := a.saveLayout(SaveLayout{Window: w.id, Name: "Shop"})
	if err != nil || again.ID != shop.ID {
		t.Fatalf("saved as %q: %v", again.ID, err)
	}
	a.handle(SaveStartLines{Layout: again, Lines: map[string][]string{"p1": {"echo NEW"}}})
	if got := a.startLines[old]; !slices.Equal(got, []string{"echo BACK"}) {
		t.Fatalf("the removed layout's terminal was given %q", got)
	}
	if got := a.startLines[mine]; !slices.Equal(got, []string{"echo NEW"}) {
		t.Fatalf("the new layout's own terminal was given %q", got)
	}
}

// waitingSlow opens layout id, whose terminal waits for its folder to be
// looked at, and returns the waiting pane's ID while it still waits.
func waitingSlow(t *testing.T, a *app, id string) string {
	t.Helper()
	a.handle(OpenLayout{ID: id})
	var pane string
	waitFor(t, a, "the waiting pane", func() bool {
		for p, s := range a.slots {
			if s.layout == id {
				pane = p
				return true
			}
		}
		return false
	})
	return pane
}

// slowLayout is a layout of one terminal that waits for its folder,
// with start lines.
func slowLayout(t *testing.T, name string, start ...string) settings.SavedLayout {
	return alone(name, settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell(), Folder: t.TempDir(), Start: start})
}

// A layout removed while its terminal is still starting, and brought
// back by Undo once the terminal runs: lines the dialog gives then reach
// the terminal.
func TestUndoAfterAPaneStartedLinksIt(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	kept := keep(t, a, slowLayout(t, "Slow"))
	waitingSlow(t, a, kept.ID)
	a.handle(RemoveLayout{ID: kept.ID})
	undo := a.st.Notices[len(a.st.Notices)-1].On.(RestoreLayout)
	w := layoutWindow(a)
	waitFor(t, a, "the layout's window", func() bool { return opened.Load() == 1 })
	p := onlyPane(t, a, w)
	a.handle(undo)
	a.handle(SaveStartLines{Layout: layoutNamed(t, a, "Slow"), Lines: map[string][]string{"p1": {"echo AFTER"}}})
	if got := a.startLines[p.ID]; !slices.Equal(got, []string{"echo AFTER"}) {
		t.Fatalf("the terminal was given %q", got)
	}
}

// Two notices of one layout removed twice, Undo pressed on both: in
// either order the layout is back, and the dialog reaches its window.
func TestTwoUndosInEitherOrderEndTheSame(t *testing.T) {
	for _, order := range [][2]int{{0, 1}, {1, 0}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			a, opened := openingApp(t)
			a.st.ShellSetup = false
			kept := keep(t, a, alone("Shop", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}))
			pane := slotPanes(t, a, opens(t, a, opened, kept.ID))["p1"]
			var undo []RestoreLayout
			for range 2 {
				a.handle(RemoveLayout{ID: kept.ID})
				undo = append(undo, a.st.Notices[len(a.st.Notices)-1].On.(RestoreLayout))
				a.handle(undo[len(undo)-1])
				a.handle(RemoveLayout{ID: kept.ID})
			}
			a.st.Notices = nil
			a.handle(undo[order[0]])
			a.handle(undo[order[1]])
			if len(a.st.Layouts) != 1 || a.st.Layouts[0].ID != kept.ID {
				t.Fatalf("back as %+v", a.st.Layouts)
			}
			a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"echo BOTH"}}})
			if got := a.startLines[pane]; !slices.Equal(got, []string{"echo BOTH"}) {
				t.Fatalf("the terminal was given %q", got)
			}
		})
	}
}

// A window saved as a layout while a pane waits links the slot it
// waits in, and the pane it becomes: no link is left under the waiting
// pane's ID.
func TestAWaitingPaneLeavesNoLinkBehind(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	kept := keep(t, a, slowLayout(t, "Slow"))
	waiting := waitingSlow(t, a, kept.ID)
	w := layoutWindow(a)
	again, _, _, err := a.saveLayout(SaveLayout{Window: w.id, Name: "Slow", Replace: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := a.linesFrom[waiting]; ok {
		t.Fatalf("the waiting pane is linked: %+v", a.linesFrom[waiting])
	}
	waitFor(t, a, "the layout's window", func() bool { return opened.Load() == 1 })
	p := onlyPane(t, a, w)
	if _, ok := a.linesFrom[waiting]; ok {
		t.Fatalf("started, the waiting pane is still linked: %+v", a.linesFrom[waiting])
	}
	if got := a.linesFrom[p.ID]; got.rev != again.Rev || got.slot != "p1" {
		t.Fatalf("the pane is linked to %+v, want p1 of revision %s", got, again.Rev)
	}
}

// A layout written by a kakel before revisions has none; a window
// opened from it is reached by the dialog all the same.
func TestALayoutWithNoRevLinksItsPanes(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	keep(t, a, alone("Old", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}))
	path := a.settings.Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]any
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	for _, l := range file["layouts"].([]any) {
		delete(l.(map[string]any), "rev")
	}
	if raw, err = json.Marshal(file); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if a.settings, err = settings.Load(path); err != nil {
		t.Fatal(err)
	}
	a.st.Layouts = a.settings.Layouts()
	if rev := a.st.Layouts[0].Rev; rev != "" {
		t.Fatalf("the old layout reads with revision %q", rev)
	}
	pane := slotPanes(t, a, opens(t, a, opened, a.st.Layouts[0].ID))["p1"]
	a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"echo OLD"}}})
	if got := a.startLines[pane]; !slices.Equal(got, []string{"echo OLD"}) {
		t.Fatalf("the terminal was given %q", got)
	}
}

// Another kakel saving over the layout leaves the panes opened here from
// it as they are: the dialog's lines, saved here after, go to the
// layout and not to them.
func TestASaveFromAnotherKakelLeavesThePanes(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	kept := keep(t, a, alone("Shop", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell()}))
	pane := slotPanes(t, a, opens(t, a, opened, kept.ID))["p1"]
	other, err := settings.Load(a.settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := other.SaveLayout(kept, true); err != nil {
		t.Fatal(err)
	}
	a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"echo THERE"}}})
	if len(a.st.Notices) != 0 {
		t.Fatalf("saving the lines said %+v", a.st.Notices)
	}
	if got := a.st.Layouts[0].Panes["p1"].Start; !slices.Equal(got, []string{"echo THERE"}) {
		t.Fatalf("the layout holds %q", got)
	}
	if got := a.startLines[pane]; got != nil {
		t.Fatalf("the terminal was given %q", got)
	}
}

// A layout of one terminal through the window desk, open in three
// windows here: the second window's pane dragged into the first, and the
// first saved over the layout. What is written down of the panes it
// holds there still has all three, the third window's too, so that one
// is watched again, as before start lines.
func TestSavingOverWithAPaneDraggedInKeepsEveryFarPane(t *testing.T) {
	a, b, opened := windowLayoutApps(t)
	id := keep(t, b, alone("Desk", throughDesk(settings.SavedPane{Kind: settings.LayoutTerminal}))).ID
	w1 := openLanded(t, a, b, opened, id)
	w2 := openLanded(t, a, b, opened, id)
	w3 := openLanded(t, a, b, opened, id)
	third := farIDs(b, w3)[0]
	b.moveToWindow(b.panesIn(w2)[0].ID, w1)
	if _, _, _, err := b.saveLayout(SaveLayout{Window: w1.id, Name: "Desk", Replace: true}); err != nil {
		t.Fatal(err)
	}
	recs := b.recordsOf(id)
	if len(recs) != 3 || !slices.ContainsFunc(recs, func(r settings.LayoutPane) bool { return r.Pane == third }) {
		t.Fatalf("written down: %+v; want three, the third window's %s among them", recs, third)
	}
}

// layoutNamed is the layout called name, as the app has it now.
func layoutNamed(t *testing.T, a *app, name string) settings.SavedLayout {
	t.Helper()
	for _, l := range a.st.Layouts {
		if l.Name == name {
			return l
		}
	}
	t.Fatalf("no layout is called %s", name)
	return settings.SavedLayout{}
}

// Lines saved in the dialog while a terminal is still starting, its
// folder being looked for, are the lines typed into it, not those the
// layout had as it opened.
func TestLinesSavedWhileStartingAreTyped(t *testing.T) {
	a, opened := openingApp(t)
	a.st.ShellSetup = false
	kept := keep(t, a, alone("Slow", settings.SavedPane{Kind: settings.LayoutTerminal, Command: plainShell(),
		Folder: t.TempDir(), Start: []string{"echo OLD-LINE"}}))
	a.handle(OpenLayout{ID: kept.ID})
	waitFor(t, a, "the waiting pane", func() bool {
		for _, s := range a.slots {
			if s.layout == kept.ID {
				return true
			}
		}
		return false
	})
	a.handle(SaveStartLines{Layout: a.st.Layouts[0], Lines: map[string][]string{"p1": {"echo NEW-LINE"}}})
	w := layoutWindow(a)
	waitFor(t, a, "the layout's window", func() bool { return opened.Load() == 1 })
	p := onlyPane(t, a, w)
	linesOf(t, a, p.ID, "NEW-LINE")
	if strings.Contains(a.terminal(p.ID).AllText(), "OLD-LINE") {
		t.Fatalf("the old line was typed: %q", a.terminal(p.ID).AllText())
	}
	if got := a.startLines[p.ID]; !slices.Equal(got, []string{"echo NEW-LINE"}) {
		t.Fatalf("the pane keeps %q", got)
	}
}

// The card counts only the lines a terminal types: not those of a
// terminal through another kakel window, which the dialog does not show.
func TestTheCountLeavesOutTerminalsThroughAWindow(t *testing.T) {
	l := alone("Mixed",
		settings.SavedPane{Kind: settings.LayoutTerminal, Start: []string{"make"}},
		settings.SavedPane{Kind: settings.LayoutTerminal, Host: "desk", HostID: "s3", Window: true, Start: []string{"htop", "top"}},
		settings.SavedPane{Kind: settings.LayoutCommand, Line: "htop", Start: []string{"no"}})
	if n := LayoutStartLines(l); n != 1 {
		t.Fatalf("counted %d start lines, want 1", n)
	}
}
