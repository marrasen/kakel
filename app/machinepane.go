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
	ShowMachine struct {
		Machine machines.ID
		// Row is the row to show, as MachineRowLog; "" for the
		// machine's own.
		Row string
	}
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
	// MachineRowShown says machine pane Pane has shown the row it was
	// asked for, so the pane does not show it again when it moves.
	MachineRowShown struct{ Pane string }
	// ConnectMachine connects to Machine and opens nothing on it, its
	// card or pane showing how that goes: a saved server or window, or
	// a quick connection again, at its address.
	ConnectMachine struct{ Machine machines.ID }
	// ReorderProfiles keeps IDs as the order of Machine's profiles: the
	// order every list of them shows.
	ReorderProfiles struct {
		Machine machines.ID
		IDs     []string
	}
	// SetDisconnectAsk has Disconnect on Machine ask before closing what
	// is open on it, or with Ask false close it at once.
	SetDisconnectAsk struct {
		Machine machines.ID
		Ask     bool
	}
)

// The rows of a machine's pane a ShowMachine can ask for.
const (
	MachineRowLog       = "log"
	MachineRowAddTunnel = "add:tunnel"
	MachineRowAddSOCKS  = "add:socks"
)

// showMachine goes to m's pane, opening it in the window in front when
// it is closed, at row.
func (a *app) showMachine(m machines.ID, row string) error {
	if m != machines.Local {
		_, saved := a.machines.Saved(m)
		known := slices.ContainsFunc(a.machines.Infos(), func(i machines.Info) bool { return i.ID == m })
		if !saved && !known {
			if row == MachineRowLog && a.machines.Get(m).Log != nil {
				// A connection made by address that failed: its log
				// is all there is of it.
				a.showLog(m)
				return nil
			}
			return errors.New("machine not found")
		}
	}
	defer a.askRow(m, row)
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

// askRow has m's pane show row.
func (a *app) askRow(m machines.ID, row string) {
	id := a.machinePane(m)
	if id == "" || row == "" {
		return
	}
	a.setPane(id, func(p *Pane) { p.Row, p.RowAt = row, p.RowAt+1 })
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

// connectMachine connects to m in place.
func (a *app) connectMachine(m machines.ID) error {
	if target, window, ok := a.machines.Quick(m); ok {
		if window {
			return a.reachWindow(ConnectWindow{Addr: target, ID: m, Only: true, InPlace: true}, false, nil)
		}
		return a.connect(ConnectTo{Target: target, As: m, Only: true, InPlace: true})
	}
	return a.connect(ConnectTo{Server: m, Only: true, InPlace: true})
}

// reorderProfiles keeps the order of a machine's profiles.
func (a *app) reorderProfiles(in ReorderProfiles) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be saved")
	}
	err := a.settings.PutProfileOrder(string(in.Machine), in.IDs)
	a.st.ProfileOrder = a.settings.ProfileOrder()
	a.mergeShells()
	return err
}

// InOrder is items sorted by where each one's id stands in order, as a
// machine's profiles are by the order the user dragged them into. Those
// not in it come after, as they were.
func InOrder[T any](items []T, id func(T) string, order []string) []T {
	at := make(map[string]int, len(order))
	for i, o := range order {
		at[o] = i
	}
	out := slices.Clone(items)
	slices.SortStableFunc(out, func(x, y T) int {
		ix, okx := at[id(x)]
		iy, oky := at[id(y)]
		switch {
		case okx && oky:
			return ix - iy
		case okx:
			return -1
		case oky:
			return 1
		}
		return 0
	})
	return out
}

// setDisconnectAsk saves whether Disconnect on a machine asks first.
func (a *app) setDisconnectAsk(in SetDisconnectAsk) error {
	if a.settings == nil {
		return errors.New("the settings could not be read, so nothing can be kept")
	}
	err := a.settings.PutDisconnectAsk(string(in.Machine), in.Ask)
	a.st.NoDisconnectAsk = noAskOf(a.settings)
	return err
}

// noAskOf are the machines s says Disconnect does not ask about.
func noAskOf(s *settings.Settings) []machines.ID {
	var out []machines.ID
	for _, id := range s.NoDisconnectAsk() {
		out = append(out, machines.ID(id))
	}
	return out
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
