package app

import (
	"errors"
	"slices"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/shellsetup"
)

// A machine's pane: everything about one machine in one place, its own
// settings, how it is reached, and its profiles. It opens from the
// machine's card and menus, and a server being added is one too, a
// draft, which becomes the server's pane once it is saved: a pane, not
// a dialog, so an address can be copied from a terminal beside it.

// KindMachine is a machine's pane. Its Machine is the machine, "" for
// this computer, and Draft says it holds a server not saved yet.
const KindMachine = "machine"

// Intents for the machine panes.
type (
	// ShowMachine goes to Machine's pane, opening it when it is closed.
	ShowMachine struct{ Machine machines.ID }
	// AddServer opens a pane for a server not saved yet.
	AddServer struct{}
	// DraftTyped says whether anything has been typed into the draft
	// machine pane Pane, which closing it then asks before losing.
	DraftTyped struct {
		Pane  string
		Typed bool
	}
	// SetDefaultProfile makes profile ID what a new terminal on Machine
	// runs: a shell or profile here, and on a server one of its
	// profiles or LoginShell.
	SetDefaultProfile struct {
		Machine machines.ID
		ID      string
	}
)

// showMachine goes to m's pane, opening it in the window in front when
// it is closed.
func (a *app) showMachine(m machines.ID) error {
	if m != machines.Local {
		if _, ok := a.machines.Saved(m); !ok {
			return errors.New(a.machines.Name(m) + " is not a saved server")
		}
	}
	if m == machines.Local {
		// The drives here, for its Files, as they are now: off the
		// program's goroutine, as a drive can be slow to answer.
		go func() { _, _ = a.fileManagerPlaces() }()
	}
	if id := a.machinePane(m); id != "" {
		a.focusRaised(id)
		return nil
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: a.machineTitle(m), Kind: KindMachine, Machine: m}, nil, Placement{})
	return nil
}

// addServer opens a pane for a server not saved yet, or goes to the one
// open already.
func (a *app) addServer() {
	for _, p := range a.st.Panes {
		if p.Kind == KindMachine && p.Draft && !a.closing[p.ID] {
			a.focusRaised(p.ID)
			return
		}
	}
	a.next++
	a.addPane(Pane{ID: "p" + itoa(a.next), Title: "New server", Kind: KindMachine, Draft: true}, nil, Placement{})
}

// keepDraft asks, before a draft machine pane with something typed in
// it closes, whether to lose it, and reports whether it asked: the
// pane closes once the answer is yes.
func (a *app) keepDraft(id string) bool {
	if !a.draftTyped[id] {
		return false
	}
	if a.draftAsked[id] {
		return true
	}
	a.draftAsked[id] = true
	a.askThen(a.ctx, Ask{Title: "Discard the new server?", Text: "What was typed for it is not saved.", Yes: "Discard", Danger: true},
		func(ans AskAnswered) {
			delete(a.draftAsked, id)
			if ans.Yes {
				delete(a.draftTyped, id)
				a.closePane(id)
			}
		})
	return true
}

// machinePane is the pane of machine m open, or "".
func (a *app) machinePane(m machines.ID) string {
	for _, p := range a.st.Panes {
		if p.Kind == KindMachine && !p.Draft && p.Machine == m && !a.closing[p.ID] {
			return p.ID
		}
	}
	return ""
}

// machineTitle is what the pane of machine m is called.
func (a *app) machineTitle(m machines.ID) string {
	if m == machines.Local {
		return "This computer"
	}
	return a.machines.Name(m)
}

// savedInPane turns the draft pane id into the pane of the server just
// saved as m, and names the machine panes again, for a rename.
func (a *app) savedInPane(id string, m machines.ID) {
	for i, p := range a.st.Panes {
		switch {
		case p.ID == id && p.Kind == KindMachine && p.Draft:
			delete(a.draftTyped, id)
			a.st.Panes[i].Draft, a.st.Panes[i].Machine = false, m
			a.st.Panes[i].Title = a.machineTitle(m)
		case p.Kind == KindMachine && !p.Draft && p.Machine == m:
			a.st.Panes[i].Title = a.machineTitle(m)
		}
	}
}

// closeMachinePanes closes the panes of machine m, as it is removed.
func (a *app) closeMachinePanes(m machines.ID) {
	for _, p := range slices.Clone(a.st.Panes) {
		if p.Kind == KindMachine && !p.Draft && p.Machine == m {
			a.closePane(p.ID)
		}
	}
}

// setDefaultProfile makes a profile what a new terminal on its machine
// runs.
func (a *app) setDefaultProfile(in SetDefaultProfile) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be kept")
	}
	if in.Machine == machines.Local {
		if in.ID == a.ownShell {
			return a.pickShell("")
		}
		return a.pickShell(in.ID)
	}
	id := in.ID
	if id == LoginShell {
		id = ""
	}
	err := a.settings.PutServerShell(string(in.Machine), id)
	a.st.ServerShells = a.settings.ServerShells()
	return err
}

// setupOn is whether a shell on machine m is taught by default, before
// what a profile says: Shell Setup here, and the server's own tick on a
// saved server.
func (a *app) setupOn(m machines.ID) bool {
	if m == machines.Local {
		return a.st.ShellSetup
	}
	for _, h := range a.st.Saved {
		if machines.ID(h.ID) == m {
			return h.Setup
		}
	}
	return false
}

// teachesFar reports whether a shell started on the saved server m is
// taught: p is the profile it runs, empty for the account's own shell,
// whose found profile may say otherwise. A profile's program the lines
// are not written for is left alone.
func (a *app) teachesFar(m machines.ID, p settings.Profile) bool {
	on := a.setupOn(m)
	set := p.Setup
	if p.ID == "" {
		set = a.foundEdit(string(m), LoginShell).Setup
	}
	if set != nil {
		on = *set
	}
	return on && (len(p.Command) == 0 || shellsetup.Knows(p.Command))
}

// moveStartFolder makes the start folder This Computer kept, from
// before it had profiles, the folder of each profile here that has none
// of its own, the shells found and those made, once: a profile is where
// a terminal here starts now, and each started there before.
func (a *app) moveStartFolder() {
	if a.settings == nil || a.settings.Err() != nil {
		return
	}
	start := a.settings.Local()
	if start == "" {
		return
	}
	var err error
	for _, p := range a.st.Profiles {
		if p.HostID == "" && p.Folder == "" && err == nil {
			p.Folder = start
			err = a.settings.PutProfile(p, nil)
		}
	}
	for _, s := range a.detected {
		if f := a.foundEdit("", s.ID); f.Folder == "" && err == nil {
			f.Folder = start
			err = a.settings.PutFound(f)
			a.st.Found = a.settings.Found()
		}
	}
	a.st.Profiles = a.settings.Profiles()
	if err == nil {
		err = a.settings.PutLocal("")
		a.showThisComputer()
	}
	if err != nil {
		a.failed("Couldn't move the start folder to the profiles", err.Error())
	}
}

// shellFoundAs is the ID of the shell found here that argv runs, the
// user's own when argv is empty, and "" for none.
func (a *app) shellFoundAs(argv []string) string {
	if len(argv) == 0 {
		return a.ownShell
	}
	if s, ok := shellfind.Running(a.detected, argv); ok {
		return s.ID
	}
	return ""
}

// Intents for what a machine keeps for next time, from its pane.
type (
	// ForgetCommand takes a saved command off the list.
	ForgetCommand struct{ Saved settings.SavedCommand }
	// ForgetTunnel takes a saved tunnel off the list.
	ForgetTunnel struct{ Saved settings.SavedTunnel }
)

// forgetCommand takes a saved command off the list.
func (a *app) forgetCommand(c settings.SavedCommand) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be kept")
	}
	if err := a.settings.DropCommand(c.Line); err != nil {
		return err
	}
	a.st.SavedCommands = a.settings.Commands()
	return nil
}

// forgetTunnel takes a saved tunnel off the list.
func (a *app) forgetTunnel(t settings.SavedTunnel) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be kept")
	}
	if err := a.settings.DropTunnel(t); err != nil {
		return err
	}
	a.st.SavedTunnels = a.settings.Tunnels()
	return nil
}
