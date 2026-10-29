package view

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/logs"
	"github.com/marrasen/kakel/look"
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
	mpLog        = app.MachineRowLog
	mpAddTunnel  = app.MachineRowAddTunnel
	mpAddSOCKS   = app.MachineRowAddSOCKS
	mpTunnels    = "head:tunnels"
	mpSOCKS      = "head:socks"
	mpTunnelOpen = "tunnel:open:"
	mpTunnelKept = "tunnel:saved:"
)

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
		out = append(out, mpItem{key: mpTunnelOpen + t.ID, title: of.title(), icon: ic, tunnel: &of})
	}
	for i, t := range mp.st.SavedTunnels {
		if isSOCKS(t.Kind) != socks || !mp.w.keptFor(t.Host, t.HostID, mp.machine) || slices.ContainsFunc(open, t.Same) {
			continue
		}
		of := mpTunnelOf{saved: &mp.st.SavedTunnels[i], socks: socks}
		out = append(out, mpItem{key: mpTunnelKept + fmt.Sprintf("%#v", t), title: of.title(), icon: ic, tunnel: &of})
	}
	return out
}

// tunnelSide is the right side for a tunnel's row: what it is, how it
// is doing, and the buttons that close or open it, save it or take it
// off the saved list.
func (mp *machinePane) tunnelSide(t mpTunnelOf) gunim.Node {
	as := t.as()
	called := mp.w.nameOf(mp.machine)
	form := widget.NewForm()
	kind := map[string]string{
		"local":  "A port here, reaching through " + called,
		"remote": "A port on " + called + ", reaching back here",
		"socks":  "A SOCKS proxy here, its connections going out from " + called,
	}[as.Kind]
	if kind == "" {
		kind = as.Kind
	}
	form.Add("Kind", widget.NewLabel(kind))
	form.Add("Listen on", widget.NewLabel(as.Listen))
	if as.Target != "" {
		form.Add("Forward to", widget.NewLabel(as.Target))
	}
	var actions []gunim.Node
	if t.open != nil {
		// What it is doing, said in place as it changes.
		form.Add("Now", mp.tunnelNow)
	}
	switch {
	case t.live():
		traffic := widget.NewButton("Show")
		traffic.Tooltip = "Show its traffic in a pane"
		traffic.OnClick = widget.Sends(app.ShowTunnel{ID: t.open.ID})
		form.Add("Traffic", buttons(traffic))
		closeIt := widget.NewButton("Close")
		closeIt.OnClick = widget.Sends(app.CloseTunnel{ID: t.open.ID})
		actions = append(actions, closeIt)
	case t.open != nil:
		open := widget.NewButton("Open")
		open.Kind = widget.ButtonPrimary
		open.OnClick = widget.Sends(app.OpenSavedTunnel{Saved: as})
		closeIt := widget.NewButton("Close")
		closeIt.Tooltip = "Close its row"
		closeIt.OnClick = widget.Sends(app.CloseTunnel{ID: t.open.ID})
		actions = append(actions, open, closeIt)
	default:
		open := widget.NewButton("Open")
		open.Kind = widget.ButtonPrimary
		open.OnClick = widget.Sends(app.OpenSavedTunnel{Saved: as})
		open.Disabled = mp.card.state != cardConnected
		actions = append(actions, open)
	}
	if t.saved != nil {
		saved := *t.saved
		remove := widget.NewButton("Remove…")
		remove.OnClick = func(u *gunim.UI) gunim.Intent { mp.confirmForget(mpSavedOf{tunnel: &saved}, u); return nil }
		actions = append(actions, remove)
	} else {
		save := widget.NewButton("Save")
		save.OnClick = widget.Sends(app.SaveTunnel{Saved: as})
		actions = append(actions, save)
	}
	what := "Saved: it opens again from here."
	if t.saved == nil {
		what = "Open until it is closed."
	}
	col := widget.Column(heading(t.title()), faintLabel(what), widget.NewCard(form), buttons(actions...))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
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
// of SOCKS proxies: the fields, and Open.
func (mp *machinePane) addTunnelSide(socks bool) gunim.Node {
	form, fields, tunnelOf, keep, way := mp.w.tunnelFields(mp.machine, socks)
	keep.SetChecked(true, nil)
	open := widget.NewButton("Open")
	open.Kind = widget.ButtonPrimary
	problem := faintLabel("")
	check := func(u *gunim.UI) {
		problem.Text = ""
		err := tunnelOf().Validate()
		connected := mp.card.state == cardConnected
		open.Disabled = err != nil || !connected
		switch {
		case err != nil && strings.TrimSpace(fields[0].Text()) != "":
			problem.Text = words.UpperFirst(err.Error())
		case !connected:
			problem.Text = "Not connected"
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
	open.OnClick = func(*gunim.UI) gunim.Intent {
		t := tunnelOf()
		if t.Validate() != nil {
			return nil
		}
		mp.addedTunnel, mp.tunnelsBefore = &t, map[string]bool{}
		for _, open := range mp.st.Tunnels {
			mp.tunnelsBefore[open.ID] = true
		}
		return app.OpenTunnel{Machine: mp.machine, Tunnel: t, Keep: keep.Checked()}
	}
	title, note := "Add Tunnel", "A port that forwards over the connection to "+mp.w.nameOf(mp.machine)+"."
	if socks {
		title, note = "Add SOCKS Proxy", "A SOCKS proxy here: programs set to use it reach the network from "+mp.w.nameOf(mp.machine)+"."
	}
	col := widget.Column(heading(title), faintLabel(note), widget.NewCard(form), problem, buttons(open))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 640, 0)
}

// logSide is the right side for the connection log's row.
func (mp *machinePane) logSide() gunim.Node {
	m := mp.machine
	lv := &logView{w: mp.w, m: m, lines: mp.st.Logs[m]}
	copyIt := widget.NewButton("Copy")
	copyIt.OnClick = func(u *gunim.UI) gunim.Intent {
		if lv.lines != nil {
			lines, _ := lv.lines.Snapshot()
			for i, l := range lines {
				lines[i] = plainLine(l)
			}
			u.SetClipboard(strings.Join(lines, "\n") + "\n")
		}
		return nil
	}
	pane := widget.NewButton("Open")
	pane.Tooltip = "Open the log in a pane of its own"
	pane.OnClick = widget.Sends(app.ShowLog{Machine: m})
	_, _, far := m.Far()
	copyIt.Disabled, pane.Disabled = lv.lines == nil, lv.lines == nil && !far
	note := "Newest at the bottom."
	var body gunim.Node = lv
	switch {
	case lv.lines == nil && far:
		window, _, _ := m.Far()
		note = "Kept by " + mp.w.nameOf(window) + ". Open shows it in a pane."
		body = &emptyNode{}
	case lv.lines == nil:
		note = "No connection log yet."
		body = &emptyNode{}
	}
	col := widget.Column(heading("Connection log"), faintLabel(note), body, buttons(copyIt, pane))
	col.Cross, col.Gap = widget.CrossStretch, mpGap
	return widget.NewSized(col, 900, 0)
}

// logView shows a connection log's last lines, newest at the bottom,
// following new ones as they come; the wheel scrolls back, and what it
// was scrolled back to stays put as lines come.
type logView struct {
	w     *Window
	m     machines.ID
	lines *logs.Lines
	// back is how many lines it is scrolled up from the newest, end the
	// log's end as last drawn, and wheel what the wheel has turned short
	// of a line.
	back  int
	end   int64
	wheel float32
}

// logViewHeight is the log's height.
const logViewHeight = 440

// Layout implements [gunim.Node].
func (v *logView) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Constrain(geom.Sz(c.Max.W, logViewHeight))
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

// The log's text: its size, the room a line takes, and the room round
// the lines.
const (
	logText = 12.5
	logStep = logText * 1.55
	logPad  = 12
)

// Paint implements [gunim.Node]: a box of the lines that fit, each
// one's time faint and the rest in its colour.
func (v *logView) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	th := f.Theme
	r := geom.Rect{Max: box.Point()}
	p.RRect(r, 8, paint.Solid(widget.FieldFill.Get(th)))
	p.RRectStroke(r.Inset(geom.Uniform(0.5)), 8, paint.Fill{}, paint.Stroke{Width: 1, Color: widget.DialogBorder.Get(th)})
	if v.lines == nil {
		return
	}
	// New lines are looked for twice a second.
	f.RedrawAt(f.Now.Add(500 * time.Millisecond))
	face := text.GoMono(false, false)
	size, step, pad := float32(logText), float32(logStep), float32(logPad)
	fits := int((box.H - 2*pad) / step)
	// Scrolled back, the lines looked at stay where they are as more
	// come; at the bottom, the newest show.
	if now := v.lines.End(); v.back > 0 && v.end > 0 {
		v.back += int(now - v.end)
	}
	lines, end := v.lines.Since(0, fits+v.back)
	v.end = end
	v.back = min(v.back, max(0, len(lines)-fits))
	last := len(lines) - v.back
	start := max(0, last-fits)
	if v.back == 0 {
		// Read to the end: what is in it has been seen.
		v.w.sawLog(v.m, end)
	}
	y := float32(pad)
	faint := look.Faint.Get(th)
	for _, l := range lines[start:last] {
		ink := widget.Ink.Get(th)
		switch {
		case strings.Contains(l, logBad):
			ink = widget.DialogDangerInk.Get(th)
		case strings.Contains(l, logWell):
			ink = widget.ToastSuccessInk.Get(th)
		}
		l = plainLine(l)
		x := float32(pad)
		if stamp, rest, ok := strings.Cut(l, "  "); ok && len(stamp) == 8 {
			run := face.Shape(stamp, size)
			run.Paint(p, geom.Pt(x, y), faint)
			x += run.Advance + 12
			l = rest
		}
		face.Shape(l, size).PaintFaded(p, geom.Pt(x, y), ink, box.W-pad-x, widget.TextFade.Get(th))
		y += step
	}
}

// Handle implements [gunim.Handler]: the wheel scrolls back through the
// log, and down to the newest, which it then follows.
func (v *logView) Handle(e input.Event, u *gunim.UI) bool {
	s, ok := e.(input.Scroll)
	if !ok || v.lines == nil || s.Mods != 0 {
		return false
	}
	n := s.Notches.Y
	if n == 0 {
		n = s.Delta.Y / 40
	}
	// Up a notch is back three lines. What falls short of a line is
	// kept for the next turn, as a touchpad turns in fractions.
	v.wheel += n * 3
	whole := int(v.wheel)
	v.wheel -= float32(whole)
	held := v.lines.End()
	switch {
	case whole > 0 && int64(v.back) >= held:
		// At the top: the page scrolls instead.
		return false
	case whole < 0 && v.back == 0:
		return false
	}
	v.back = max(0, v.back+whole)
	u.Invalidate()
	return true
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

// liveMark is a tunnel row's mark: a dot, green while it forwards and
// breathing while bytes go past, faint while it is only saved; and the
// last seconds of its traffic.
type liveMark struct {
	t    mpTunnelOf
	rate meter.Rate
}

// Layout implements [gunim.Node].
func (m *liveMark) Layout(c gunim.Constraints, _ gunim.Frame, _ gunim.Children) geom.Size {
	return c.Constrain(geom.Sz(graphRoom+12, 16))
}

// Paint implements [gunim.Node].
func (m *liveMark) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	state := meter.Closed
	counted := m.t.live() && m.t.open.Meter != nil
	switch {
	case counted:
		state = m.t.open.Meter.StateAt(f.Now)
		f.RedrawAt(f.Now.Add(250 * time.Millisecond))
	case m.t.live():
		// Open, with nothing counting what moves through it here.
		state = meter.Settled
	}
	colour := markColour(state, f.Now, f)
	if m.t.open != nil && !m.t.open.Live {
		colour = widget.DialogDangerInk.Get(f.Theme)
	}
	mid := box.H / 2
	p.RRect(geom.Rc(box.W-7, mid-3.5, 7, 7), 3.5, paint.Solid(colour))
	if !counted {
		return
	}
	m.rate.Sample(m.t.open.Meter, f.Now)
	past := m.rate.Past()
	if !slices.ContainsFunc(past, func(s uint64) bool { return s > 0 }) {
		return
	}
	const most = 8
	bars := meter.Bars(past, most)
	c := markThere
	c.A = 0xc0
	w := float32(graphRoom) / float32(meter.Samples)
	right := box.W - 14
	for i, b := range bars {
		h := (box.H - 2) * float32(b) / most
		x := right - float32(len(bars)-i)*w
		p.RRect(geom.Rc(x, box.H-1-h, max(w-1, 1), h), 0, paint.Solid(c))
	}
}

// countBadge is a small count, in red, such as the problems in a log
// not read yet. It grows and fades in as the count comes, and out as it
// goes, keeping its last words while it does.
type countBadge struct {
	anim.Group
	n     int
	said  string
	shown *anim.Float
}

func newCountBadge() *countBadge {
	b := &countBadge{shown: anim.NewFloat(0)}
	b.Add(b.shown)
	return b
}

// Layout implements [gunim.Node].
func (b *countBadge) Layout(c gunim.Constraints, f gunim.Frame, _ gunim.Children) geom.Size {
	if b.n > 0 {
		b.said = words.ManyOf(b.n, "problem", "problems")
		b.shown.Animate(1, widget.Quick.Get(f.Theme))
	} else {
		b.shown.Animate(0, widget.Settle.Get(f.Theme))
	}
	t := min(max(b.shown.Value(), 0), 1)
	if t < 0.001 || b.said == "" {
		return geom.Size{}
	}
	run := text.Default().Shape(b.said, smallText.Get(f.Theme))
	return c.Constrain(geom.Sz((run.Advance+14)*t, 18))
}

// Paint implements [gunim.Node].
func (b *countBadge) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, _ gunim.Children) {
	t := min(max(b.shown.Value(), 0), 1)
	if t < 0.001 || box.W == 0 {
		return
	}
	defer p.Layer(paint.LayerOpts{Bounds: geom.Rect{Max: box.Point()}, Opacity: t})()
	ink := widget.DialogDangerInk.Get(f.Theme)
	fill := ink
	fill.A = 0x2c
	p.RRect(geom.Rect{Max: box.Point()}, box.H/2, paint.Solid(fill))
	run := text.Default().Shape(b.said, smallText.Get(f.Theme))
	run.PaintFaded(p, geom.Pt(7, box.H/2-run.Height()/2), ink, box.W-7, widget.TextFade.Get(f.Theme))
}

// sideFoot is the bottom of a machine's sidebar: a line, and the
// machine's status switch under it, stretched across. This computer's
// pane has none.
type sideFoot struct {
	chip *connChip
	mp   *machinePane
}

// Children implements [gunim.Composite].
func (s *sideFoot) Children() []gunim.Node { return []gunim.Node{s.chip} }

// Layout implements [gunim.Node].
func (s *sideFoot) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	if s.mp.machine == machines.Local || s.mp.draft {
		k.Layout(gunim.Constraints{})
		return geom.Size{}
	}
	const pad = 10
	w := max(0, c.Max.W-2*pad)
	cs := k.Layout(gunim.Constraints{Min: geom.Sz(w, connHeight+4), Max: geom.Sz(w, connHeight+4)})
	k.Place(geom.Pt(pad, pad+1))
	return geom.Sz(c.Max.W, cs.H+2*pad+1)
}

// Paint implements [gunim.Node].
func (s *sideFoot) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	if box.H == 0 {
		return
	}
	p.RRect(geom.Rc(0, 0, box.W, 1), 0, paint.Solid(widget.PanelBorder.Get(f.Theme)))
	kids.At(0).Paint(p)
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
