package view

import (
	"image/color"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/settings"
	"github.com/marrasen/kakel/words"
)

// A machine's pane holds its connection too: its log, the tunnels and
// SOCKS proxies through it, open and saved, each heading with a + that
// adds one, and at the bottom of the sidebar the switch that connects
// and disconnects it.

// Keys of the connection's rows.
const (
	mpLog       = app.MachineRowLog
	mpAddTunnel = app.MachineRowAddTunnel
	mpAddSOCKS  = app.MachineRowAddSOCKS
	mpTunnels   = "head:tunnels"
	mpSOCKS     = "head:socks"
	// mpTunnel starts a tunnel's key, by what it is, so its row stays
	// the one picked as it opens and closes, and is saved or not.
	mpTunnel = "tunnel:"
)

// tunnelKey is the key of the row of a tunnel saved as kind, listening
// on listen and reaching target.
func tunnelKey(kind, listen, target string) string {
	return mpTunnel + kind + "\x00" + listen + "\x00" + target
}

// mpTunnelOf is a tunnel as a machine's pane lists it: one open, one
// saved, or one open that is saved too.
type mpTunnelOf struct {
	open  *app.Tunnel
	saved *settings.SavedTunnel
	socks bool
}

// as is the tunnel as the saved list writes it.
func (t mpTunnelOf) as() settings.SavedTunnel {
	if t.open != nil {
		return t.open.As
	}
	return *t.saved
}

// title is what the tunnel's row says: where it listens, and for a
// tunnel where it goes.
func (t mpTunnelOf) title() string {
	as := t.as()
	if t.socks || as.Target == "" {
		return as.Listen
	}
	return as.Listen + " → " + as.Target
}

// live reports whether the tunnel is open and forwarding.
func (t mpTunnelOf) live() bool { return t.open != nil && t.open.Live }

// tunnelRows are a heading, Tunnels or with socks SOCKS proxies, with
// the + that adds one, then those open over the machine, then those
// saved for it and not open.
func (mp *machinePane) tunnelRows(socks bool) []mpItem {
	head := mpItem{key: mpTunnels, title: "Tunnels", heading: true, plus: mpAddTunnel}
	ic := icon.Cable
	if socks {
		head = mpItem{key: mpSOCKS, title: "SOCKS proxies", heading: true, plus: mpAddSOCKS}
		ic = icon.Network
	}
	out := []mpItem{head}
	isSOCKS := func(kind string) bool { return kind == remote.DynamicForward.String() }
	var open []settings.SavedTunnel
	for i, t := range mp.st.Tunnels {
		if t.Machine != mp.machine || isSOCKS(t.As.Kind) != socks {
			continue
		}
		of := mpTunnelOf{open: &mp.st.Tunnels[i], socks: socks}
		if j := slices.IndexFunc(mp.st.SavedTunnels, t.As.Same); j >= 0 {
			of.saved = &mp.st.SavedTunnels[j]
		}
		open = append(open, t.As)
		out = append(out, mpItem{key: tunnelKey(t.As.Kind, t.As.Listen, t.As.Target), title: of.title(), icon: ic, tunnel: &of})
	}
	for i, t := range mp.st.SavedTunnels {
		if isSOCKS(t.Kind) != socks || !mp.w.keptFor(t.Host, t.HostID, mp.machine) || slices.ContainsFunc(open, t.Same) {
			continue
		}
		of := mpTunnelOf{saved: &mp.st.SavedTunnels[i], socks: socks}
		out = append(out, mpItem{key: tunnelKey(t.Kind, t.Listen, t.Target), title: of.title(), icon: ic, tunnel: &of})
	}
	return out
}

// tunnelSide is the right side for a tunnel's row: how it is doing,
// with a graph of its traffic while it is open; its fields, to change
// and save; and the buttons that open or close it, or remove it.
func (mp *machinePane) tunnelSide(t mpTunnelOf) gunim.Node {
	as := t.as()
	var parts []gunim.Node
	if t.open != nil {
		// What it is doing, said in place as it changes. Each page has
		// its own: the page before still shows its line as it fades.
		mp.tunnelNow = faintLabel(t.now())
		parts = append(parts, mp.tunnelNow)
	}
	if t.live() && t.open.Meter != nil {
		parts = append(parts, trafficGraph(t.open.Meter))
	}
	var actions []gunim.Node
	switch {
	case t.live():
		closeIt := widget.NewButton("Close")
		closeIt.OnClick = widget.Sends(app.CloseTunnel{ID: t.open.ID})
		actions = append(actions, closeIt)
	case t.saved != nil:
		open := widget.NewButton("Open")
		open.OnClick = widget.Sends(app.OpenSavedTunnel{Saved: as})
		open.Disabled = mp.card.state != cardConnected
		actions = append(actions, open)
	}
	if t.saved == nil {
		// Open, and not saved: what it is, and Save.
		form := widget.NewForm().Add("Listen on", widget.NewLabel(as.Listen))
		if as.Target != "" {
			form.Add("Forward to", widget.NewLabel(as.Target))
		}
		save := widget.NewButton("Save")
		save.Kind = widget.ButtonPrimary
		save.OnClick = widget.Sends(app.SaveTunnel{Saved: as})
		parts = append(parts, widget.NewCard(form), buttons(append([]gunim.Node{save}, actions...)...))
		col := widget.Column(append([]gunim.Node{heading(t.title())}, parts...)...)
		col.Cross, col.Gap = widget.CrossStretch, mpGap
		return widget.NewSized(col, 640, 0)
	}
	// Saved: its fields, to change and save.
	saved := *t.saved
	form, fields, tunnelOf, auto, way := mp.w.tunnelFields(mp.machine, t.socks, &saved)
	save := widget.NewButton("Save")
	save.Kind = widget.ButtonPrimary
	problem := faintLabel("")
	check := func(u *gunim.UI) {
		now := tunnelOf()
		err := now.Validate()
		problem.Text = ""
		if err != nil {
			problem.Text = words.UpperFirst(err.Error())
		}
		save.Disabled = err != nil || now.Kind.String() == saved.Kind && now.Listen == saved.Listen &&
			now.Target == saved.Target && auto.Checked() == saved.Auto
		if u != nil {
			u.Invalidate()
		}
	}
	for _, f := range fields {
		f.OnChange = func(_ string, u *gunim.UI) gunim.Intent { check(u); return nil }
	}
	way.OnChange = func(_ int, u *gunim.UI) gunim.Intent { check(u); return nil }
	auto.OnChange = func(_ bool, u *gunim.UI) gunim.Intent { check(u); return nil }
	check(nil)
	save.OnClick = func(*gunim.UI) gunim.Intent {
		now := tunnelOf()
		if now.Validate() != nil {
			return nil
		}
		// Its row is the one picked as it is now.
		mp.wantSel = tunnelKey(now.Kind.String(), now.Listen, now.Target)
		return app.EditTunnel{Was: saved, Machine: mp.machine, Tunnel: now, Auto: auto.Checked()}
	}
	remove := widget.NewButton("Remove…")
	remove.OnClick = func(u *gunim.UI) gunim.Intent { mp.confirmForget(mpSavedOf{tunnel: &saved}, u); return nil }
	actions = append([]gunim.Node{save}, append(actions, remove)...)
	parts = append(parts, widget.NewCard(form), problem, buttons(actions...))
	col := widget.Column(append([]gunim.Node{heading(t.title())}, parts...)...)
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// trafficGraph is a graph of what goes through a tunnel each second,
// both ways, saying the speed now.
func trafficGraph(m *meter.Meter) gunim.Node {
	var rate meter.Rate
	g := widget.NewLiveGraph(500*time.Millisecond, 120)
	g.Floor = trafficFloor
	g.Sample = func() float64 {
		rate.Sample(m, time.Now())
		past := rate.Past()
		if len(past) == 0 {
			return 0
		}
		return float64(past[len(past)-1])
	}
	g.Label = func(v float64) string { return words.Size(int64(v)) + "/s" }
	g.SetRunning(true)
	return g
}

// now is what an open tunnel is doing, for its page.
func (t mpTunnelOf) now() string {
	switch {
	case t.open == nil:
		return ""
	case !t.live() && t.open.Note == "":
		return "Stopped"
	case !t.live():
		return words.UpperFirst(t.open.Note)
	case t.open.Note == "":
		return "Open"
	}
	return "Open · " + t.open.Note
}

// meterOf is what counts a tunnel's traffic, nil for one not open.
func meterOf(t mpTunnelOf) *meter.Meter {
	if t.open == nil {
		return nil
	}
	return t.open.Meter
}

// addTunnelSide is the right side for the + of Tunnels, or with socks
// of SOCKS proxies: the fields, and Save. One saved to connect
// automatically opens at once, with the machine connected.
func (mp *machinePane) addTunnelSide(socks bool) gunim.Node {
	form, fields, tunnelOf, auto, way := mp.w.tunnelFields(mp.machine, socks, nil)
	save := widget.NewButton("Save")
	save.Kind = widget.ButtonPrimary
	problem := faintLabel("")
	check := func(u *gunim.UI) {
		problem.Text = ""
		err := tunnelOf().Validate()
		save.Disabled = err != nil
		if err != nil && strings.TrimSpace(fields[0].Text()) != "" {
			problem.Text = words.UpperFirst(err.Error())
		}
		if u != nil {
			u.Invalidate()
		}
	}
	for _, f := range fields {
		f.OnChange = func(_ string, u *gunim.UI) gunim.Intent { check(u); return nil }
	}
	way.OnChange = func(_ int, u *gunim.UI) gunim.Intent { check(u); return nil }
	check(nil)
	save.OnClick = func(*gunim.UI) gunim.Intent {
		t := tunnelOf()
		if t.Validate() != nil {
			return nil
		}
		// Its row is picked once it comes.
		mp.wantSel = tunnelKey(t.Kind.String(), t.Listen, t.Target)
		return app.AddTunnel{Machine: mp.machine, Tunnel: t, Auto: auto.Checked()}
	}
	title, note := "Add Tunnel", "A port that forwards over the connection to "+mp.w.nameOf(mp.machine)+"."
	if socks {
		title, note = "Add SOCKS Proxy", "A SOCKS proxy here: programs set to use it reach the network from "+mp.w.nameOf(mp.machine)+"."
	}
	col := widget.Column(heading(title), faintLabel(note), widget.NewCard(form), problem, buttons(save))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// logSide is the right side for the connection log's row.
func (mp *machinePane) logSide(u *gunim.UI) gunim.Node {
	m := mp.machine
	lines := mp.st.Logs[m]
	lv := widget.NewLogView()
	lv.Plain = true
	lv.OnSeen = func(end int64, u *gunim.UI) gunim.Intent {
		mp.w.sawLog(m, end)
		// Read to its newest line: the row's count of problems goes.
		if r, ok := widget.RowOf[*mpRow](mp.list, widget.Key(mpLog)); ok {
			r.badge.SetHidden(unseenBad(mp.st.Logs[m], end) == 0, u)
		}
		return nil
	}
	mp.logView, mp.logShown = lv, -1
	mp.showLog(u)
	copyIt := widget.NewIconButton(icon.Copy, "Copy the log")
	copyIt.OnClick = func(u *gunim.UI) gunim.Intent {
		if lines != nil {
			lines, _ := lines.Snapshot()
			for i, l := range lines {
				lines[i] = plainLine(l)
			}
			u.SetClipboard(strings.Join(lines, "\n") + "\n")
		}
		return nil
	}
	copyIt.Disabled = lines == nil
	gap := widget.NewSpacer()
	head := widget.Row(heading("Connection log"), gap, copyIt).Grow(gap, 1)
	head.Cross = widget.CrossCenter
	var body gunim.Node = lv
	if lines == nil {
		note := "No connection log yet."
		if window, _, far := m.Far(); far {
			// Its log is the window's, shown in a pane of that window's.
			note = "Kept by " + mp.w.nameOf(window) + "."
			open := widget.NewButton("Show")
			open.OnClick = widget.Sends(app.ShowLog{Machine: m})
			col := widget.Column(head, faintLabel(note), buttons(open))
			col.Cross, col.Gap = widget.CrossStretch, mpGap
			return col
		}
		body = faintLabel(note)
	}
	// The log is the page: all the room under its heading.
	col := widget.Column(head, body).Grow(body, 1)
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return col
}

// The colours a log's lines are written in, as logLine writes them.
const (
	logBad  = "\x1b[31m"
	logWell = "\x1b[32m"
)

// plainLine is a log line without its colour.
func plainLine(l string) string {
	for _, code := range []string{logBad, logWell, "\x1b[0m"} {
		l = strings.ReplaceAll(l, code, "")
	}
	return l
}

// logLines are a log's last lines as gunim's LogView takes them: each
// line's time apart, and its colour from how it went.
func logLines(l *logs.Lines) ([]widget.LogLine, int64) {
	lines, end := l.Since(0, logs.DefaultKeep)
	out := make([]widget.LogLine, len(lines))
	for i, line := range lines {
		var ink theme.Token[color.NRGBA]
		switch {
		case strings.Contains(line, logBad):
			ink = widget.DialogDangerInk
		case strings.Contains(line, logWell):
			ink = widget.ToastSuccessInk
		}
		line = plainLine(line)
		out[i] = widget.LogLine{Text: line, Ink: ink}
		if stamp, rest, ok := strings.Cut(line, "  "); ok && len(stamp) == 8 {
			out[i].Lead, out[i].Text = stamp, rest
		}
	}
	return out, end
}

// showLog gives the log page the machine's log as far as it has got,
// when it has got further than the page shows.
func (mp *machinePane) showLog(u *gunim.UI) {
	l := mp.st.Logs[mp.machine]
	if mp.logView == nil || l == nil || mp.st.LogEnds[mp.machine] == mp.logShown {
		return
	}
	lines, end := logLines(l)
	mp.logView.SetLines(lines, end, u)
	mp.logShown = end
}

// unseenBad counts the lines gone badly in m's log since seen, its end
// when the log was last looked at.
func unseenBad(l *logs.Lines, seen int64) int {
	if l == nil {
		return 0
	}
	lines, _ := l.Since(seen, logs.DefaultKeep)
	n := 0
	for _, line := range lines {
		if strings.Contains(line, logBad) {
			n++
		}
	}
	return n
}

// sawLog notes m's log as read to end, so its row counts no problems
// before it.
func (w *Window) sawLog(m machines.ID, end int64) {
	if w.logSeen == nil {
		w.logSeen = map[machines.ID]int64{}
	}
	w.logSeen[m] = max(w.logSeen[m], end)
}

// overview is the line at the top of a remote machine's page: how its
// connection is, and what is open on it.
func (mp *machinePane) overview(now time.Time) string {
	m := mp.machine
	info := mp.w.cardFor(m, mp.w.nameOf(m))
	words, _ := pillFor(info, now, nil)
	switch {
	case info.state == cardConnected && info.silent:
		words = "Connected, not answering"
	case info.state == cardConnected:
		words = "Connected"
		if since := now.Sub(info.since); !info.since.IsZero() && since >= time.Minute {
			words += " for " + connectedFor(since)
		}
		if info.rtt > 0 {
			words += ", round trip " + roundTrip(info.rtt)
		}
	default:
		words = strings.ToUpper(words[:1]) + words[1:]
	}
	if open := mp.w.closingOn(m).open(); open != "" {
		words += " · " + open + " open"
	}
	return words
}
