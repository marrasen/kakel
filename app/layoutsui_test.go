package app

import (
	"log"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/shellsetup"
)

// Saving says so, and what of the window's panes it left out that the
// user might look for, but not the tools; saved over a layout of the same
// name, it says it replaced it, and only then.
func TestSavingALayoutSaysWhatItLeftOut(t *testing.T) {
	a, web := layoutApp(t)
	one := workWindow(t, a, web)
	a.addPane(Pane{ID: "r1", Title: "scrollback", Kind: KindReader}, nil, Placement{Beside: "p1"})
	a.front(one)

	a.handle(SaveLayout{Window: one.id, Name: "Work"})
	n := a.st.Notices
	if len(n) != 1 || n[0].Title != "Saved Work" || n[0].Kind != NoticeWorked || n[0].Body != "Left out: 1 reader." {
		t.Fatalf("saved, it said %+v", n)
	}
	a.handle(SaveLayout{Window: one.id, Name: "work", Replace: true})
	if n := a.st.Notices; len(n) != 2 || n[1].Title != "Replaced work" {
		t.Fatalf("saved over, it said %+v", n)
	}
	// Replace asked for, with no layout of the name to replace.
	a.handle(SaveLayout{Window: one.id, Name: "Other", Replace: true})
	if n := a.st.Notices; len(n) != 3 || n[2].Title != "Saved Other" {
		t.Fatalf("saved under a new name, it said %+v", n)
	}
}

// Save Layout saves the window it is in, and the layout that window
// came from is offered; from a window of tools it saves the window last
// worked in, offering that one's layout; with neither, it has nothing to
// save.
func TestSaveLayoutSavesTheWindowWorkedIn(t *testing.T) {
	a, web := layoutApp(t)
	one := workWindow(t, a, web)
	two := a.cur
	a.front(one)
	a.noteWork()
	a.handle(SaveLayout{Window: one.id, Name: "Work"})
	if st := a.stateFor(one, a.st); st.SaveWindow != one.id || st.SaveFrom != "work" {
		t.Fatalf("saved, the window saves %d from %q", st.SaveWindow, st.SaveFrom)
	}
	if st := a.stateFor(two, a.st); st.SaveWindow != two.id || st.SaveFrom != "" {
		t.Fatalf("the other window saves %d from %q", st.SaveWindow, st.SaveFrom)
	}

	tools := a.addWindow(gunimtest.New(t, geom.Sz(400, 300), nil).Client(), nil)
	a.front(tools)
	a.next = 100
	a.handle(ShowLayouts{})
	if st := a.stateFor(tools, a.st); st.SaveWindow != one.id || st.SaveFrom != "work" {
		t.Fatalf("a window of tools saves %d from %q", st.SaveWindow, st.SaveFrom)
	}
	a.work = nil
	if st := a.stateFor(tools, a.st); st.SaveWindow != 0 {
		t.Fatalf("with no window worked in, a window of tools saves %d", st.SaveWindow)
	}
}

// A layout removed is said, with Undo, which puts it back where it
// stood, opening at start as it did.
func TestARemovedLayoutComesBackWithUndo(t *testing.T) {
	a, _ := layoutApp(t)
	a.addPane(Pane{ID: "p1"}, nil, Placement{})
	for _, name := range []string{"One", "Two", "Three"} {
		a.handle(SaveLayout{Name: name})
	}
	a.handle(SetLayoutAtStart{ID: "two", On: true})
	a.st.Notices = nil

	a.handle(RemoveLayout{ID: "two"})
	n := a.st.Notices
	if len(n) != 1 || n[0].Title != "Removed Two" || n[0].Action != "Undo" || len(a.st.Layouts) != 2 {
		t.Fatalf("removed, it said %+v, and the layouts are %+v", n, a.st.Layouts)
	}
	a.handle(n[0].On)

	var names []string
	for _, l := range a.settings.Layouts() {
		names = append(names, l.Name)
	}
	if !slices.Equal(names, []string{"One", "Two", "Three"}) || !a.st.Layouts[1].AtStart || a.st.Layouts[1].ID != "two" {
		t.Fatalf("put back, the layouts are %+v", a.st.Layouts)
	}
	// Twice is once; and over a name taken since, it says so.
	a.handle(n[0].On)
	if len(a.st.Layouts) != 3 || len(a.st.Notices) != 1 {
		t.Fatalf("put back twice, the layouts are %+v, and it said %+v", a.st.Layouts, a.st.Notices)
	}
	a.handle(RemoveLayout{ID: "two"})
	a.handle(SaveLayout{Name: "two"})
	a.handle(a.st.Notices[1].On)
	if got := a.st.Notices[len(a.st.Notices)-1]; got.Title != "Couldn't restore the layout" || got.Body != "another layout is called Two now" {
		t.Fatalf("put back over a name taken, it said %+v", got)
	}
}

// The launcher finds each layout by its name and the machines it opens
// on, saying what it holds, and launching one opens it in a window of
// its own.
func TestTheLauncherOpensALayout(t *testing.T) {
	a, opened := openingApp(t)
	kept := keep(t, a, alone("Evening", settings.SavedPane{Kind: settings.LayoutTerminal, Command: []string{"/bin/sh"}}))
	a.st.Layouts = a.settings.Layouts()

	things := a.launchThings(a.launchMachines())
	i := slices.IndexFunc(things, func(th LaunchThing) bool { return th.Kind == "layout" })
	if i < 0 || things[i].Title != "Evening" || things[i].Note != "Layout · 1 pane" || !slices.Contains(things[i].Also, "This computer") {
		t.Fatalf("the launcher offers %+v", things)
	}

	before := len(a.wins)
	a.handleLaunch(Launch{Action: things[i].Action})
	waitFor(t, a, "the layout's window", func() bool { return opened.Load() == 1 && len(a.wins) > before })
	if w := layoutWindow(a); w.fromLayout != kept.ID {
		t.Fatalf("the window opened says it came from %q", w.fromLayout)
	}
}

// The Layouts pane is a tool: a window holding it is not saved with it,
// and Exit does not ask about it.
func TestTheLayoutsPaneIsATool(t *testing.T) {
	a, _ := layoutApp(t)
	a.addPane(Pane{ID: "t1", Title: "sh"}, nil, Placement{})
	a.handle(ShowLayouts{})
	id := a.paneOf(KindLayouts)
	if id == "" {
		t.Fatal("no Layouts pane opened")
	}
	a.handle(ShowLayouts{})
	if n := len(slices.DeleteFunc(slices.Clone(a.st.Panes), func(p Pane) bool { return p.Kind != KindLayouts })); n != 1 {
		t.Fatalf("asked again, %d Layouts panes are open", n)
	}

	l, skipped, _, err := a.saveLayout(SaveLayout{Name: "Work"})
	if err != nil || skipped.total() != 0 || len(l.Panes) != 1 {
		t.Fatalf("saved %+v, %+v left out, %v", l.Panes, skipped, err)
	}
	if !a.nothingToLose(Pane{ID: id, Kind: KindLayouts}) {
		t.Fatal("Exit asks about the Layouts pane")
	}
}

// What a layout holds and where is said in words.
func TestALayoutIsSaidInWords(t *testing.T) {
	item := func(id string) *settings.LayoutNode { return &settings.LayoutNode{ID: id, Kind: "item"} }
	l := settings.SavedLayout{Root: &settings.LayoutNode{Kind: "group", Kids: []*settings.LayoutNode{item("p1"), item("p2"), item("p3"), item("p4")}},
		Panes: map[string]settings.SavedPane{
			"p1": {Kind: settings.LayoutFiles, Host: "old", HostID: "s1"},
			"p2": {Kind: settings.LayoutTerminal},
			"p3": {Kind: settings.LayoutTerminal, Host: "gone", HostID: "s9"},
			"p4": {Kind: settings.LayoutTerminal, Host: "quick.example"},
		}}
	if got := LayoutHolds(l); got != "3 terminals and 1 file manager" {
		t.Errorf("it holds %q", got)
	}
	named := func(id string) (string, bool) { return map[string]string{"s1": "new"}[id], id == "s1" }
	if got := LayoutMachines(l, named); !slices.Equal(got, []string{"new", "This computer", "gone", "quick.example"}) {
		t.Errorf("it opens on %q", got)
	}
}

// Start lines saved reach the settings file, cleaned, on terminals
// alone, and are read back from it; a name given since is kept. Saved
// to a layout removed since, or saved over with other panes, they are
// refused, said and written to the Window Log, and nothing changes.
func TestStartLinesSavedReachTheSettings(t *testing.T) {
	a, _ := layoutApp(t)
	kept := keep(t, a, alone("Shop",
		settings.SavedPane{Kind: settings.LayoutTerminal, Command: []string{"/bin/sh"}},
		settings.SavedPane{Kind: settings.LayoutFiles, Folder: "/srv"}))
	was := a.st.Layouts[0]
	a.handle(RenameLayout{ID: kept.ID, Name: "Shop front"})

	a.handle(SaveStartLines{Layout: was, Lines: map[string][]string{"p1": {"  npm install\t--quiet ", "", "npm run dev\nnpm test"}, "p2": {"ls"}}})
	if n := a.st.Notices; len(n) != 0 {
		t.Fatalf("saved, it said %+v", n)
	}
	again, err := settings.Load(a.settings.Path())
	if err != nil {
		t.Fatal(err)
	}
	got := again.Layouts()[0]
	if !slices.Equal(got.Panes["p1"].Start, []string{"  npm install --quiet", "npm run dev", "npm test"}) || got.Panes["p2"].Start != nil || got.Name != "Shop front" {
		t.Fatalf("read back, the layout is %q with %+v", got.Name, got.Panes)
	}

	var logged strings.Builder
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	for _, c := range []struct {
		change func()
		body   string
	}{
		{func() {
			l := settings.SavedLayout{Name: "Shop front", Root: item("p1"), Panes: map[string]settings.SavedPane{"p1": {Kind: settings.LayoutTerminal}}}
			if _, _, err := a.settings.SaveLayout(l, true); err != nil {
				t.Fatal(err)
			}
		}, "layout saved over"},
		{func() { a.handle(RemoveLayout{ID: kept.ID}) }, "layout not found"},
	} {
		if _, _, err := a.settings.SaveLayout(settings.SavedLayout{Name: "Shop front", Root: was.Root, Panes: was.Panes}, true); err != nil {
			t.Fatal(err)
		}
		c.change()
		before := a.settings.Layouts()
		a.st.Notices = nil
		a.handle(SaveStartLines{Layout: was, Lines: map[string][]string{"p1": {"make"}}})
		n := a.st.Notices
		if len(n) != 1 || n[0].Title != "Couldn't save the start lines" || n[0].Body != c.body {
			t.Fatalf("refused, it said %+v", n)
		}
		if !reflect.DeepEqual(a.settings.Layouts(), before) {
			t.Fatalf("refused, the layouts changed to %+v", a.settings.Layouts())
		}
		if !strings.Contains(logged.String(), "Couldn't save the start lines: "+c.body) {
			t.Fatalf("%q is not in the Window Log", c.body)
		}
	}
}

// A layout's terminal is said by the shell it starts, and that shell's
// kind found, from what the window was told of the shells: the one kept,
// or else the default, here; the profile kept, or the server's default,
// or its own shell, on a server; none through another kakel window.
func TestALayoutsTerminalIsSaidByItsShell(t *testing.T) {
	known := KnownShells{
		Profiles: []settings.Profile{
			{ID: "profile:py", Name: "Python", Command: []string{"python3"}},
			{ID: "profile:ps", Name: "Admin PowerShell", Command: []string{"pwsh", "-NoLogo"}},
			{ID: "profile:zsh", Name: "Zsh", HostID: "s1", Command: []string{"zsh"}},
		},
		Shells:  []ShellChoice{{ID: "cmd", Title: "Command Prompt (mine)"}},
		Found:   []ShellChoice{{ID: "bash", Title: "Bash", Line: "/bin/bash"}, {ID: "cmd", Title: "Command Prompt", Line: `C:\Windows\System32\cmd.exe`}},
		Default: "bash",
		Defaults: map[machines.ID]MachineDefaults{
			"s3": {Shells: []ShellEntry{{Action: "login", Name: "Own Shell"}}, Login: "Own Shell"},
			"s5": {Runs: "profile:pwsh7", Login: "Login Shell"},
			"s6": {Runs: "profile:zsh6", Login: "Login Shell"},
			// Renamed, on a server of no profiles.
			"s7": {Shells: []ShellEntry{{Action: "terminal", Name: "My Login", Default: true}}, Login: "My Login"},
		},
	}
	known.Profiles = append(known.Profiles,
		settings.Profile{ID: "profile:pwsh7", Name: "PowerShell 7", HostID: "s5", Command: []string{"pwsh"}},
		settings.Profile{ID: "profile:zsh6", Name: "Zsh 6", HostID: "s6", Command: []string{"zsh"}})
	for _, c := range []struct {
		r    settings.SavedPane
		name string
		kind shellsetup.Route
	}{
		{settings.SavedPane{}, "Bash", shellsetup.Posix},
		{settings.SavedPane{Profile: "cmd"}, "Command Prompt (mine)", shellsetup.Cmd},
		{settings.SavedPane{Profile: "gone"}, "Bash", shellsetup.Posix},
		{settings.SavedPane{Profile: "profile:ps"}, "Admin PowerShell", shellsetup.PowerShell},
		{settings.SavedPane{Profile: "profile:py"}, "Python", shellsetup.NoRoute},
		{settings.SavedPane{Command: []string{`C:\Windows\System32\cmd.exe`}}, "Command Prompt (mine)", shellsetup.Cmd},
		{settings.SavedPane{Command: []string{"/usr/bin/fish"}}, "fish", shellsetup.Posix},
		{settings.SavedPane{Host: "web", HostID: "s1", Profile: "profile:zsh"}, "Zsh", shellsetup.Posix},
		{settings.SavedPane{Host: "web", HostID: "s1", Profile: LoginShell}, "Login Shell", shellsetup.Posix},
		{settings.SavedPane{Host: "db", HostID: "s3", Profile: LoginShell}, "Own Shell", shellsetup.Posix},
		{settings.SavedPane{Host: "desk", HostID: "s4", Window: true}, "", shellsetup.NoRoute},
		// A profile removed since starts what the server runs by default,
		// as opening does: its default profile, or with its own shell
		// hidden, the first shown, or its own shell by the name it shows.
		{settings.SavedPane{Host: "win", HostID: "s5", Profile: "profile:gone"}, "PowerShell 7", shellsetup.PowerShell},
		{settings.SavedPane{Host: "win", HostID: "s5"}, "PowerShell 7", shellsetup.PowerShell},
		{settings.SavedPane{Host: "hid", HostID: "s6"}, "Zsh 6", shellsetup.Posix},
		{settings.SavedPane{Host: "own", HostID: "s7", Profile: "profile:gone"}, "My Login", shellsetup.Posix},
	} {
		if name, kind := LayoutShell(c.r, known); name != c.name || kind != c.kind {
			t.Errorf("%+v starts %q, %v; want %q, %v", c.r, name, kind, c.name, c.kind)
		}
	}
}
