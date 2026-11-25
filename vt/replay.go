package vt

import (
	"bytes"
	"fmt"
	"slices"
	"strings"

	vte "github.com/marrasen/go-vte"

	"github.com/marrasen/kakel/grid"
)

// Replay is the escape sequences that would bring another emulator of
// this size to where this one stands: the live screen, the one a
// full-screen program covers, the modes, the cursors, the title, and the
// start of a sequence the program is part way through.
//
// What is written to this terminal next, written there as well, leaves
// the two alike. Three things do not travel: the scrollback, what an
// alternate screen nobody is looking at holds, and what the shell said
// about its folder and its commands.
//
// whole is false when the program is part way through a sequence too
// long to send, as one that carries an image. It is left out, so the two
// part once it ends, and whoever asked has to ask again after that, when
// AtRest says so.
func (t *Terminal) Replay() (seq string, whole bool) {
	s := t.scr
	// Reverse video is a mode of the far end's too, so the cells travel as
	// they are.
	was := s.mode.ReverseVid
	s.mode.ReverseVid = false
	g := grid.New(s.cols, s.rows, s.palette.FG, s.palette.BG)
	t.RenderLive(g)
	full := t.Screenful()
	if full.Alt {
		// And the screen the full-screen program is covering, so that the
		// watcher has something to go back to when it quits.
		under := grid.New(s.cols, s.rows, s.palette.FG, s.palette.BG)
		if t.RenderUnder(under) {
			full.Under = under
		}
	}
	s.mode.ReverseVid = was
	// The images of the ordinary screen, which travel as the escape
	// sequence that draws one: the watcher's emulator reads it, so nothing
	// new is needed on the wire.
	full.Images = t.LivePlaced()
	tail, whole := t.unfinished()
	full.rest = &rest{
		rows: s.rows, top: s.top, bot: s.bot,
		mode: s.mode, cursor: s.cursor, saved: s.saved,
		tabs: s.tabs, title: t.title, last: t.lastRune,
		url: t.LinkURL, tail: tail,
	}
	full.rest.mode.ReverseVid = was
	return Repaint(g, full), whole
}

// rest is what a Screenful holds of a terminal besides what its own
// fields say, for an emulator that carries on from it.
type rest struct {
	rows, top, bot int
	mode           modes
	cursor         cursorState
	saved          [2]cursorState
	tabs           []bool
	title          string
	last           rune
	url            func(uint32) string
	tail           []byte
}

// link is the sequence that puts a hyperlink under what is written next.
// A pen's reset takes one off, so there is none that only ends one.
func (r *rest) link(id uint32) string {
	if r == nil || id == 0 {
		return ""
	}
	url := r.url(id)
	if url == "" {
		return ""
	}
	return "\x1b]8;;" + url + "\x07"
}

// clean is what makes it safe to draw on the far end: nothing set that
// changes where cells land or what they look like.
const clean = "\x1b[0m\x1b[r\x1b[?6l\x1b[4l\x1b[?7h"

// writeSaved has the far end save c as its cursor, on the screen g, and
// leaves the pen and the origin reset. The scroll region is the whole
// screen.
func (r *rest) writeSaved(b *strings.Builder, g *grid.Grid, c cursorState) {
	r.writeCursorState(b, g, c, 0)
	b.WriteString("\x1b7\x1b[?6l\x1b[0m")
}

// writeSavedBoth has the far end, which is on the screen g, save the
// cursors this terminal has saved. With alt, g is the alternate screen,
// and the ordinary screen's was saved on the way to it.
func (r *rest) writeSavedBoth(b *strings.Builder, g *grid.Grid, alt bool) {
	if alt {
		r.writeSaved(b, g, r.saved[1])
		return
	}
	r.writeSaved(b, g, r.saved[0])
	c := r.saved[1]
	if c.X == 0 && c.Y == 0 && !c.WrapNext && !c.Origin && c.Pen.Link == 0 && sameStyle(c.Pen, g.Blank()) {
		return
	}
	b.WriteString("\x1b[?1047h")
	r.writeSaved(b, g, c)
	b.WriteString("\x1b[?47l")
}

// writeCursorState puts the far end's cursor where c has it, with its pen,
// in a scroll region that begins at row top. A cursor that counts from a
// region it stands outside of, as one restored from before the region was
// set does, is put in its first row: nothing can put it back where it was.
func (r *rest) writeCursorState(b *strings.Builder, g *grid.Grid, c cursorState, top int) {
	y := c.Y
	if c.Origin {
		b.WriteString("\x1b[?6h")
		y = max(y-top, 0)
	}
	cols, _ := g.Size()
	if c.WrapNext && c.X == cols-1 {
		writeOwedWrap(b, g, r, c.X, c.Y, y)
	} else {
		fmt.Fprintf(b, "\x1b[%d;%dH", y+1, c.X+1)
	}
	b.WriteString(sgr(c.Pen, g))
	b.WriteString(r.link(c.Pen.Link))
}

// writeModes sets every mode the program has set, and resets the ones it
// has not.
func (r *rest) writeModes(b *strings.Builder) {
	m := r.mode
	for _, p := range []struct {
		on  bool
		num int
	}{
		{m.ReverseVid, 5}, {m.Wrap, 7},
		{m.MouseClick, 1000}, {m.MouseDrag, 1002}, {m.MouseMotion, 1003},
		{m.FocusEvents, 1004}, {m.MouseSGR, 1006}, {m.Bracketed, 2004},
	} {
		set := 'l'
		if p.on {
			set = 'h'
		}
		fmt.Fprintf(b, "\x1b[?%d%c", p.num, set)
	}
	if m.Insert {
		b.WriteString("\x1b[4h")
	}
	if m.AppKeypad {
		b.WriteString("\x1b=")
	} else {
		b.WriteString("\x1b>")
	}
}

// writeTabs sets the tab stops, when a program has moved them.
func (r *rest) writeTabs(b *strings.Builder) {
	own := false
	for i, on := range r.tabs {
		own = own || on != (i > 0 && i%8 == 0)
	}
	if !own {
		return
	}
	b.WriteString("\x1b[3g")
	for i, on := range r.tabs {
		if on {
			fmt.Fprintf(b, "\x1b[%dG\x1bH", i+1)
		}
	}
}

// writeEnd is the last of a replay, once everything is drawn.
func (r *rest) writeEnd(b *strings.Builder, g *grid.Grid) {
	r.writeTabs(b)
	if r.top != 0 || r.bot != r.rows-1 {
		fmt.Fprintf(b, "\x1b[%d;%dr", r.top+1, r.bot+1)
	}
	r.writeCursorState(b, g, r.cursor, r.top)
	r.writeModes(b)
	// The character REP repeats, which has no sequence of its own.
	fmt.Fprintf(b, "\x1b]1338;last;%d\x07", r.last)
	b.WriteString("\x1b]2;" + r.title + "\x07")
	b.Write(r.tail)
}

// Adopt empties the terminal, makes it cols by rows, and writes seq, as
// Replay gave it. What has scrolled off stays in its history.
//
// Nothing of what it held besides carries over, a sequence half read
// among it: seq is the whole of what it shows from now on.
func (t *Terminal) Adopt(cols, rows int, seq []byte) {
	t.parser = vte.NewParser(t)
	t.long = longOSC{most: t.long.most}
	t.tail = nil
	t.lastRune = 0
	if t.scr.OnAltBuffer() {
		// Back on the ordinary screen first, for the resize to re-wrap it
		// round its own cursor.
		t.scr.UseAltBuffer(false, false)
		t.scr.cursor = t.scr.priCur
	}
	t.Resize(cols, rows)
	t.scr.renew()
	// The sequence clears the screen to draw it, which is not the program
	// having cleared it: a taller screen still takes back what it did.
	floor := t.scr.floor
	_, _ = t.Write(seq)
	t.scr.floor = floor
}

// renew empties the screen and gives it the modes it began with, keeping
// its history.
func (s *Screen) renew() {
	s.cur = s.pri
	blank := s.blank()
	for _, b := range []*buffer{s.pri, s.alt} {
		for _, l := range b.lines {
			for i := range l {
				l[i] = blank
			}
		}
	}
	s.cursor = cursorState{Pen: blank}
	s.saved = [2]cursorState{s.cursor, s.cursor}
	s.priCur = s.cursor
	s.top, s.bot = 0, s.rows-1
	s.mode = modes{Wrap: true, CursorVis: true}
	s.curStyle, s.curBlink = grid.CursorBlock, true
	s.resetTabs()
	s.clampScrollOff()
	s.touchAll()
}

// mostTail is how much is kept of a sequence the program is part way
// through. A parser keeps a kilobyte of an OSC and thirty-two parameters
// of a CSI, and nothing of any other string, so the start of one says as
// much as all of it.
const mostTail = 8 << 10

// mostLongTail is how much of a sequence read by longOSC is sent.
const mostLongTail = 64 << 10

// quiet begins a sequence of kakel's own that says nothing.
const quiet = "\x1b]1338;"

// advance hands bytes to the parser, and keeps the start of a sequence
// they leave unfinished.
func (t *Terminal) advance(p []byte) {
	t.parser.AdvanceBytes(p)
	switch t.parser.State() {
	case vte.GroundState:
		t.tail = t.tail[:0]
		return
	case vte.Utf8State:
		// A character cut in two. Its first byte is the last one that can
		// begin one.
		for i := len(p) - 1; i >= 0; i-- {
			if p[i] >= 0xc0 {
				t.tail, p = t.tail[:0], p[i:]
				break
			}
		}
	default:
		// A sequence. An ESC always begins one, whatever it is inside.
		if i := bytes.LastIndexByte(p, 0x1b); i >= 0 {
			t.tail, p = t.tail[:0], p[i:]
		}
	}
	for _, b := range p {
		// A control character inside a sequence is acted on as it comes and
		// is no part of it: read again, it would act twice.
		if len(t.tail) < mostTail && (b >= 0x20 || b == 0x1b) {
			t.tail = append(t.tail, b)
		}
	}
}

// AtRest reports whether the program is between sequences, rather than
// part way through one.
func (t *Terminal) AtRest() bool {
	return t.long.num == nil && len(t.long.lead) == 0 && t.parser.State() == vte.GroundState
}

// unfinished is what another emulator is written to be part way through
// the same sequence as this one, and whether that is all of it.
func (t *Terminal) unfinished() (tail []byte, whole bool) {
	// The parser may be part way through one of its own besides: longOSC
	// takes a sequence out from under it.
	if t.parser.State() != vte.GroundState {
		tail = slices.Clone(t.tail)
	}
	for _, m := range longOSCMarkers {
		if len(tail) > 0 && (bytes.HasPrefix(tail, m) || bytes.HasPrefix(m, tail)) {
			// One the parser was handed though longOSC would have taken it,
			// as it is when it comes in the middle of another: so it comes
			// in the middle of one that says nothing.
			tail = append([]byte(quiet), tail...)
			break
		}
	}
	if t.long.num == nil {
		return append(tail, t.long.lead...), true
	}
	if whole = !t.long.over && len(t.long.body) <= mostLongTail; whole {
		tail = fmt.Appendf(tail, "\x1b]%s;%s", t.long.num, t.long.body)
	} else {
		// For the rest to be read into rather than printed.
		tail = append(tail, quiet...)
	}
	if t.long.esc {
		tail = append(tail, 0x1b)
	}
	return tail, whole
}
