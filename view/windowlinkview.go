package view

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/access"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/icon"
	gi "github.com/marrasen/gunim/input"
	"github.com/marrasen/gunim/paint"
	"github.com/marrasen/gunim/theme"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/look"
	"github.com/marrasen/kakel/machines"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/words"
)

// The window's side of sharing it with a link: the dialog that makes a
// link and lists those there are, and the one that asks about a guest.

// The words of sharing with a link, said in its dialog and in the
// settings.
const (
	linkSays       = "Somebody with a web browser works in this window's terminals with you. The link lets in one guest, once, when you allow it."
	linkRelaySays  = "Whoever runs the relay could see everything."
	linkTypeSays   = "A guest who types can do anything you can in the terminals shared."
	linkNoneSays   = "This build of kakel has no web client. BUILDING.md says how to build one in."
	linkDialogWide = 500
)

// guestMays are what a guest can do, as a choice offers them.
var guestMays = []string{"Type", "Only watch"}

// mayType reports whether the choice of guestMays at i lets a guest type.
func mayType(i int) bool { return i == 0 }

// mayIndex is the choice of guestMays for whether a guest may type.
func mayIndex(canType bool) int {
	if canType {
		return 0
	}
	return 1
}

// The room in the dialog: within a share's card, and between its words.
var (
	linkCardGap = theme.Length("kakel.link.card", 10)
	linkTextGap = theme.Length("kakel.link.text", 2)
)

// linksHere are the shares of this window.
func (w *Window) linksHere() []app.WindowLink {
	return slices.DeleteFunc(slices.Clone(w.links), func(l app.WindowLink) bool { return l.Window != w.winID })
}

// liveLinks are the shares of this window that go on.
func (w *Window) liveLinks() []app.WindowLink {
	return slices.DeleteFunc(w.linksHere(), func(l app.WindowLink) bool { return !l.Live() })
}

// linkChip is what the chip says of a window's shares, "" for none, and
// whether it says that a guest waits to be let in.
func linkChip(live []app.WindowLink) (text string, waits bool) {
	var asking, in []string
	for _, l := range live {
		switch {
		case l.Stands == app.LinkAsking:
			asking = append(asking, guestCalled(l))
		case l.GuestIn():
			in = append(in, guestCalled(l))
		}
	}
	switch {
	case len(asking) == 1:
		return asking[0] + " asks", true
	case len(asking) > 1:
		return words.ManyOf(len(asking), "guest asks", "guests ask"), true
	case len(in) == 1:
		return in[0], false
	case len(in) > 1:
		return words.ManyOf(len(in), "guest", "guests"), false
	case len(live) > 1:
		return "Shared · " + strconv.Itoa(len(live)), false
	case len(live) == 1:
		return "Shared", false
	}
	return "", false
}

// linkHeading is what a share's card is headed.
func linkHeading(l app.WindowLink) string {
	if l.Guest != "" {
		return l.Guest
	}
	return "Link"
}

// left is how long is left until t, as a clock counts it down.
func left(t, now time.Time) string {
	d := max(t.Sub(now), 0).Round(time.Second)
	return fmt.Sprintf("%d:%02d", int(d.Minutes()), int(d.Seconds())%60)
}

// answerIn says how long there is to answer about a guest.
func answerIn(l app.WindowLink, now time.Time) string {
	if l.Until.IsZero() {
		return ""
	}
	return "Answer within " + left(l.Until, now) + "."
}

// guestFrom says where a guest came from, and with what.
func guestFrom(l app.WindowLink) string {
	s := "from " + l.From
	if l.About != "" {
		s += ", with " + l.About
	}
	return s
}

// linkStands says how a share stands, now.
func linkStands(l app.WindowLink, now time.Time) string {
	if l.Live() && l.Trouble != "" {
		return "Couldn't reach the relay: " + strings.TrimSuffix(l.Trouble, ".") + ". Trying again."
	}
	switch l.Stands {
	case app.LinkReaching:
		return "Reaching " + l.Relay + "…"
	case app.LinkSending:
		return "Sending the web client to " + l.Relay + "…"
	case app.LinkWaiting:
		return "Waiting for a guest. Expires in " + left(l.Until, now) + "."
	case app.LinkAsking:
		return "Asks to join, " + guestFrom(l) + ". " + answerIn(l, now)
	case app.LinkIn:
		return "In the window since " + clockAt(l.Since, now) + ", " + guestFrom(l) + "."
	case app.LinkAway:
		return "Connection lost. Ends in " + left(l.Until, now) + "."
	case app.LinkEnded:
	}
	if l.Trouble != "" {
		return l.Trouble
	}
	return upFirst(l.Over) + "."
}

// linkBox is the Share Window with Link dialog, while it is open: the
// face that makes a link, and the one that lists those there are, each
// folding away as the other comes.
type linkBox struct {
	w    *Window
	d    *widget.Dialog
	col  *widget.Flex
	face [2]*widget.Fold
	// listing says the links are shown.
	listing bool
	// tries is the count of links asked for as one was asked for here,
	// and waiting that it has not been tried yet.
	tries   uint64
	waiting bool

	name, relay *widget.TextField
	may         *widget.Segmented
	via         *widget.Dropdown
	viaIDs      []machines.ID
	wrong       *widget.Label
	wrongFold   *widget.Fold

	list    *widget.List
	another *widget.Button
}

// The dialog's faces.
const (
	linkMakes = iota
	linkLists
)

// linkDialog opens the dialog: on the links there are, where there are
// any, or else to make one.
func (w *Window) linkDialog(u *gunim.UI) {
	if w.linkBox != nil && w.dialog == w.linkBox.d && u.Presence(w.dialog) != gunim.Exiting {
		return
	}
	d := widget.NewDialog("Share Window with Link")
	d.Width, d.GrowDown = linkDialogWide, true
	d.OnDismiss = widget.Sends(app.ClearLinks{Window: w.winID})
	if w.linkSetup.NoClient && len(w.linksHere()) == 0 {
		d.SetButtons("Close", "")
		d.OnAccept = d.OnDismiss
		d.Body = widget.NewForm().Add("", widget.NewLabel(linkNoneSays))
		w.openDialog(d, u)
		return
	}
	// Create makes the link and leaves the dialog open, which turns to it
	// and has Close alone then.
	d.SetButtons("Create", "Close")
	d.StayOpen = true
	b := newLinkBox(w, d)
	d.Body = b
	d.OnAccept = func(u *gunim.UI) gunim.Intent { b.make(u); return nil }
	w.linkBox = b
	w.openDialog(d, u)
	b.sync(w.linksHere(), u)
	b.tick(u)
}

func newLinkBox(w *Window, d *widget.Dialog) *linkBox {
	s := w.linkSetup
	b := &linkBox{w: w, d: d, name: widget.NewTextField(), relay: widget.NewTextField(), may: newGuestMay(!s.Watch),
		wrong: widget.NewLabel(""), list: widget.NewList(), another: widget.NewButton("Add")}
	b.name.SetText(s.Name, nil)
	b.relay.SetText(s.Relay, nil)
	b.relay.Select(0, 0)
	b.relay.Placeholder = relayFormat
	b.via, b.viaIDs = w.throughPicker(s.Via)
	b.via.Disabled = len(b.viaIDs) <= 1
	b.wrong.Color = widget.DialogProblem
	b.wrongFold = widget.NewFold(under(b.wrong, linkUnder), false)
	b.relay.OnChange = func(_ string, u *gunim.UI) gunim.Intent { b.wrongFold.SetOpen(false, u); return nil }
	// What goes under a field is the field's own, so a fold shut leaves no
	// room behind it.
	form := widget.NewForm().
		Add("Your name", b.name).
		Add("Guest can", folded(b.may, typeSays(b.may))).
		Add("Relay", folded(b.relay, b.wrongFold)).
		Add("Through", b.via).
		Add("", faintLabel(linkRelaySays))
	makes := folded(widget.NewLabel(linkSays), under(form, linkUnderForm))

	b.another.Icon = icon.Plus
	b.another.Tooltip = "Make another link"
	b.another.Disabled = s.NoClient
	b.another.OnClick = func(u *gunim.UI) gunim.Intent { b.turn(linkMakes, u); return nil }
	foot := widget.Row(b.another)
	foot.Justify = widget.JustifyStart
	lists := folded(b.list, under(foot, linkFoot))

	b.listing = len(w.linksHere()) > 0
	d.SetButtonShown(d.OK(), !b.listing, nil)
	b.face[linkMakes], b.face[linkLists] = widget.NewFold(makes, !b.listing), widget.NewFold(lists, b.listing)
	// The links above, so they come where they stay.
	b.col = folded(b.face[linkLists], b.face[linkMakes])
	return b
}

// relayFormat is how a relay's URL is written.
const relayFormat = "https://relay.example.com/k/…"

// newGuestMay is the choice of what a guest can do.
func newGuestMay(canType bool) *widget.Segmented {
	s := widget.NewSegmented(guestMays...)
	s.SetSelected(mayIndex(canType), nil)
	s.Tooltip = "Guest can"
	return s
}

// typeSays says what a guest who types can do, while may is to let it.
func typeSays(may *widget.Segmented) *widget.Fold {
	f := widget.NewFold(under(faintLabel(linkTypeSays), linkUnder), mayType(may.Selected()))
	was := may.OnChange
	may.OnChange = func(i int, u *gunim.UI) gunim.Intent {
		f.SetOpen(mayType(i), u)
		if was != nil {
			return was(i, u)
		}
		return nil
	}
	return f
}

// folded is a column of parts, each with the room it needs in itself.
func folded(parts ...gunim.Node) *widget.Flex {
	col := widget.Column(parts...)
	col.Cross, col.Gap = widget.CrossStretch, noGap
	return col
}

// make makes a link of what is typed, or says what is wrong with it.
func (b *linkBox) make(u *gunim.UI) {
	if _, err := relay.ParseURL(b.relay.Text()); err != nil {
		b.wrong.Text = "Invalid relay URL"
		if strings.TrimSpace(b.relay.Text()) == "" {
			b.wrong.Text = "Enter a relay URL"
		}
		b.wrongFold.SetOpen(true, u)
		u.Focus(b.relay)
		return
	}
	b.wrongFold.SetOpen(false, u)
	b.tries, b.waiting = b.w.linkSetup.Tries, true
	u.Send(b.w, app.ShareWindow{Window: b.w.winID, Name: strings.TrimSpace(b.name.Text()), CanType: mayType(b.may.Selected()),
		Relay: strings.TrimSpace(b.relay.Text()), Via: b.viaIDs[max(0, min(b.via.Selected(), len(b.viaIDs)-1))]})
	b.turn(linkLists, u)
}

// turn shows a face of the dialog in place of the other.
func (b *linkBox) turn(to int, u *gunim.UI) {
	b.listing = to == linkLists
	b.d.SetButtonShown(b.d.OK(), !b.listing, u)
	for i, f := range b.face {
		f.SetOpen(i == to, u)
	}
	if !b.listing {
		u.Focus(b.name)
		return
	}
	// To the dialog itself, until there is something to press.
	u.Focus(b.d)
}

// sync shows the links as they are.
func (b *linkBox) sync(links []app.WindowLink, u *gunim.UI) {
	now := time.Now()
	widget.Sync(b.list, u, links, linkKey,
		func(l app.WindowLink) *linkCard { return newLinkCard(b.w, l, now) },
		func(c *linkCard, l app.WindowLink, u *gunim.UI) { c.show(l, now, u) })
}

// linkKey is a share's card: another once it has a guest, and once it is
// over, so the one goes as the other comes.
func linkKey(l app.WindowLink) widget.Key {
	kind := "link"
	switch {
	case !l.Live():
		kind = "over"
	case l.GuestIn() || l.Stands == app.LinkAsking:
		kind = "guest"
	}
	return widget.Key(l.ID + ":" + kind)
}

// tick counts the links' times down, a second at a time, while the
// dialog is open.
func (b *linkBox) tick(u *gunim.UI) {
	u.After(time.Second, func(u *gunim.UI) {
		if b.w.linkBox != b || u.Presence(b.d) == gunim.Exiting {
			return
		}
		if b.listing {
			b.sync(b.w.linksHere(), u)
			u.Invalidate()
		}
		b.tick(u)
	})
}

// showLinkBox keeps the dialog up to date with the shares.
func (w *Window) showLinkBox(u *gunim.UI) {
	b := w.linkBox
	if b == nil {
		return
	}
	if u.Presence(b.d) == gunim.Exiting || w.dialog != b.d {
		w.linkBox = nil
		return
	}
	here := w.linksHere()
	b.sync(here, u)
	b.another.Disabled = w.linkSetup.NoClient
	if b.waiting && w.linkSetup.Tries == b.tries {
		return
	}
	b.waiting = false
	if b.listing && len(here) == 0 {
		// None left, or the one asked for could not be begun.
		b.turn(linkMakes, u)
	}
}

// Children implements [gunim.Composite].
func (b *linkBox) Children() []gunim.Node { return []gunim.Node{b.col} }

// Layout implements [gunim.Node].
func (b *linkBox) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	s := k.Layout(c)
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node].
func (b *linkBox) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// Focusables are what Tab moves through, of the face shown.
func (b *linkBox) Focusables() []gunim.Node {
	if !b.listing {
		out := []gunim.Node{b.name, b.may, b.relay}
		if !b.via.Disabled {
			out = append(out, b.via)
		}
		return out
	}
	var out []gunim.Node
	for _, k := range b.list.Keys() {
		if n, ok := b.list.Row(k); ok {
			if c, ok := n.(*linkCard); ok {
				out = append(out, c.focusables()...)
			}
		}
	}
	if !b.another.Disabled {
		out = append(out, b.another)
	}
	return out
}

// linkCard is one share on the dialog: who is in it or that nobody is,
// how it stands, the link while it works, and what can be done.
type linkCard struct {
	w *Window
	l app.WindowLink

	card         *widget.Card
	head, says   *widget.Label
	link         *widget.Label
	copyIt, stop *widget.Button
	// asks opens the question about a guest who waits for its answer.
	asks   *widget.Button
	askRow *widget.Fold

	linkRow, busy, mayRow *widget.Fold
	may                   *widget.Segmented
}

func newLinkCard(w *Window, l app.WindowLink, now time.Time) *linkCard {
	c := &linkCard{w: w, head: widget.NewLabel(""), says: faintLabel(""), link: selectable(""), copyIt: widget.NewButton("Copy"),
		may: newGuestMay(l.CanType), stop: widget.NewButton("Stop"), asks: widget.NewButton("Show")}
	c.head.Face = widget.BoldFont
	c.link.Face, c.link.MaxLines = widget.MonoFont, 1
	id := l.ID
	c.copyIt.Icon, c.copyIt.Tooltip = icon.Copy, "Copy the link"
	c.copyIt.OnClick = widget.Sends(app.CopyLink{ID: id})
	c.stop.Icon = icon.Unlink
	c.stop.OnClick = widget.Sends(app.StopLink{ID: id})
	c.asks.Kind = widget.ButtonPrimary
	c.asks.OnClick = func(u *gunim.UI) gunim.Intent { w.askFirst = id; w.showGuestAsk(u); return nil }
	c.may.OnChange = func(i int, _ *gunim.UI) gunim.Intent { return app.SetLinkMay{ID: id, CanType: mayType(i)} }
	mark := widget.NewIcon(linkMark(l), "")
	mark.Color = look.Guest
	if !l.Live() {
		mark.Color = look.Faint
	}
	text := widget.Column(c.head, c.says)
	text.Cross, text.Gap = widget.CrossStretch, linkTextGap
	parts := []gunim.Node{mark, text}
	if l.Live() {
		parts = append(parts, c.stop)
	}
	top := widget.Row(parts...).Grow(text, 1)
	top.Cross, top.Gap = widget.CrossCenter, linkCardGap
	// said is a row of words and what goes with them.
	said := func(n gunim.Node, control gunim.Node) *widget.Fold {
		r := widget.Row(n, control).Grow(n, 1)
		r.Cross, r.Gap = widget.CrossCenter, linkCardGap
		return widget.NewFold(under(r, linkUnder), false)
	}
	c.linkRow = said(c.link, c.copyIt)
	c.mayRow = said(widget.NewLabel("Guest can"), c.may)
	c.askRow = said(widget.NewLabel("Waits for your answer"), c.asks)
	bar := widget.NewProgressBar()
	bar.Indeterminate = true
	c.busy = widget.NewFold(under(bar, linkUnder), false)
	c.card = widget.NewCard(folded(top, c.busy, c.linkRow, c.askRow, c.mayRow))
	c.show(l, now, nil)
	return c
}

// under is n with room round it, for a fold to take away with it.
func under(n gunim.Node, room theme.Token[geom.Insets]) gunim.Node {
	p := widget.NewPad(n)
	p.Padding = room
	return p
}

// linkUnder is the room above a part of a card or under a field,
// linkUnderForm above the form, and linkFoot round the button under the
// links, whose focus ring has to have room.
var (
	linkUnder     = theme.Insets("kakel.link.under", geom.Insets{Top: 10})
	linkUnderForm = theme.Insets("kakel.link.under.form", geom.Insets{Top: 14})
	linkFoot      = theme.Insets("kakel.link.foot", geom.Insets{Left: 4, Top: 10, Bottom: 4})
)

// linkMark is the image of a share's card.
func linkMark(l app.WindowLink) *icon.Icon {
	switch {
	case !l.Live():
		return icon.Unlink
	case l.Guest != "":
		return icon.UserRound
	}
	return icon.Link
}

// show shows the share as it is.
func (c *linkCard) show(l app.WindowLink, now time.Time, u *gunim.UI) {
	c.l = l
	c.head.Text, c.says.Text = linkHeading(l), linkStands(l, now)
	c.says.Color = look.Faint
	if l.Trouble != "" {
		c.says.Color = widget.DialogProblem
	}
	if l.Link != "" {
		// Kept as it folds away.
		c.link.Text = l.Link
	}
	c.linkRow.SetOpen(l.Link != "", u)
	c.copyIt.Disabled = l.Link == ""
	c.busy.SetOpen(l.Stands == app.LinkReaching || l.Stands == app.LinkSending, u)
	c.mayRow.SetOpen(l.Live() && l.Stands != app.LinkAsking, u)
	c.askRow.SetOpen(l.Stands == app.LinkAsking, u)
	if i := mayIndex(l.CanType); c.may.Selected() != i {
		c.may.SetSelected(i, u)
	}
	c.may.Disabled = !l.Live()
	c.stop.Disabled = !l.Live()
}

// focusables are what Tab moves through on the card.
func (c *linkCard) focusables() []gunim.Node {
	if !c.l.Live() {
		return nil
	}
	out := []gunim.Node{}
	if c.l.Link != "" {
		out = append(out, c.copyIt)
	}
	if c.l.Stands == app.LinkAsking {
		out = append(out, c.asks)
	} else {
		out = append(out, c.may)
	}
	return append(out, c.stop)
}

// Access implements [gunim.Accessible]: what is on the card is the
// share's, by its guest or as a link.
func (c *linkCard) Access() access.Info {
	return access.Info{Role: access.RoleGroup, Name: linkHeading(c.l), Description: c.says.Text}
}

// Children implements [gunim.Composite].
func (c *linkCard) Children() []gunim.Node { return []gunim.Node{c.card} }

// Layout implements [gunim.Node].
func (c *linkCard) Layout(cs gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	s := k.Layout(cs)
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node].
func (c *linkCard) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// guestAsk is the dialog that asks whether a guest may come in, while it
// is open. Nothing in it works until nothing has been pressed, with the
// keyboard or the pointer, for a moment: it opens by itself, under the
// hands of somebody at work in a terminal. Then the button that refuses
// has the keyboard. A window that loses the keyboard begins that again as
// it gets it back.
type guestAsk struct {
	w    *Window
	id   string
	d    *widget.Dialog
	form *widget.Form
	may  *widget.Segmented
	// within says how long there is to answer.
	within *widget.Label
	// ready says it works, and stop cancels what makes it.
	ready bool
	stop  func()
}

// askWait is how long nothing is pressed before the dialog that asks
// about a guest works, and askAgain how long until it is seen whether
// another dialog still keeps it back.
const (
	askWait  = time.Second
	askAgain = 200 * time.Millisecond
)

// showGuestAsk asks about a guest who waits to be let into this window,
// the one that asked first, once no other dialog is open, and takes the
// dialog away when the question goes.
func (w *Window) showGuestAsk(u *gunim.UI) {
	asking := slices.DeleteFunc(w.linksHere(), func(l app.WindowLink) bool { return l.Stands != app.LinkAsking || w.answered[l.ID] })
	w.callForGuests(asking, u)
	if g := w.guestAsk; g != nil {
		if i := slices.IndexFunc(asking, func(l app.WindowLink) bool { return l.ID == g.id }); i >= 0 && u.Presence(g.d) != gunim.Exiting {
			g.within.Text = answerIn(asking[i], time.Now())
			return
		}
		if u.Presence(g.d) != gunim.Exiting {
			g.d.Close(u)
		}
		w.guestAsk = nil
	}
	if len(asking) == 0 {
		return
	}
	if b := w.linkBox; b != nil && w.dialog == b.d && u.Presence(b.d) != gunim.Exiting {
		// The one the link was made in gives way.
		b.d.Close(u)
	}
	if w.dialog != nil && u.Presence(w.dialog) != gunim.Exiting {
		// Once the one open has closed, which nothing may say.
		if !w.askingLater {
			w.askingLater = true
			u.After(askAgain, func(u *gunim.UI) { w.askingLater = false; w.showGuestAsk(u) })
		}
		return
	}
	i := max(0, slices.IndexFunc(asking, func(l app.WindowLink) bool { return l.ID == w.askFirst }))
	g := newGuestAsk(w, asking[i])
	w.guestAsk, w.askFirst = g, ""
	w.openDialog(g.d, u)
	g.hold(u)
	g.tick(u)
	if w.away || w.behind {
		u.RequestAttention()
	}
}

// callForGuests has the user told, outside kakel's windows, of each guest
// who asks while no kakel window has the keyboard.
func (w *Window) callForGuests(asking []app.WindowLink, u *gunim.UI) {
	if w.calledFor == nil {
		w.calledFor, w.answered = map[string]bool{}, map[string]bool{}
	}
	for _, l := range asking {
		if w.calledFor[l.ID] {
			continue
		}
		w.calledFor[l.ID] = true
		// Behind another of kakel's windows, that one says it.
		if w.away && !w.behind {
			u.Send(w, app.GuestUnseen{ID: l.ID})
		}
	}
}

func newGuestAsk(w *Window, l app.WindowLink) *guestAsk {
	g := &guestAsk{w: w, id: l.ID, may: newGuestMay(l.CanType), within: faintLabel(answerIn(l, time.Now()))}
	fact := func(name, note string) gunim.Node {
		n := widget.NewLabel(name)
		n.Face = widget.BoldFont
		small := widget.NewLabel(note)
		small.Size, small.Color = smallText, look.Faint
		col := widget.Column(n, small)
		col.Cross, col.Gap = widget.CrossStretch, factGap
		return col
	}
	about := "Browser not said"
	if l.About != "" {
		about = l.About + ", as the browser says"
	}
	g.form = widget.NewForm().
		Add("Name", fact(guestCalled(l), "As the guest wrote it")).
		Add("From", fact(l.From, about)).
		Add("Guest can", folded(g.may, typeSays(g.may))).
		Add("", g.within)

	d := widget.NewDialog("Guest asks to join this window")
	g.d = d
	d.Icon = icon.UserRound
	d.Body, d.Keys = g, g.key
	d.SetButtons("Allow", "Refuse")
	answer := func(allow bool) func(*gunim.UI) gunim.Intent {
		return func(*gunim.UI) gunim.Intent {
			w.answered[g.id] = true
			return app.AnswerGuest{ID: g.id, Allow: allow, CanType: mayType(g.may.Selected())}
		}
	}
	d.OnAccept, d.OnDismiss = answer(true), answer(false)
	g.setReady(false)
	return g
}

// setReady has the dialog work, or not yet.
func (g *guestAsk) setReady(on bool) {
	g.ready = on
	g.d.OK().Disabled, g.d.Cancel().Disabled, g.may.Disabled = !on, !on, !on
}

// gone reports whether the dialog has closed, or closes.
func (g *guestAsk) gone(u *gunim.UI) bool {
	return g.w.guestAsk != g || u.Presence(g.d) == gunim.Exiting
}

// hold has the dialog wait from now for nothing to be pressed, with the
// keyboard on the dialog itself. In a window without the keyboard it
// waits for that first.
func (g *guestAsk) hold(u *gunim.UI) {
	if g.stop != nil {
		g.stop()
		g.stop = nil
	}
	if g.ready {
		g.setReady(false)
		u.Focus(g.d)
	}
	u.Invalidate()
	if g.w.away {
		return
	}
	g.stop = u.After(askWait, func(u *gunim.UI) {
		g.stop = nil
		if g.gone(u) {
			return
		}
		g.setReady(true)
		u.Focus(g.d.Cancel())
		u.ShowFocusRing()
		u.Invalidate()
	})
}

// tick counts down the time there is to answer.
func (g *guestAsk) tick(u *gunim.UI) {
	u.After(time.Second, func(u *gunim.UI) {
		if g.gone(u) {
			return
		}
		g.w.showGuestAsk(u)
		u.Invalidate()
		g.tick(u)
	})
}

// Overhear implements [gunim.Overhearer]: whatever is pressed before the
// dialog works, wherever, has it wait anew.
func (g *guestAsk) Overhear(e gi.Event, u *gunim.UI) {
	switch e.(type) {
	case gi.PointerDown, gi.KeyPress:
		if !g.ready && !g.gone(u) {
			g.hold(u)
		}
	}
}

// key takes the keys the dialog's parts leave: all of them until it
// works, and Enter always, as only Allow itself allows.
func (g *guestAsk) key(e gi.Event, u *gunim.UI) bool {
	if !g.ready {
		// Text with no key press, as an input method types it.
		if _, text := e.(gi.TextInput); text {
			g.hold(u)
		}
		return true
	}
	k, press := e.(gi.KeyPress)
	return press && (k.Key == gi.KeyEnter || k.Key == gi.KeyKPEnter)
}

// Children implements [gunim.Composite].
func (g *guestAsk) Children() []gunim.Node { return []gunim.Node{g.form} }

// Focusables are what Tab moves through, once the dialog works: until
// then it keeps the keyboard itself.
func (g *guestAsk) Focusables() []gunim.Node {
	if !g.ready {
		return nil
	}
	return []gunim.Node{g.may}
}

// Layout implements [gunim.Node].
func (g *guestAsk) Layout(c gunim.Constraints, _ gunim.Frame, kids gunim.Children) geom.Size {
	k := kids.At(0)
	s := k.Layout(c)
	k.Place(geom.Point{})
	return s
}

// Paint implements [gunim.Node].
func (g *guestAsk) Paint(p *paint.Painter, _ gunim.Frame, _ geom.Size, kids gunim.Children) {
	kids.At(0).Paint(p)
}

// guestRows adds a row for each guest let into a window, under this
// computer.
func (w *Window) guestRows(rows []sideItem, st app.State) []sideItem {
	for _, l := range st.Links {
		if !l.GuestIn() {
			continue
		}
		at := slices.IndexFunc(rows, func(r sideItem) bool { return r.key == "machine:" }) + 1
		for at < len(rows) && !rows[at].heading {
			at++
		}
		note := "in this window"
		if l.Window != st.Window {
			note = "in another window"
		}
		if l.Stands == app.LinkAway {
			note = "connection lost"
		}
		item := sideItem{key: "guest:" + l.ID, text: guestCalled(l), note: note, dim: l.Stands == app.LinkAway,
			closes: app.StopLink{ID: l.ID}}
		if l.Window == st.Window {
			item.local = func(u *gunim.UI) { w.linkDialog(u) }
		}
		rows = slices.Insert(rows, at, item)
	}
	return rows
}
