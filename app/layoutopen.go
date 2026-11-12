package app

import (
	"context"
	"errors"
	"log"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gunim/driver"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/serve"
	"github.com/marrasen/kakel/settings"
	shellfind "github.com/marrasen/kakel/shells"
	"github.com/marrasen/kakel/vfs"
)

// Opening a layout. It always opens in a new window, so no shell that
// is running is ever closed for it. The window opens in its final
// shape: every pane of the layout is in its place from the start, as a
// waiting pane that says what it waits for. Each is then replaced by
// the pane it waits for, as that starts.
//
// The panes are started machine by machine. This computer's start at
// once; a server is connected to once for all its panes, so a password
// or a host key is asked once, and its panes start as it comes up. A
// server that cannot be reached fails only its own panes, which say why
// and offer Retry.
//
// A pane through another kakel window goes with that window's: the
// window is connected to once for all the panes through it, on its own
// machine or on those it reaches, and each is then asked of it anew, in
// a pane of its own there that this one watches, as New Terminal there
// opens one: unless the layout opened that pane there before and it is
// still open there, when it is watched again (see reattach.go). Nothing
// else open there is ever attached to.

// Intents for opening a layout.
type (
	// OpenLayout opens the layout with ID in a new window.
	OpenLayout struct{ ID string }
	// RetryWaiting starts a failed waiting pane again, and with it every
	// other failed pane of its window on the same machine, which share
	// the one connection.
	RetryWaiting struct{ Pane string }
)

// KindWaiting is a layout's pane before what it holds has started: it
// says what it waits for, or why that failed.
const KindWaiting = "waiting"

// Waiting is what a layout's waiting pane shows: what it waits for, or,
// once that has failed, a heading and why.
type Waiting struct {
	// Doing is what it waits for, as "Connecting to web…".
	Doing string
	// Failed heads what went wrong, as "Couldn't connect to web", and
	// Why says why. Both are empty while it waits.
	Failed, Why string
}

// slot is a layout's waiting pane: the recipe of the pane it waits for,
// the machine that runs on as last found, whether its start failed, and
// a count of its starts, so what a start gone before brings back late
// is let go.
type slot struct {
	recipe  settings.SavedPane
	machine machines.ID
	failed  bool
	gen     int
	// layout and name are the layout's ID and the slot's name in it, by
	// which a pane it opened on another window is found again.
	layout, name string
}

// errNothingToOpen refuses a layout of which no pane can be opened, as
// one written by hand.
var errNothingToOpen = errors.New("the layout has no panes")

// layoutPlan is a layout made ready to open: the window's tree, under
// new names, the waiting panes it holds, in order, and the pane in
// front.
type layoutPlan struct {
	// from is the layout's ID.
	from  string
	root  *dock.Node
	slots []plannedSlot
	front string
}

// plannedSlot is one waiting pane of a plan: its pane's ID, what goes
// there, and the slot's name in the layout.
type plannedSlot struct {
	id     string
	recipe settings.SavedPane
	name   string
}

// openLayout opens the layout with id in a new window.
func (a *app) openLayout(id string) error {
	if a.settings == nil {
		return errNoSettings
	}
	for _, l := range a.settings.Layouts() {
		if l.ID == id {
			return a.openLayoutOf(l)
		}
	}
	return settings.ErrNoLayout
}

// openLayoutOf opens layout l in a new window, where l says it stood. A
// layout of which nothing can be opened is refused before a window
// opens.
func (a *app) openLayoutOf(l settings.SavedLayout) error {
	plan, err := a.planLayout(l)
	if err != nil {
		return err
	}
	a.newWindowAt(placementOf(l.Place), func() { a.fillLayout(plan, a.cur) })
	return nil
}

// placementOf is where a window kept at place opens, or nil for where a
// new window goes. gunim moves it onto a screen when the one it was on
// has gone.
func placementOf(place *settings.WindowPlace) *driver.Placement {
	if place == nil || place.W <= 0 || place.H <= 0 {
		return nil
	}
	return &driver.Placement{Bounds: geom.Rc(place.X, place.Y, place.W, place.H), Maximized: place.Maximized}
}

// planLayout is layout l made ready to open: its tree with its slots
// given panes' IDs, and its splits and groups names, that no part of the
// layout of the windows open has. A slot with no recipe, or one of a
// kind this kakel does not know, is left out, and the parts around it
// close up, as a closed pane's do.
func (a *app) planLayout(l settings.SavedLayout) (layoutPlan, error) {
	var plan layoutPlan
	used := map[string]bool{}
	ids := map[string]string{}
	var walk func(n *settings.LayoutNode) *dock.Node
	walk = func(n *settings.LayoutNode) *dock.Node {
		if n == nil {
			return nil
		}
		switch k := dock.Kind(n.Kind); k {
		case dock.Item:
			r, ok := l.Panes[n.ID]
			if !ok || used[n.ID] || !opensAs(r.Kind) {
				return nil
			}
			used[n.ID] = true
			a.next++
			id := "p" + strconv.Itoa(a.next)
			ids[n.ID] = id
			plan.slots = append(plan.slots, plannedSlot{id: id, recipe: r, name: n.ID})
			return &dock.Node{ID: id, Kind: dock.Item}
		case dock.Row, dock.Column, dock.Group:
			out := &dock.Node{Kind: k, Share: n.Share}
			for _, kid := range n.Kids {
				if kid := walk(kid); kid != nil {
					out.Kids = append(out.Kids, kid)
				}
			}
			out.Front = ids[n.Front]
			return out
		}
		return nil
	}
	// Mended on its own, as the dock mends a layout read from a file: the
	// splits and groups are named past every name the windows' layout has
	// used, a split left with one part gives way to it, and every group
	// has a tab in front.
	one := dock.Layout{Grouped: true, Next: a.layout.Next, Windows: []dock.Window{{Root: walk(l.Root), Keep: true}}}
	one.Mend(nil)
	a.layout.Next = one.Next
	root := one.Windows[0].Root
	in := map[string]bool{}
	for _, id := range root.Items() {
		in[id] = true
	}
	plan.slots = slices.DeleteFunc(plan.slots, func(s plannedSlot) bool { return !in[s.id] })
	if root == nil || len(plan.slots) == 0 {
		return layoutPlan{}, errNothingToOpen
	}
	plan.root = root
	plan.front = frontOf(root)
	plan.from = l.ID
	return plan, nil
}

// opensAs reports whether a layout's pane of kind is one this kakel
// opens.
func opensAs(kind string) bool {
	switch kind {
	case settings.LayoutTerminal, settings.LayoutCommand, settings.LayoutFiles:
		return true
	}
	return false
}

// fillLayout puts plan in window w, new and empty, as waiting panes,
// the one in front with the keyboard, and starts what each waits for.
func (a *app) fillLayout(plan layoutPlan, w *ownWin) {
	a.keepWindow(w.id)
	w.fromLayout = plan.from
	a.layout.Window(w.id).Root = plan.root
	ids := make([]string, 0, len(plan.slots))
	for _, s := range plan.slots {
		m, err := a.slotMachine(s.recipe)
		if err != nil {
			// A server removed since: its pane says so as it starts.
			m = machines.Local
		}
		m = a.slotOn(s.recipe, m)
		a.nameBeyond(s.recipe, m)
		p := a.paneOn(m, Pane{ID: s.id, Kind: KindWaiting, Title: a.waitTitle(s.recipe, m), Becomes: s.recipe.Kind, Named: s.recipe.Title != ""})
		// What it is called with no name given, as a rename to nothing
		// brings back.
		unnamed := s.recipe
		unnamed.Title = ""
		p.shell = a.waitTitle(unnamed, m)
		a.st.Panes = append(a.st.Panes, p)
		a.slots[s.id] = &slot{recipe: s.recipe, layout: plan.from, name: s.name}
		a.setWait(s.id, Waiting{Doing: a.doingOf(s.recipe, m)})
		ids = append(ids, s.id)
	}
	a.stayEmpty = false
	a.setFocusIn(w, plan.front)
	a.startSlots(ids)
}

// waitTitle is what a waiting pane for r, on machine m, is called: the
// name the user gave the pane, or else what the pane it waits for is
// first called.
func (a *app) waitTitle(r settings.SavedPane, m machines.ID) string {
	if r.Title != "" {
		return r.Title
	}
	far := r.Host != "" || r.HostID != ""
	switch r.Kind {
	case settings.LayoutFiles:
		folder := strings.TrimRight(r.Folder, `/\`)
		switch {
		case folder == "":
			return "Files"
		case far:
			// A Windows server's folder may be written with either
			// separator, as C:\Users\me or /C:/Users/me.
			return folder[strings.LastIndexAny(folder, `/\`)+1:]
		}
		return filepath.Base(folder)
	case settings.LayoutCommand:
		return joinLine(splitLine(r.Line))
	}
	if !far {
		switch {
		case r.Profile != "":
			if s, ok := a.foundShell(r.Profile); ok && s.Title != "" {
				return s.Title
			}
		case len(r.Command) > 0:
			return a.localShellName(r.Command)
		}
		return a.localShellName(nil)
	}
	if window, key, far := m.Far(); far && r.Window {
		// Named after the machine alone, as one opened there is: where
		// it is reached through is said beside it.
		return a.machines.FarName(window, key)
	}
	name := a.machineName(r, m)
	if p, ok := a.farProfile(m, r.Profile); ok {
		return p.Name + " on " + name
	}
	return name
}

// machineName is what the machine of r, found as m, is called now: a
// saved server or window by its name now, and one removed by the name it
// was saved with, a window's address without a relay's secret.
func (a *app) machineName(r settings.SavedPane, m machines.ID) string {
	switch {
	case m != machines.Local:
		return a.machines.Name(m)
	case r.Window:
		return relay.Shown(r.Host)
	case r.Host != "":
		return r.Host
	}
	return "this computer"
}

// slotMachine is the machine connected to for a layout's pane kept as r:
// this computer, the server it runs on, or the kakel window it is
// through, found as machineNow finds a server, or for a window kept by
// its address alone, the window known at that address, or a quick
// connection made to it. One removed from the list is refused.
func (a *app) slotMachine(r settings.SavedPane) (machines.ID, error) {
	if !r.Window || r.HostID != "" {
		return a.machineNow(r.Host, r.HostID)
	}
	addr := serveAddr(r.Host)
	if addr == "" {
		return "", errors.New("the layout has no address for the window")
	}
	if known := a.windowAt(addr); known != "" {
		return known, nil
	}
	return a.machines.NewQuick(addr, true), nil
}

// slotOn is the machine a layout's pane kept as r runs on, reached by
// connecting to dial: dial itself, or for a pane on a machine beyond a
// kakel window, that machine there, by the window's key for it. Once the
// window has listed what it has open and the machines it reaches, the
// key kept is used where the window lists it, as after a rename there;
// otherwise the machine it lists by the name kept, as one it reaches by
// a quick connection, whose key is new each run; and one it lists by
// neither goes by its name, which the window also takes, and says in
// its words that it does not reach. A window that has not listed them
// in time is asked by the key kept, or the name where none was.
func (a *app) slotOn(r settings.SavedPane, dial machines.ID) machines.ID {
	if !r.Window || r.On == "" && r.OnKey == "" || dial == machines.Local {
		return dial
	}
	key := r.OnKey
	if key == "" {
		key = r.On
	}
	if w := a.machines.Get(dial).Window; w != nil && listed(w) {
		ms := w.Serve.Machines()
		byKey := r.OnKey != "" && (slices.ContainsFunc(ms, func(m serve.Machine) bool { return m.Key == r.OnKey }) ||
			slices.ContainsFunc(w.Serve.Opens(), func(o serve.Open) bool { return o.Key() == r.OnKey }))
		named := slices.IndexFunc(ms, func(m serve.Machine) bool { return m.Name == r.On })
		switch {
		case byKey:
			key = r.OnKey
		case named >= 0:
			key = ms[named].Key
		case r.On != "":
			key = r.On
		}
	}
	return machines.FarID(dial, key)
}

// nameBeyond notes what the window calls machine m beyond it, a
// layout's pane kept as r runs on, where the window has not said it
// yet: what it lists, or else the name kept, so the pane and what it
// says name the machine as the user knows it, and not by a key.
func (a *app) nameBeyond(r settings.SavedPane, m machines.ID) {
	window, key, far := m.Far()
	if !far {
		return
	}
	if w := a.machines.Get(window).Window; w != nil {
		for _, l := range w.Serve.Machines() {
			if l.Key == key {
				a.machines.NameFar(window, key, l.Name)
				return
			}
		}
	}
	if a.machines.FarName(window, key) == key && r.On != "" {
		a.machines.NameFar(window, key, r.On)
	}
}

// listed reports whether window w has said what it has open, and which
// machines it reaches, or will not.
func listed(w *machines.Window) bool {
	select {
	case <-w.Serve.Listed():
		return true
	default:
		return false
	}
}

// listWait is the longest a layout's pane beyond a window waits for the
// window's first list, to find its machine there by: it comes a moment
// after the connection does.
const listWait = 5 * time.Second

// dialOf is the machine connected to for a pane on m: m, or the window
// a machine beyond one is reached through.
func dialOf(m machines.ID) machines.ID {
	window, _, _ := m.Far()
	return window
}

// doingOf is what a waiting pane for r on machine m says it waits for.
func (a *app) doingOf(r settings.SavedPane, m machines.ID) string {
	switch {
	case r.Host != "" || r.HostID != "":
		return "Connecting to " + a.machineName(r, m) + "…"
	case r.Kind == settings.LayoutFiles:
		return "Opening the files…"
	}
	return "Starting…"
}

// setWait has waiting pane id show w.
func (a *app) setWait(id string, w Waiting) {
	m := maps.Clone(a.st.Waits)
	if m == nil {
		m = map[string]Waiting{}
	}
	m[id] = w
	a.st.Waits = m
}

// startSlots starts what waiting panes ids wait for, machine by machine,
// in the order each machine is first met.
func (a *app) startSlots(ids []string) {
	var keys []string
	by := map[string][]string{}
	for _, id := range ids {
		s := a.slots[id]
		if s == nil {
			continue
		}
		k := s.recipe.HostID + "\n" + s.recipe.Host + "\n" + strconv.FormatBool(s.recipe.Window)
		if _, ok := by[k]; !ok {
			keys = append(keys, k)
		}
		by[k] = append(by[k], id)
	}
	for _, k := range keys {
		a.startOn(by[k])
	}
}

// startOn starts what waiting panes ids, all on one machine, wait for:
// at once on this computer or a machine connected, and otherwise once
// the machine is connected to, once for them all.
func (a *app) startOn(ids []string) {
	r := a.slots[ids[0]].recipe
	m, err := a.slotMachine(r)
	if err != nil {
		title := "Couldn't connect to " + a.machineName(r, machines.Local)
		a.failed(title, reason(err))
		a.slotsFailed(ids, title, err, true)
		return
	}
	for _, id := range ids {
		s := a.slots[id]
		s.machine = a.slotOn(s.recipe, m)
	}
	if m == machines.Local || a.up(m) {
		for _, id := range ids {
			a.startSlot(id)
		}
		return
	}
	name := a.machines.Name(m)
	gens := map[string]int{}
	for _, id := range ids {
		gens[id] = a.slots[id].gen
	}
	// Those still waiting once the connection answers: not closed, nor
	// started another way meanwhile.
	waiting := func() []string {
		return slices.DeleteFunc(slices.Clone(ids), func(id string) bool {
			s := a.slots[id]
			return s == nil || s.failed || s.gen != gens[id]
		})
	}
	// A dial on its way already is waited on: it may be a quiet one, which
	// says nothing of how it went, so a failure is written down here.
	joined := a.machines.Get(m).Dialing != nil
	title := "Couldn't connect to " + name
	err = a.dialInPlace(m, func(err error) {
		if joined {
			a.layoutJoins[m]--
		} else {
			delete(a.layoutDials, m)
		}
		live := waiting()
		switch {
		case len(live) == 0:
		case errors.Is(err, errDeclined) || errors.Is(err, context.Canceled):
			// No toast is shown for what the user cancelled.
			a.slotsFailed(live, title, errors.New("cancelled"), false)
		case err != nil:
			// A dial of the layout's own said it in a toast.
			a.slotsFailed(live, title, err, !joined)
		case !a.up(m):
			a.failed(title, reason(errUnderOtherName))
			a.slotsFailed(live, title, errUnderOtherName, true)
		default:
			for _, id := range live {
				a.startSlot(id)
			}
		}
	})
	if err != nil {
		a.failed(title, reason(err))
		a.slotsFailed(ids, title, err, true)
		return
	}
	switch {
	case joined:
		a.layoutJoins[m]++
	case a.machines.Get(m).Dialing != nil:
		a.layoutDials[m] = true
	}
}

// up reports whether machine m is connected, as a server or a window.
func (a *app) up(m machines.ID) bool {
	return a.machines.Get(m).Conn != nil || a.machines.Get(m).Window != nil
}

// slotsFailed has waiting panes ids say that starting them failed, with
// title and the reason err gives, and offer Retry. said says a toast has
// told it already, and so the Window Log; otherwise it is written there
// here, as every failure shown is.
func (a *app) slotsFailed(ids []string, title string, err error, said bool) {
	why := reason(err)
	if !said {
		log.Printf("%s: %s", title, why)
	}
	for _, id := range ids {
		if s := a.slots[id]; s != nil {
			s.failed = true
			a.setWait(id, Waiting{Failed: title, Why: why})
		}
	}
}

// failure is how a start in a waiting pane says it failed: under title,
// with err, said says a toast has told it already. It says nothing once
// the pane no longer waits for that start.
type failure func(title string, err error, said bool)

// startSlot starts what waiting pane id waits for, on its machine,
// which is this computer or connected.
func (a *app) startSlot(id string) {
	s := a.slots[id]
	s.gen++
	s.failed = false
	gen := s.gen
	// still says the pane is still waiting for this start: not closed,
	// nor failed, nor started again since.
	still := func() bool {
		o := a.slots[id]
		return o == s && o.gen == gen && !o.failed
	}
	fail := func(title string, err error, said bool) {
		if still() {
			a.slotsFailed([]string{id}, title, err, said)
		}
	}
	if w := a.machines.Get(dialOf(s.machine)).Window; s.recipe.Window && (s.recipe.On+s.recipe.OnKey != "" || a.hasRecord(s)) && w != nil && !listed(w) {
		// A machine beyond a window just connected to is found by what
		// the window lists, which comes a moment later, and so is a pane
		// opened there before, to watch again.
		go func() {
			t := time.NewTimer(listWait)
			defer t.Stop()
			select {
			case <-w.Serve.Listed():
			case <-t.C:
			case <-a.ctx.Done():
				return
			}
			a.events <- func() {
				if still() {
					a.startSlotNow(id, still, fail)
				}
			}
		}()
		return
	}
	a.startSlotNow(id, still, fail)
}

// startSlotNow is startSlot once a machine beyond a window can be found.
func (a *app) startSlotNow(id string, still func() bool, fail failure) {
	s := a.slots[id]
	if s.recipe.Window {
		// The window's key for a machine beyond it, as it lists it.
		s.machine = a.slotOn(s.recipe, dialOf(s.machine))
		a.nameBeyond(s.recipe, s.machine)
	}
	r, m := s.recipe, s.machine
	switch {
	case r.Kind == settings.LayoutFiles:
		a.startFilesSlot(id, m, r, still, fail)
	case a.machines.IsWindow(dialOf(m)):
		a.startWindowSlot(id, m, r, still, fail)
	case r.Kind == settings.LayoutCommand:
		a.startCommandSlot(id, m, r, still, fail)
	case m == machines.Local:
		a.startLocalSlot(id, r, still, fail)
	default:
		a.startFarSlot(id, m, r, still, fail)
	}
}

// startLocalSlot starts a terminal on this computer in waiting pane id:
// the profile or the shell kept, or the program kept, in the folder kept.
// A profile no longer here starts the default shell, and a folder no
// longer there the default folder, each said.
func (a *app) startLocalSlot(id string, r settings.SavedPane, still func() bool, fail failure) {
	a.folderThere(r.Folder, "The terminal starts in the default folder.", folderHere, still, func(dir string) {
		argv := a.slotArgv(r, dir)
		sess, err := a.startLocalSession(argv, dir, screen.Cols, screen.Rows, true)
		if err != nil {
			a.failed("Couldn't open a terminal", reason(err))
			fail("Couldn't open a terminal", err, true)
			return
		}
		a.next++
		pane := "p" + strconv.Itoa(a.next)
		sh := screen.Open(sess, a.palette, a.withLinks(a.hooks(pane), pane, machines.Local))
		a.argvs[pane] = withoutFolder(argv)
		a.addPane(Pane{ID: pane, Title: a.freeTitle(a.localShellName(argv))}, sh, Placement{Instead: id})
	})
}

// slotArgv is what a layout's terminal kept as r runs on this computer,
// started in dir, a folder as Windows names it there: the profile or
// shell kept, or the program kept, or else the shell new terminals
// start. dir also goes to the shell as the folder it starts in, which
// Git Bash and the rest take; a WSL shell takes only a path of its own,
// so it is told the folder in its words on its command line, the
// default shell too when that is a WSL one.
func (a *app) slotArgv(r settings.SavedPane, dir string) []string {
	switch {
	case r.Profile != "":
		if s, ok := a.foundShell(r.Profile); ok {
			return s.Command(dir)
		}
		a.failed("Profile not found", profileName(r.Profile)+" is no longer on this computer. The terminal starts the default shell.")
	case len(r.Command) > 0:
		return slices.Clone(r.Command)
	}
	argv := a.localShell()
	if s, ok := shellfind.Running(a.found, argv); ok && len(argv) > 0 {
		return s.Command(dir)
	}
	return argv
}

// profileName is a profile's or a shell's ID as the user reads it.
func profileName(id string) string {
	for _, prefix := range []string{"profile:", "wsl:"} {
		id = strings.TrimPrefix(id, prefix)
	}
	return id
}

// errClosed is why nothing starts on a machine whose connection closed
// on the way.
var errClosed = errors.New("the connection has closed")

// startFarSlot starts a terminal on server m, connected, in waiting pane
// id: the profile kept, or the account's own shell, in the folder kept.
// A profile no longer on the server starts the server's default, and a
// folder no longer there the default folder, each said.
func (a *app) startFarSlot(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) {
	name := a.machines.Name(m)
	title := "Couldn't open a shell on " + name
	run := r.Profile
	switch _, known := a.farProfile(m, run); {
	case run == LoginShell, known:
	case run == "":
		run = a.farDefault(m)
	default:
		a.failed("Profile not found", profileName(run)+" is no longer saved on "+name+". The terminal starts the default shell.")
		run = a.farDefault(m)
	}
	p, _ := a.farProfile(m, run)
	login := len(p.Command) == 0
	a.farFolder(m, r.Folder, "The terminal starts in the default folder.", still, func(dir string, checked, posix bool) {
		if dir != "" && login && !(checked && posix) {
			// The account's own shell starts where the server starts it,
			// unless a POSIX shell is known to be there to change folder
			// first.
			a.failed("Couldn't restore the folder", dir+"\n\nThe terminal on "+name+" starts in the default folder.")
			dir = ""
		}
		conn, ok, err := a.connOf(m)
		if err == nil && !ok {
			err = errClosed
		}
		if err != nil {
			a.failed(title, reason(err))
			fail(title, err, true)
			return
		}
		a.next++
		a.startFar(m, conn, run, dir, "p"+strconv.Itoa(a.next), Placement{Instead: id}, still, func(_ string, err error) {
			if err != nil {
				// The start said it in a toast already.
				fail(title, err, true)
			}
		})
	})
}

// startWindowSlot opens a terminal or a command kept as r in waiting pane
// id, through the kakel window it ran through, connected: on the
// window's own machine, m being the window, or on the machine beyond it
// m names. The window opens it anew, in a pane of its own that this one
// watches, as New Terminal there opens one, running the shell new
// terminals start there: which one ran is the window's own, and is not
// kept. A terminal on the window's machine starts in the folder kept,
// one on a machine beyond starts where that machine starts it, which is
// said, and a command runs in its folder wherever it runs.
func (a *app) startWindowSlot(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) {
	if a.reattach(id, m, r, still, fail) {
		return
	}
	a.startWindowNew(id, m, r, still, fail)
}

// slotCalled is what a pane through a window, kept as r and on m, is
// called until its shell names it: as one opened there is.
func (a *app) slotCalled(m machines.ID, r settings.SavedPane) string {
	if r.Kind == settings.LayoutCommand {
		return joinLine(splitLine(r.Line))
	}
	if window, key, far := m.Far(); far {
		return a.machines.FarName(window, key)
	}
	return a.machines.Name(m)
}

// startWindowNew is startWindowSlot asking the window for a new pane.
func (a *app) startWindowNew(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) {
	window, key := dialOf(m), ""
	if _, k, far := m.Far(); far {
		key = k
	}
	name := a.machines.Name(m)
	cmd := command{dir: r.Folder}
	title := "Couldn't open a shell on " + name
	instead := "The terminal starts in the default folder."
	called := a.slotCalled(m, r)
	if r.Kind == settings.LayoutCommand {
		cmd.argv = splitLine(r.Line)
		title = "Couldn't run " + called + " on " + name
		instead = "The command runs in the default folder."
	} else if key != "" && cmd.dir != "" {
		a.failed("Couldn't restore the folder", cmd.dir+"\n\nThe terminal on "+name+" starts in the default folder.")
		cmd.dir = ""
	}
	a.next++
	pane := "p" + strconv.Itoa(a.next)
	if len(cmd.argv) > 0 {
		// To run again, as any command pane is.
		a.commands[pane] = cmd
	}
	how := throughHow{in: true, instead: instead, still: still, landed: a.landedFor(a.slots[id], r)}
	err := a.openThroughHow(window, key, cmd, pane, a.freeTitle(called), Placement{Instead: id}, how, func(_ string, err error) {
		if err != nil {
			// The start said it in a toast already.
			fail(title, err, true)
		}
	})
	if err != nil {
		a.failed(title, reason(err))
		fail(title, err, true)
	}
}

// startCommandSlot runs the command kept in waiting pane id, on m, in
// the folder kept, or in the default folder when that is no longer
// there, said.
func (a *app) startCommandSlot(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) {
	line := joinLine(splitLine(r.Line))
	title := "Couldn't run " + line
	const instead = "The command runs in the default folder."
	run := func(dir string) {
		err := a.runCommandWith(RunCommand{Machine: m, Line: r.Line, Dir: dir, Instead: id}, still, func(_ string, err error) {
			if err != nil {
				// The start said it in a toast already.
				fail(title, err, true)
			}
		})
		if err != nil {
			a.failed(title, reason(err))
			fail(title, err, true)
		}
	}
	if m == machines.Local {
		a.folderThere(r.Folder, instead, folderHere, still, run)
		return
	}
	// A folder that could not be looked at goes on the command line as
	// it is: one not there stops the command rather than running it
	// elsewhere.
	a.farFolder(m, r.Folder, instead, still, func(dir string, _, _ bool) { run(dir) })
}

// startFilesSlot opens a file manager on m in waiting pane id, at the
// folder kept, or at the machine's default folder when that is no longer
// there, said.
func (a *app) startFilesSlot(id string, m machines.ID, r settings.SavedPane, still func() bool, fail failure) {
	title := "Couldn't open the files on " + a.machines.Name(m)
	if m == machines.Local {
		title = "Couldn't open the files"
	}
	const instead = "The files open at the default folder."
	at := Placement{Instead: id}
	failed := func(err error) {
		if err == nil {
			err = errClosed
		}
		a.failed(title, reason(err))
		fail(title, err, true)
	}
	if a.files == nil {
		failed(errNoFileManager)
		return
	}
	if m == machines.Local {
		a.folderThere(r.Folder, instead, folderHere, still, func(dir string) {
			if err := a.filePaneOn(m, dir, at, true); err != nil {
				failed(err)
			}
		})
		return
	}
	err := a.withFilesHow(m, func(f vfs.FS) {
		if !still() {
			return
		}
		a.folderThere(r.Folder, instead, farThere(f), still, func(dir string) {
			if err := a.newFilePane(m, a.fmFor(m, f), a.startFolder(m, dir), at); err != nil {
				failed(err)
			}
		})
	}, func(err error) {
		if still() {
			failed(err)
		}
	}, true)
	if err != nil {
		failed(err)
	}
}

// farThere is dir when f has it, and otherwise "".
func farThere(f vfs.FS) func(dir string) string {
	return func(dir string) string {
		if _, err := f.Stat(dir); err != nil {
			return ""
		}
		return dir
	}
}

// folderThere hands then dir once there says it is there, or "" for the
// default folder when it is not, which is said, with instead, the
// sentence saying where the pane opens instead: a pane that opens
// somewhere else than it was kept at must not do so in silence. there
// runs on a goroutine of its own, as a folder on a share that has gone
// would hold up the program; then runs on the program's, and only while
// still says the pane waits for it.
func (a *app) folderThere(dir, instead string, there func(string) string, still func() bool, then func(dir string)) {
	if dir == "" {
		then("")
		return
	}
	go func() {
		found := there(dir)
		a.later(func() {
			if !still() {
				return
			}
			if found == "" {
				a.failed("Folder not found", dir+"\n\n"+instead)
			}
			then(found)
		})
	}()
}

// farFolder is folderThere for a folder on server m, connected, looked
// at over its files. then also hears whether it was looked at, and
// whether the server has a POSIX shell at /bin/sh, which can change to
// the folder before the account's own shell starts. A server whose files
// cannot be opened hands then dir as it was kept, unchecked, which is
// written in the Window Log.
func (a *app) farFolder(m machines.ID, dir, instead string, still func() bool, then func(dir string, checked, posix bool)) {
	if dir == "" {
		then("", true, false)
		return
	}
	unchecked := func(err error) {
		if !still() {
			return
		}
		if err == nil {
			err = errClosed
		}
		log.Printf("couldn't look for %s on %s: %v", dir, a.machines.Name(m), err)
		then(dir, false, false)
	}
	err := a.withFilesHow(m, func(f vfs.FS) {
		if !still() {
			return
		}
		go func() {
			found := farThere(f)(dir)
			_, shErr := f.Stat("/bin/sh")
			a.later(func() {
				if !still() {
					return
				}
				if found == "" {
					a.failed("Folder not found", dir+"\n\n"+instead)
				}
				then(found, true, shErr == nil)
			})
		}()
	}, unchecked, true)
	if err != nil {
		unchecked(err)
	}
}

// intoSlot puts pane p, started, in the place of waiting pane id, which
// goes. Its window stays where it is among the others, since the user
// may have gone to another while the pane started; and p has the
// keyboard there only where the waiting pane had it. A name the user
// gave the pane, in the layout or while it waited, is put back.
func (a *app) intoSlot(p Pane, id string) {
	i := slices.IndexFunc(a.st.Panes, func(q Pane) bool { return q.ID == id })
	if q := a.st.Panes[i]; q.Named {
		// Named in the layout, or by the user while it waited.
		p.shell, p.Title, p.Named = p.Title, q.Title, true
	}
	if p.Kind == KindTerminal && !p.Command {
		p.ShellKind = a.shellKindOf(p)
	}
	w := a.ownerOf(id)
	had := w != nil && a.focusIn(w) == id
	// In the waiting pane's place in the list, and in the layout, where
	// its tab stays in front or behind as it was.
	a.st.Panes[i] = p
	if n := a.layout.Node(id); n != nil {
		n.ID = p.ID
		if g := a.layout.GroupOf(p.ID); g != nil && g.Front == id {
			g.Front = p.ID
		}
	}
	a.forgetSlot(id)
	log.Printf("opened %s", a.paneForLog(p))
	a.stayEmpty = false
	if had {
		a.setFocusIn(w, p.ID)
	}
}

// forgetSlot lets waiting pane id's slot go.
func (a *app) forgetSlot(id string) {
	delete(a.slots, id)
	delete(a.farHost, id)
	if _, ok := a.st.Waits[id]; ok {
		m := maps.Clone(a.st.Waits)
		delete(m, id)
		a.st.Waits = m
	}
}

// slotClosed lets waiting pane id go as it closes, before what it waited
// for has started: what starts for it later is closed as it comes. A
// connection a layout started for it, and no other waiting pane still
// waits for, is given up, and with it any question it was asking, unless
// something other than a layout waits on it too.
func (a *app) slotClosed(id string) {
	s := a.slots[id]
	if s == nil {
		return
	}
	a.forgetSlot(id)
	m := dialOf(s.machine)
	if m == machines.Local || !a.layoutDials[m] || a.machines.Get(m).Dialing == nil {
		return
	}
	for _, o := range a.slots {
		if dialOf(o.machine) == m && !o.failed {
			return
		}
	}
	if len(a.machines.Get(m).Waiters) > a.layoutJoins[m] {
		// Something else waits on the dial too, as files opened there.
		return
	}
	a.giveUp(m)
}

// retryWaiting starts failed waiting pane id again, with every other
// failed pane of its window on the same machine, which share the one
// connection. A server removed since is looked for again.
func (a *app) retryWaiting(id string) {
	s := a.slots[id]
	if s == nil || !s.failed {
		return
	}
	w := a.ownerOf(id)
	var ids []string
	for _, p := range a.st.Panes {
		o := a.slots[p.ID]
		if o != nil && o.failed && a.ownerOf(p.ID) == w && o.recipe.Host == s.recipe.Host && o.recipe.HostID == s.recipe.HostID {
			ids = append(ids, p.ID)
		}
	}
	for _, id := range ids {
		o := a.slots[id]
		o.failed = false
		a.setWait(id, Waiting{Doing: a.doingOf(o.recipe, o.machine)})
	}
	a.startSlots(ids)
}

// openAtStart opens the layouts kept to open as kakel starts, each in a
// window of its own, and reports whether any is on its way. A layout
// that cannot be opened is said in the first window that opens, as the
// window kakel starts with is never shown; with none opening, the
// caller is handed what to say, as unopened.
func (a *app) openAtStart() (opened bool, unopened []string) {
	if a.settings == nil {
		return false, nil
	}
	var plans []layoutPlan
	var places []*settings.WindowPlace
	for _, l := range a.settings.Layouts() {
		if !l.AtStart {
			continue
		}
		plan, err := a.planLayout(l)
		if err != nil {
			unopened = append(unopened, l.Name+": "+err.Error()+".")
			continue
		}
		plans = append(plans, plan)
		places = append(places, l.Place)
	}
	if len(plans) == 0 {
		return false, unopened
	}
	for i, plan := range plans {
		a.newWindowAt(placementOf(places[i]), func() {
			a.fillLayout(plan, a.cur)
			if i == 0 {
				a.sayUnopened(unopened)
			}
		})
	}
	return true, nil
}

// sayUnopened says each layout in unopened, with why, could not be
// opened at start.
func (a *app) sayUnopened(unopened []string) {
	for _, why := range unopened {
		a.failed("Couldn't open the layout", why)
	}
}
