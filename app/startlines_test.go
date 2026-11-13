package app

import (
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
// prompt takes to work out: the second line is not typed into cat.
func TestARunningStartLineHoldsBackTheNext(t *testing.T) {
	for name, rc := range map[string]string{
		"a prompt at once": "",
		"a slow prompt":    `PS1='$(sleep 0.4)\$ '`,
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
