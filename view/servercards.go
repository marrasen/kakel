package view

import (
	"hash/fnv"
	"image/color"
	"os"
	"os/user"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/remote"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/anim"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/text"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"
)

// The Machines pane shows each machine as a card: a badge in its own
// colour, its name, how its connection is doing, who and where it is,
// buttons for what is opened there most, and under them what is open
// on it in every window. The cards stand side by side as the pane is
// wide, under three headings: this computer, the machines connected,
// and the servers saved.
//
// A card's header is the machine's heading row, drawn as a card's: its
// ⋯ opens the machine's menu, as the heading's plus did.

// cardState is how a card's machine is doing, for its pill.
type cardState int

const (
	cardOff cardState = iota
	cardLocal
	cardConnected
	cardDialing
	cardLost
)

// The sections the cards stand in, in order.
const (
	sectionHere = iota
	sectionConnected
	sectionSaved
	sections
)

var sectionTitles = [sections]string{"THIS COMPUTER", "CONNECTED", "SAVED"}

// cardInfo is what a card's header shows of its machine.
type cardInfo struct {
	name, sub string
	state     cardState
	// since is when it connected, and rtt the round trip its last ping
	// took, said on its pill.
	since time.Time
	rtt   time.Duration
	// silent says its last ping went unanswered.
	silent  bool
	badge   string
	hue     color.NRGBA
	section int
}

// cardHues are the badges' colours, a machine's picked by its name, so
// it keeps its colour from one run to the next.
var cardHues = []color.NRGBA{
	{R: 0x5e, G: 0x9c, B: 0xff, A: 0xff},
	{R: 0x4c, G: 0xc3, B: 0x8a, A: 0xff},
	{R: 0xe8, G: 0xb3, B: 0x4a, A: 0xff},
	{R: 0xd9, G: 0x6c, B: 0xc8, A: 0xff},
	{R: 0x9b, G: 0x7b, B: 0xff, A: 0xff},
	{R: 0x3f, G: 0xc1, B: 0xc9, A: 0xff},
	{R: 0xe5, G: 0x7a, B: 0x5a, A: 0xff},
	{R: 0x8f, G: 0xb5, B: 0x4a, A: 0xff},
}

// hueOf is name's badge colour.
func hueOf(name string) color.NRGBA {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(name)))
	return cardHues[h.Sum32()%uint32(len(cardHues))]
}

// badgeOf is the two letters on name's badge: the first letters of its
// first two words, or its first two letters.
func badgeOf(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var b []rune
	switch {
	case len(words) >= 2:
		b = []rune{[]rune(words[0])[0], []rune(words[1])[0]}
	case len(words) == 1:
		b = []rune(words[0])
		if len(b) > 2 {
			b = b[:2]
		}
	default:
		b = []rune("?")
	}
	return strings.ToUpper(string(b))
}

// localWho is this computer's user@host, as a card says it.
func localWho() string {
	host, _ := os.Hostname()
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
		if i := strings.LastIndexAny(name, `\`); i >= 0 {
			name = name[i+1:]
		}
	}
	switch {
	case name != "" && host != "":
		return name + "@" + host
	case host != "":
		return host
	}
	return name
}

// cardFor is what m's card shows.
func (w *Window) cardFor(m machines.ID, name string) cardInfo {
	c := cardInfo{name: name, badge: badgeOf(name), hue: hueOf(name)}
	if m == machines.Local {
		c.name, c.state, c.section = "This computer", cardLocal, sectionHere
		c.badge = "⌂"
		c.sub = localWho()
		for _, sh := range w.shellChoices {
			if sh.ID == w.chosenShell && w.chosenShell != "" {
				c.sub += " · " + sh.Title
			}
		}
		return c
	}
	c.section = sectionConnected
	switch {
	case slices.Contains(w.dialing, m):
		c.state = cardDialing
	case slices.Contains(w.dropped, m):
		c.state = cardLost
	case slices.Contains(w.connected, m) || slices.ContainsFunc(w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == m }):
		c.state = cardConnected
	default:
		if win, _, far := m.Far(); far && w.isConnected(win) {
			// Reached by another window, which says it is connected.
			c.state = cardConnected
		} else {
			c.section = sectionSaved
		}
	}
	if win, _, far := m.Far(); far {
		c.sub = "through " + w.nameOf(win)
	}
	for _, info := range w.machineList {
		if info.ID != m {
			continue
		}
		if info.Quick {
			c.sub = info.Target + " · not saved"
		}
		c.since, c.rtt, c.silent = info.Since, info.RTT, info.Silent
	}
	for _, h := range w.saved {
		if machines.ID(h.ID) != m {
			continue
		}
		c.sub = hostWho(h)
		if h.Window {
			c.sub = "kakel window · " + h.Target()
		}
		if h.Via != "" {
			c.sub += " · via " + w.nameOf(machines.ID(h.Via))
		}
		if profiles := w.profilesOn(m); len(profiles) > 0 && profiles[0].ID == w.serverShells[h.ID] {
			c.sub += " · " + profiles[0].Name
		}
	}
	return c
}

// hostWho is a saved server's user@address, with its port when it has
// one.
func hostWho(h remote.Host) string {
	s := h.Address
	if h.User != "" {
		s = h.User + "@" + s
	}
	if h.Port != 0 && h.Port != 22 {
		s += ":" + strconv.Itoa(h.Port)
	}
	return s
}

// serverCards holds the cards, by machine, in the order the rows gave.
type serverCards struct {
	w     *Window
	cards map[machines.ID]*serverCard
	order []machines.ID
	grid  *cardGrid
	// rows are the rows last shown, and filter what the search field
	// holds: only the machines it is found in show.
	rows   []sideItem
	filter string
	// compact says the pane is too narrow for cards side by side: each
	// card is a line, its buttons icons at its end.
	compact bool
}

func newServerCards(w *Window) *serverCards {
	s := &serverCards{w: w, cards: map[machines.ID]*serverCard{}}
	s.grid = &cardGrid{s: s, hint: widget.NewLabel("")}
	s.grid.hint.Color = look.Faint
	for i := range s.grid.titles {
		l := widget.NewLabel(sectionTitles[i])
		l.Size, l.Color = smallText, look.Faint
		s.grid.titles[i] = l
	}
	return s
}

// serverCard is one machine's card: its header row, and a list of
// what is open on it.
type serverCard struct {
	anim.Group
	id    machines.ID
	head  *sideRow
	items []*sideRow
	info  cardInfo
	// at is where the card stands, sliding to where the grid puts it;
	// shown how far it has faded in, 0 to 1, as it comes and goes; size
	// its size, as last laid out, which it keeps as it goes.
	at    *anim.Point
	shown *anim.Float
	size  geom.Size
}

// newServerCard is m's card, faded out until it comes in.
func newServerCard(m machines.ID) *serverCard {
	c := &serverCard{id: m, shown: anim.NewFloat(0)}
	c.Add(c.shown)
	return c
}

// Transition implements [gunim.Transitioner]: a card fades in as it
// comes, and out where it stood as it goes.
func (c *serverCard) Transition(p gunim.Presence, f gunim.Frame) bool {
	to := float32(1)
	if p == gunim.Exiting {
		to = 0
	}
	c.shown.Animate(to, widget.Settle.Get(f.Theme))
	return !c.shown.Active()
}

// placeAt moves the card to pt: at once the first time, sliding after.
func (c *serverCard) placeAt(pt geom.Point, f gunim.Frame) {
	if c.at == nil {
		c.at = anim.NewPoint(pt)
		c.Add(c.at)
		return
	}
	c.at.Animate(pt, widget.Settle.Get(f.Theme))
}

// Children implements [gunim.Composite]: the header, then the rows.
func (c *serverCard) Children() []gunim.Node {
	out := []gunim.Node{c.head}
	for _, r := range c.items {
		out = append(out, r)
	}
	return out
}

// cardPad is the room inside a card's edges, and cardGap the room
// between cards.
const (
	cardPad   = 10
	cardGap   = 12
	cardWidth = 330
)

// Layout implements [gunim.Node]: the header, then the rows.
func (c *serverCard) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	w := cs.Max.W
	at := childrenByNode(kids)
	head := at[c.head]
	hs := head.Layout(gunim.Constraints{Max: geom.Sz(w, cs.Max.H)})
	head.Place(geom.Pt(0, 0))
	// Nothing open on it, the header is the whole card, its ring round
	// all of it.
	y := hs.H
	if len(c.items) > 0 {
		y += 4
	}
	for _, r := range c.items {
		k, ok := at[r]
		if !ok {
			continue
		}
		delete(at, r)
		rs := k.Layout(gunim.Constraints{Max: geom.Sz(w-2*cardPad+12, 1e6)})
		k.Place(geom.Pt(cardPad-6, y))
		y += rs.H
	}
	delete(at, c.head)
	// Rows on their way out take no room.
	for _, k := range at {
		k.Layout(gunim.Constraints{})
	}
	if len(c.items) > 0 {
		y += cardPad - 4
	}
	return geom.Sz(w, y)
}

// Paint implements [gunim.Node].
func (c *serverCard) Paint(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	r := geom.Rect{Max: box.Point()}
	if t := c.shown.Value(); t < 1 {
		defer p.Layer(paint.LayerOpts{Bounds: r, Opacity: max(t, 0)})()
	}
	radius := float32(10)
	p.RRect(r, radius, paint.Solid(widget.CardFill.Get(f.Theme)))
	p.RRectStroke(r, radius, paint.Fill{}, paint.Stroke{Width: 1, Color: widget.DialogBorder.Get(f.Theme)})
	at := childrenByNode(kids)
	head := at[c.head]
	head.Paint(p)
	if len(c.items) > 0 {
		// A line between the header and what is open.
		line := widget.DialogBorder.Get(f.Theme)
		y := head.Size().H - 1
		p.RRect(geom.Rc(cardPad, y, box.W-2*cardPad, 1), 0, paint.Solid(line))
		for _, r := range c.items {
			if k, ok := at[r]; ok {
				k.Paint(p)
			}
		}
	}
}

// cardGrid lays the cards out in columns, under their sections'
// titles.
type cardGrid struct {
	s      *serverCards
	titles [sections]*widget.Label
	// hint says what to do where there is nothing to show: no machine
	// found, or no server saved yet.
	hint *widget.Label
	// shown are the sections with a card, and laidOut the cards in each.
	shown [sections]bool
}

// Children implements [gunim.Composite]: the titles, then the cards.
func (g *cardGrid) Children() []gunim.Node {
	out := make([]gunim.Node, 0, sections+len(g.s.order))
	for _, t := range g.titles {
		out = append(out, t)
	}
	out = append(out, g.hint)
	for _, m := range g.s.order {
		out = append(out, g.s.cards[m])
	}
	return out
}

// Layout implements [gunim.Node]: each section's title, then its cards
// in as many columns as fit, each row of cards as tall as its tallest.
func (g *cardGrid) Layout(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	const pad = 16
	width := max(c.Max.W, 1)
	g.s.compact = width < compactBelow
	cols := max(1, int((width-2*pad+cardGap)/(cardWidth+cardGap)))
	cw := (width - 2*pad - float32(cols-1)*cardGap) / float32(cols)
	at := childrenByNode(kids)
	placed := map[gunim.Node]bool{}
	y := float32(4)
	for sec := range sections {
		var in []gunim.Child
		for _, m := range g.s.order {
			c := g.s.cards[m]
			if k, ok := at[c]; ok && c.info.section == sec {
				in = append(in, k)
				placed[c] = true
			}
		}
		title := at[g.titles[sec]]
		placed[g.titles[sec]] = true
		g.shown[sec] = len(in) > 0
		if len(in) == 0 {
			title.Layout(gunim.Constraints{})
			continue
		}
		ts := title.Layout(gunim.Constraints{Max: geom.Sz(width-2*pad, 40)})
		title.Place(geom.Pt(pad+2, y+6))
		y += ts.H + 14
		for row := 0; row < len(in); row += cols {
			tallest := float32(0)
			for col := 0; col < cols && row+col < len(in); col++ {
				k := in[row+col]
				s := k.Layout(gunim.Constraints{Max: geom.Sz(cw, 1e6)})
				card := k.Node().(*serverCard)
				card.size = s
				card.placeAt(geom.Pt(pad+float32(col)*(cw+cardGap), y), f)
				k.Place(card.at.Value())
				tallest = max(tallest, s.H)
			}
			y += tallest + cardGap
		}
		y += 8
	}
	placed[g.hint] = true
	if hint := at[g.hint]; g.hint.Text != "" {
		hs := hint.Layout(gunim.Constraints{Max: geom.Sz(width-2*pad, 200)})
		hint.Place(geom.Pt(pad+2, y))
		y += hs.H + 16
	} else {
		hint.Layout(gunim.Constraints{})
	}
	// Cards on their way out take no room, and fade where they stood.
	for n, k := range at {
		if placed[n] {
			continue
		}
		if card, ok := n.(*serverCard); ok && card.at != nil {
			k.Layout(gunim.Tight(card.size))
			k.Place(card.at.Value())
			continue
		}
		k.Layout(gunim.Constraints{})
	}
	return geom.Sz(width, y+8)
}

// childrenByNode maps each of kids to its node, as the engine's order
// of them need not be the one shown.
func childrenByNode(kids gunim.Children) map[gunim.Node]gunim.Child {
	at := make(map[gunim.Node]gunim.Child, kids.Len())
	for k := range kids.All {
		at[k.Node()] = k
	}
	return at
}

// Paint implements [gunim.Node].
func (g *cardGrid) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	at := childrenByNode(kids)
	for i, t := range g.titles {
		if k, ok := at[t]; ok && g.shown[i] {
			k.Paint(p)
		}
	}
	if g.hint.Text != "" {
		at[g.hint].Paint(p)
	}
	// Those going first, under the ones staying.
	for n, k := range at {
		if card, ok := n.(*serverCard); ok && g.s.cards[card.id] != card {
			k.Paint(p)
		}
	}
	for _, m := range g.s.order {
		if k, ok := at[g.s.cards[m]]; ok {
			k.Paint(p)
		}
	}
}

// sync brings the cards up to date with rows, the machines' headings
// each followed by what is open on it.
func (s *serverCards) sync(rows []sideItem, u *gunim.UI) {
	type group struct {
		head  sideItem
		items []sideItem
	}
	s.rows = rows
	// One group a machine: a machine a window reaches, which a tunnel
	// through it also names at the top, comes together on one card.
	var groups []group
	at := map[string]int{}
	cur := -1
	for _, r := range rows {
		if r.heading {
			i, ok := at[r.key]
			if !ok {
				i = len(groups)
				at[r.key] = i
				groups = append(groups, group{head: r})
			}
			cur = i
			continue
		}
		if cur >= 0 {
			groups[cur].items = append(groups[cur].items, r)
		}
	}
	seen := map[machines.ID]bool{}
	s.order = s.order[:0]
	for _, g := range groups {
		m := machines.ID(strings.TrimPrefix(g.head.key, "machine:"))
		info := s.w.cardFor(m, strings.TrimSuffix(g.head.text, " (quick)"))
		if !s.found(info, g.items) {
			continue
		}
		seen[m] = true
		s.order = append(s.order, m)
		c := s.cards[m]
		fresh := c == nil
		if fresh {
			c = newServerCard(m)
			c.head = s.w.newSideRow(g.head)
			c.head.card = &c.info
			c.head.makeButtons()
			s.cards[m] = c
		} else {
			c.head.set(g.head)
		}
		c.info = info
		c.head.card = &c.info
		c.head.title.Text = c.info.name
		c.head.title.Size, c.head.title.Color = cardTitleSize, widget.Ink
		c.head.note.Size, c.head.note.Color = smallText, look.Faint
		c.head.said = ""
		c.head.note.Text = c.info.sub
		// The rows it had, kept by key, so a row keeps its state.
		had := map[string]*sideRow{}
		for _, r := range c.items {
			had[r.key] = r
		}
		// Cleared before it is filled again: a row gone, left past the
		// end, would keep its pane's terminal, history and all.
		clear(c.items)
		c.items = c.items[:0]
		for _, it := range g.items {
			r := had[it.key]
			if r == nil {
				r = s.w.newSideRow(it)
				if !fresh {
					u.Insert(c, r)
				}
			} else {
				r.set(it)
				delete(had, it.key)
			}
			c.items = append(c.items, r)
		}
		for _, r := range had {
			u.Remove(r)
		}
		if fresh {
			// Inserted whole: its header and rows come with it.
			u.Insert(s.grid, c)
		}
	}
	for m, c := range s.cards {
		if !seen[m] {
			c.head.closeMenu(u)
			u.Remove(c)
			delete(s.cards, m)
		}
	}
	// The sections in their order: this computer, connected, saved.
	slices.SortStableFunc(s.order, func(a, b machines.ID) int { return s.cards[a].info.section - s.cards[b].info.section })
	hint := ""
	switch {
	case s.filter != "" && len(s.order) == 0 && looksLikeHost(s.filter):
		hint = "No machine is called that. Press Enter to connect to " + strings.TrimSpace(s.filter) + "."
	case s.filter != "" && len(s.order) == 0:
		hint = "No machine is called that."
	case s.filter == "" && len(s.w.saved) == 0:
		hint = "No servers saved yet. Add one, or import the ones your SSH config knows, from Add."
	}
	s.grid.hint.Text = hint
}

// found reports whether the search field's text is in a card: its
// name, who and where, or what is open on it.
func (s *serverCards) found(info cardInfo, items []sideItem) bool {
	q := strings.ToLower(strings.TrimSpace(s.filter))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(info.name), q) || strings.Contains(strings.ToLower(info.sub), q) {
		return true
	}
	return slices.ContainsFunc(items, func(it sideItem) bool { return strings.Contains(strings.ToLower(it.text), q) })
}

// find shows only the machines text is found in.
func (s *serverCards) find(text string, u *gunim.UI) {
	s.filter = text
	if s.w.listShown {
		s.sync(s.rows, u)
		u.Invalidate()
	}
}

// looksLikeHost reports whether text could be an address to connect
// to: user@host, or a host name with a dot or a port.
func looksLikeHost(text string) bool {
	text = strings.TrimSpace(text)
	return text != "" && !strings.ContainsAny(text, " \t") && strings.ContainsAny(text, "@.:")
}

// keys are every row's key, headers and what is open under them, in the
// order the cards show them.
func (s *serverCards) keys() []widget.Key {
	var out []widget.Key
	for _, m := range s.order {
		c := s.cards[m]
		out = append(out, widget.Key(c.head.key))
		for _, r := range c.items {
			out = append(out, widget.Key(r.key))
		}
	}
	return out
}

// row is the row with key, a header or one under it.
func (s *serverCards) row(key widget.Key) (*sideRow, bool) {
	for _, m := range s.order {
		c := s.cards[m]
		if widget.Key(c.head.key) == key {
			return c.head, true
		}
		for _, r := range c.items {
			if widget.Key(r.key) == key {
				return r, true
			}
		}
	}
	return nil, false
}

// cardTitleSize is a card's name's size.
var cardTitleSize = theme.Length("kakel.card.title", 15)

// cardHeadHeight is a card's header's height: the name, who and where,
// and the buttons; compactHeadHeight a compact card's, a line.
const (
	cardHeadHeight    = 98
	compactHeadHeight = 50
)

// compactBelow is how narrow the pane is when its cards go compact.
const compactBelow = 420

// The header's buttons: their height, the room between them, and the
// badge's size.
const (
	cardChipHeight = 32
	cardChipGap    = 6
	cardBadge      = 36
)

// The header's buttons, in r.chips, in the order the keyboard reaches
// them: the gear and ⋯ at the top right, Terminal and Files along the
// bottom. The status chip comes after them.
const (
	chipGear = iota
	chipMore
	chipTerminal
	chipFiles
)

// makeButtons gives a header its buttons, once: Terminal and Files,
// which connect first where they have to; the gear, the machine's
// settings; ⋯, the machine's menu; and the status chip, which connects
// and disconnects. They press on release, show the keyboard's ring, and
// say what they do as the pointer rests on them.
func (r *sideRow) makeButtons() {
	if r.chips != nil {
		return
	}
	term := iconButton(icon.SquareTerminal, "Terminal")
	files := iconButton(icon.Folder, "Files")
	gear := iconButton(icon.Settings, "")
	gear.Ghost, gear.Tooltip = true, "Settings"
	more := iconButton(icon.Ellipsis, "")
	more.Ghost, more.Tooltip = true, "Everything that opens here"
	term.OnClick = func(u *gunim.UI) gunim.Intent { u.Send(r, app.OpenOn{Machine: r.machine}); return nil }
	files.OnClick = func(u *gunim.UI) gunim.Intent { u.Send(r, app.OpenFilesOn{Machine: r.machine}); return nil }
	gear.OnClick = func(u *gunim.UI) gunim.Intent { u.Send(r, app.ShowMachine{Machine: r.machine}); return nil }
	more.OnClick = func(u *gunim.UI) gunim.Intent { r.w.openMachineMenu(r, u); return nil }
	r.chips = []*widget.Button{gear, more, term, files}
	r.conn = newConnChip(r.w, func() (machines.ID, *cardInfo) { return r.machine, r.card })
}

// buttonsShown says which of the header's buttons its machine has: a
// saved kakel window not connected has nothing more than Files and its
// settings.
func (r *sideRow) buttonsShown() [4]bool {
	var shown [4]bool
	shown[chipTerminal], shown[chipFiles], shown[chipMore], shown[chipGear] = true, true, true, true
	m := r.machine
	if m == machines.Local {
		return shown
	}
	for _, h := range r.w.saved {
		if machines.ID(h.ID) != m {
			continue
		}
		if h.Window && !slices.ContainsFunc(r.w.remoteWindows, func(rw app.RemoteWindow) bool { return rw.Name == m }) {
			shown[chipTerminal], shown[chipMore] = false, false
		}
	}
	return shown
}

// chipWords are the bottom buttons' words, by button, which a narrow
// card says as the pointer rests on them instead.
var chipWords = map[int]string{chipTerminal: "Terminal", chipFiles: "Files"}

// layoutCard lays a header out. On a card: the badge and the name, with
// the gear and ⋯ at the top right; who and where under the name; along
// the bottom Terminal and Files, and the status chip at the end. As
// room runs short, the chip leaves out how long it has been connected,
// then Terminal and Files leave out their words. Compact, all on one
// line, the buttons icons at its end.
func (r *sideRow) layoutCard(c gunim.Constraints, f gunim.Frame, kids gunim.Children) geom.Size {
	compact := r.w.cards.compact
	shown := r.buttonsShown()
	for i, b := range r.chips {
		b.Disabled = !shown[i]
	}
	// The name and who and where are drawn by the card, fading where
	// they are cut.
	kids.At(0).Layout(gunim.Constraints{})
	kids.At(1).Layout(gunim.Constraints{})
	conn := kids.At(2 + len(r.chips))
	r.conn.compact, r.conn.short = compact, false
	// This computer has no connection to make: no chip.
	local := r.machine == machines.Local
	if len(r.chipRects) != len(r.chips) {
		r.chipRects = make([]geom.Rect, len(r.chips))
	}
	clear(r.chipRects)
	layout := func(i int) geom.Size {
		if !shown[i] {
			kids.At(2 + i).Layout(gunim.Constraints{})
			return geom.Size{}
		}
		return kids.At(2 + i).Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, cardChipHeight)})
	}
	words := func(on bool) {
		for i, word := range chipWords {
			if on {
				r.chips[i].Label, r.chips[i].Tooltip = word, ""
			} else {
				r.chips[i].Label, r.chips[i].Tooltip = "", word
			}
		}
	}
	h := float32(cardHeadHeight)
	badge := float32(cardBadge)
	if compact {
		h, badge = compactHeadHeight, 30
		words(false)
		// From the end: ⋯, the gear, the chip, Files, Terminal.
		x := c.Max.W - cardPad
		for _, i := range []int{chipMore, chipGear, -1, chipFiles, chipTerminal} {
			var s geom.Size
			if i < 0 {
				if local {
					conn.Layout(gunim.Constraints{})
					continue
				}
				s = conn.Layout(gunim.Constraints{Max: geom.Sz(c.Max.W, cardChipHeight)})
				x -= s.W
				conn.Place(geom.Pt(x, (h-s.H)/2))
				x -= cardChipGap
				continue
			}
			if s = layout(i); s.W == 0 {
				continue
			}
			x -= s.W
			r.chipRects[i] = geom.Rc(x, (h-s.H)/2, s.W, s.H)
			x -= 2
		}
		r.textX = cardPad + badge + 10
		r.nameRoom, r.subRoom = max(0, x-4-r.textX), max(0, x-4-r.textX)
	} else {
		// The gear and ⋯ at the top right, close to the edge, as their
		// fill shows only under the pointer.
		x := c.Max.W - cardPad + 4
		for _, i := range []int{chipMore, chipGear} {
			s := layout(i)
			if s.W == 0 {
				continue
			}
			x -= s.W
			r.chipRects[i] = geom.Rc(x, cardPad-4, s.W, s.H)
		}
		r.textX = cardPad + badge + 10
		r.nameRoom = max(0, x-6-r.textX)
		r.subRoom = r.nameRoom
		// Along the bottom: as much as fits of the buttons' words and
		// the chip's.
		y := h - cardPad - cardChipHeight
		room := c.Max.W - 2*cardPad
		var cs, ts, fs geom.Size
		fit := func() bool {
			if local {
				cs = conn.Layout(gunim.Constraints{})
			} else {
				cs = conn.Layout(gunim.Constraints{Max: geom.Sz(room, cardChipHeight)})
			}
			ts, fs = layout(chipTerminal), layout(chipFiles)
			return ts.W+fs.W+2*cardChipGap+cs.W <= room
		}
		// Short of room, the chip says less first: how long it has
		// been connected, then its words, leaving the switch in the
		// state's colour. Then the buttons lose theirs.
		words(true)
		r.conn.bare = false
		if !fit() {
			r.conn.short = true
			if !fit() {
				r.conn.bare = true
				if !fit() {
					words(false)
					fit()
				}
			}
		}
		x = cardPad
		for _, b := range []struct {
			i int
			s geom.Size
		}{{chipTerminal, ts}, {chipFiles, fs}} {
			i, s := b.i, b.s
			if s.W == 0 {
				continue
			}
			r.chipRects[i] = geom.Rc(x, y, s.W, s.H)
			x += s.W + cardChipGap
		}
		conn.Place(geom.Pt(c.Max.W-cardPad-cs.W, y+(cardChipHeight-cs.H)/2))
	}
	for i := range r.chips {
		kids.At(2 + i).Place(r.chipRects[i].Min)
	}
	return geom.Sz(c.Max.W, h)
}

// paintCard draws a header: the badge, the name and who and where,
// faded where they are cut, and the buttons.
func (r *sideRow) paintCard(p *paint.Painter, f gunim.Frame, box geom.Size, kids gunim.Children) {
	th := f.Theme
	info := r.card
	compact := r.w.cards.compact
	badge := float32(cardBadge)
	b := geom.Rc(cardPad, cardPad, badge, badge)
	if compact {
		badge = 30
		b = geom.Rc(cardPad, (box.H-badge)/2, badge, badge)
	}
	// The badge: the machine's letters on its colour, or this computer's
	// laptop.
	fill := info.hue
	fill.A = 0x38
	p.RRect(b, 9, paint.Solid(fill))
	if r.machine == machines.Local {
		drawIcon(p, icon.Laptop, b.Inset(geom.Uniform(badge/4)), info.hue, 1.8)
	} else {
		run := text.GoSans(true, false).Shape(info.badge, 14)
		run.Paint(p, geom.Pt(b.Center().X-run.Advance/2, b.Center().Y-run.Height()/2), info.hue)
	}
	name := text.Default().Shape(info.name, cardTitleSize.Get(th))
	sub := text.Default().Shape(info.sub, smallText.Get(th))
	top := float32(cardPad + 1)
	if compact {
		top = (box.H - name.Height() - sub.Height() - 2) / 2
	}
	fade := widget.TextFade.Get(th)
	at := geom.Pt(r.textX, top)
	name.PaintFaded(p, at, widget.Ink.Get(th), r.nameRoom, fade)
	r.nameAt = geom.Rc(at.X, at.Y, min(name.Advance, r.nameRoom), name.Height())
	r.nameCut = name.Advance > r.nameRoom
	at = geom.Pt(r.textX, top+name.Height()+2)
	sub.PaintFaded(p, at, look.Faint.Get(th), r.subRoom, fade)
	r.subAt = geom.Rc(at.X, at.Y, min(sub.Advance, r.subRoom), sub.Height())
	r.subCut = sub.Advance > r.subRoom
	for i := 2; i < kids.Len(); i++ {
		if i-2 < len(r.chips) && r.chips[i-2].Disabled {
			continue
		}
		kids.At(i).Paint(p)
	}
	if compact {
		// The state as a dot on the badge's corner.
		_, c := pillOf(info.state, th)
		dot := geom.Rc(b.Max.X-8, b.Max.Y-8, 11, 11)
		p.RRect(dot, 5.5, paint.Solid(widget.CardFill.Get(th)))
		p.RRect(dot.Inset(geom.Uniform(2)), 3.5, paint.Solid(c))
	}
	if t := r.ring.Value(); t > 0.01 {
		c := widget.Accent.Get(th)
		c.A = uint8(float32(c.A) * min(t, 1))
		p.RRectStroke(geom.Rect{Max: box.Point()}.Inset(geom.Uniform(1)), 9, paint.Fill{}, paint.Stroke{Width: 1.5, Color: c})
	}
}

// cutWords is the whole of the name or of who and where under the
// pointer at pos, where it is cut, for its tooltip; "" elsewhere.
func (r *sideRow) cutWords(pos geom.Point) string {
	switch {
	case r.nameCut && r.nameAt.Contains(pos):
		return r.card.name
	case r.subCut && r.subAt.Contains(pos):
		return r.card.sub
	}
	return ""
}

// pillOf is a state's words and colour.
func pillOf(s cardState, th *theme.Live) (string, color.NRGBA) {
	return pillFor(cardInfo{state: s}, time.Time{}, th)
}

// pillFor is a card's pill's words and colour at now: a connected
// machine's round trip and how long it has been connected, once pinged.
func pillFor(c cardInfo, now time.Time, th *theme.Live) (string, color.NRGBA) {
	switch c.state {
	case cardLocal:
		return "local", widget.ToastSuccessInk.Get(th)
	case cardConnected:
		if c.silent {
			return "not answering", widget.ToastWarningInk.Get(th)
		}
		if c.rtt > 0 && !c.since.IsZero() {
			return roundTrip(c.rtt) + " · " + connectedFor(now.Sub(c.since)), widget.ToastSuccessInk.Get(th)
		}
		return "connected", widget.ToastSuccessInk.Get(th)
	case cardDialing:
		return "connecting", widget.ToastWarningInk.Get(th)
	case cardLost:
		return "connection lost", widget.DialogDangerInk.Get(th)
	}
	return "not connected", look.Faint.Get(th)
}

// roundTrip says a ping's round trip: in milliseconds, or seconds once
// it takes one.
func roundTrip(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return "<1 ms"
	case d < time.Second:
		return strconv.Itoa(int(d.Milliseconds())) + " ms"
	}
	return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + " s"
}

// connectedFor says how long a connection has stood, to the minute.
func connectedFor(d time.Duration) string {
	m := int(d.Minutes())
	switch {
	case m < 1:
		return "just now"
	case m < 60:
		return strconv.Itoa(m) + " min"
	case m < 24*60:
		s := strconv.Itoa(m/60) + " h"
		if m%60 != 0 {
			s += " " + strconv.Itoa(m%60) + " min"
		}
		return s
	}
	s := strconv.Itoa(m/(24*60)) + " d"
	if h := m % (24 * 60) / 60; h != 0 {
		s += " " + strconv.Itoa(h) + " h"
	}
	return s
}

// handleCard is a header's answer to the pointer and the keys: a press
// on its room gives it the keyboard; its buttons answer for themselves.
func (r *sideRow) handleCard(e input.Event, u *gunim.UI) bool {
	switch e := e.(type) {
	case input.PointerMove:
		r.cutTip.Handle(e, u, r, r.cutWords(e.Pos))
		return false
	case input.PointerLeave:
		r.cutTip.Handle(e, u, r, "")
		return false
	case input.PointerDown:
		if e.Button != input.ButtonPrimary {
			return false
		}
		u.Focus(r)
		return true
	case input.FocusGained:
		r.ring.Animate(1, widget.Quick.Get(u.Theme()))
		return false
	case input.FocusLost:
		r.ring.Animate(0, widget.Settle.Get(u.Theme()))
		return false
	case input.KeyPress:
		if r.menu != nil {
			if e.Key == input.KeyEscape || e.Key == input.KeyTab {
				r.closeMenu(u)
				return true
			}
			return r.menu.Key(e, u)
		}
		return r.cardKey(e, u)
	}
	return false
}

// cardKey is a header's answer to a key. The arrows go to what is above,
// below or beside it on the screen; Enter opens the menu of what opens
// on the machine, its first line a terminal; F opens the files, E edits
// the machine, Delete removes it, the menu key or Shift+F10 opens the
// menu too. With Ctrl or Alt held a key is the window's, as Ctrl+PageDown
// to the next tab.
func (r *sideRow) cardKey(e input.KeyPress, u *gunim.UI) bool {
	w, m := r.w, r.machine
	if e.Key == input.KeyMenu || e.Key == input.KeyF10 && e.Mods == input.ModShift {
		w.openMachineMenu(r, u)
		return true
	}
	if e.Mods != 0 {
		return false
	}
	switch e.Key {
	case input.KeyUp, input.KeyDown, input.KeyLeft, input.KeyRight:
		w.cards.move(r, e.Key, u)
	case input.KeyHome:
		w.focusRow("", 1, u)
	case input.KeyEnd:
		w.focusRowsAway(r.key, len(w.cards.keys()), u)
	case input.KeyPageUp, input.KeyPageDown:
		step := sidebarPage
		if e.Key == input.KeyPageUp {
			step = -step
		}
		w.focusRowsAway(r.key, step, u)
	case input.KeyEnter, input.KeyKPEnter, input.KeySpace:
		w.openMachineMenu(r, u)
	case input.KeyF:
		u.Send(w, app.OpenFilesOn{Machine: m})
	case input.KeyE:
		u.Send(w, app.ShowMachine{Machine: m})
	case input.KeyDelete:
		if m != machines.Local && slices.ContainsFunc(w.saved, func(h remote.Host) bool { return machines.ID(h.ID) == m }) {
			w.confirmRemove(m, u)
		}
	case input.KeyEscape:
		// Back to the pane last worked in, wherever it is.
		if id := w.lastWorked; id != "" && id != w.focused {
			u.Send(r, app.FocusPane{Pane: id})
		} else if n := w.focusNode(w.focused, u); n != nil {
			u.Focus(n)
		}
	default:
		return false
	}
	return true
}

// move gives the keyboard to what is next to from on the screen, the
// way key points: the nearest header or row past from's edge, the one
// most in line with it first. Down from a header is the first row under
// it; down from a card's last row, the card below.
func (s *serverCards) move(from gunim.Node, key input.Key, u *gunim.UI) {
	cur, ok := u.Bounds(from)
	if !ok {
		return
	}
	var best gunim.Node
	bestScore := float32(-1)
	for _, k := range s.keys() {
		row, ok := s.row(k)
		if !ok || !row.takesKeys() || gunim.Node(row) == from {
			continue
		}
		b, ok := u.Bounds(row)
		if !ok {
			continue
		}
		var along, across float32
		switch key {
		case input.KeyDown:
			along, across = b.Min.Y-cur.Max.Y, b.Center().X-cur.Center().X
		case input.KeyUp:
			along, across = cur.Min.Y-b.Max.Y, b.Center().X-cur.Center().X
		case input.KeyRight:
			along, across = b.Min.X-cur.Max.X, b.Center().Y-cur.Center().Y
		case input.KeyLeft:
			along, across = cur.Min.X-b.Max.X, b.Center().Y-cur.Center().Y
		}
		if along < -1 {
			continue
		}
		score := max(along, 0) + 3*abs32(across)
		if best == nil || score < bestScore {
			best, bestScore = row, score
		}
	}
	if best != nil {
		u.Focus(best)
		u.Reveal(best)
	}
}

// abs32 is x without its sign.
func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// at is the header of the card at p, in the window's space, or nil.
func (s *serverCards) at(p geom.Point, u *gunim.UI) gunim.Node {
	for _, m := range s.order {
		c := s.cards[m]
		if box, ok := u.Bounds(c); ok && box.Contains(p) {
			return c.head
		}
	}
	return nil
}

// first is the first card's header, for the keyboard to come to, or nil
// with no card.
func (s *serverCards) first() gunim.Node {
	if len(s.order) == 0 {
		return nil
	}
	return s.cards[s.order[0]].head
}

// menuAnchor is where a heading's menu opens from: its ⋯ on a card, its
// plus otherwise.
func (r *sideRow) menuAnchor(box geom.Size, _ *theme.Live) geom.Rect {
	if r.card != nil && len(r.chipRects) == len(r.chips) {
		// Its ⋯, or the last button shown where it has none.
		if !r.chipRects[chipMore].Empty() {
			return r.chipRects[chipMore]
		}
		for i := len(r.chipRects) - 1; i >= 0; i-- {
			if !r.chipRects[i].Empty() {
				return r.chipRects[i]
			}
		}
	}
	return geom.Rect{Min: geom.Pt(box.W-plusWidth-6, 0), Max: box.Point()}
}
