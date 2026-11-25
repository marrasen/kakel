package guestview

import (
	"context"
	"errors"
	"log"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
	"github.com/marrasen/kakel/session"
)

// pacing is how long things take, which the tests shorten.
type pacing struct {
	// tries are the waits before a lost connection is made again, the
	// last of them over and over, each with up to half as much added.
	tries []time.Duration
	// held is how long a connection has to have held for the waits to
	// begin anew when it is lost.
	held time.Duration
	// dials is how many dials there may be at once, and each how long
	// between them after that, however one comes about: within what a
	// relay takes from an address, ten at once and then one each four
	// seconds, with room for a second tab.
	dials int
	each  time.Duration
	// joins is how many times a Join that does not get there is tried
	// before the guest is told.
	joins int
	// blocked is the wait after a relay said there were too many.
	blocked time.Duration
	// wakes is the least time between two tries that a wake begins, stuck
	// how long one has had to take for a wake to begin another in its
	// place, and giveUp how long a lost connection is tried for: longer
	// than the host waits.
	wakes, stuck, giveUp time.Duration
	// hidden is how long a pane off the screen stays open, kept how long
	// its terminal is kept, again how long before one that could not be
	// opened is asked for once more, twice as long each time, and
	// againMost the longest that gets.
	hidden, kept, again, againMost time.Duration
	// bells is the least time between two of a pane's bells.
	bells time.Duration
	// room is the most the terminals take, in bytes.
	room int
}

// paces are how long things take.
var paces = pacing{
	tries: []time.Duration{300 * time.Millisecond, time.Second, 2 * time.Second, 4 * time.Second, 6 * time.Second},
	held:  30 * time.Second,
	dials: 5, each: 5 * time.Second,
	joins: 3, blocked: 30 * time.Second, wakes: 3 * time.Second, stuck: 5 * time.Second, giveUp: guest.AwayFor + 5*time.Minute,
	hidden: 20 * time.Second, kept: 5 * time.Minute, again: 2 * time.Second, againMost: time.Minute,
	bells: 250 * time.Millisecond,
	room:  mostHeld,
}

// The panes open at once, and how often one is opened: well within what
// the host takes.
const (
	mostOpen   = 40
	opensBurst = 40
	opensEach  = 250 * time.Millisecond
)

// Run shows the page in w, and returns when the window closes or ctx
// ends. Every failure the guest is shown is logged.
func Run(ctx context.Context, w *gunim.Window, cfg Config) error {
	f := newFlow(ctx, w, cfg)
	defer f.close()
	gunim.RegisterView(w, "guest", func(st State) *page { return newPage(f.shells, f.cfg, st) }, (*page).Update)
	gunim.RegisterPatch(w, "guest", func(p *page, _ outputArrived, u *gunim.UI) { p.output.Arrived(u) })
	gunim.RegisterPatch(w, "guest", func(p *page, n notCopied, u *gunim.UI) { p.failed(notCopiedTitle, errors.New(n.why), u) })
	gunim.RegisterPatch(w, "guest", func(p *page, r rang, u *gunim.UI) { p.ring(r.pane, u) })
	if err := f.c.Mount(gunim.Root, "guest", "guest", f.st, topic); err != nil {
		return err
	}
	f.begin()
	return f.run()
}

// flow is the program's half of the page. All of it is its own
// goroutine's, but for what is said otherwise.
type flow struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	c      gunim.Client
	posts  chan func()
	// wrote has word that a program wrote.
	wrote  chan struct{}
	shells *screen.Shells

	st State
	// key is the guest's own, once it has one, joining that the guest has
	// clicked Join, and joined that the host has been asked.
	key     guest.Key
	keyed   bool
	joining bool
	joined  bool
	// over says it has ended, and nothing more is tried.
	over bool

	// sess is the connection there is, and dial counts the tries to make
	// one, so that what comes of an older one is let be.
	sess  Session
	dial  int
	drop  context.CancelFunc
	tries int
	// retry is the timer of the next try, nil with none; tried when the
	// last one began, and lostAt when the connection was lost.
	retry         *time.Timer
	tried, lostAt time.Time
	// madeAt is when the connection there is was made. mayDial is how many
	// dials there may be now, as of dialAt, and notBefore when there may be
	// one again, the relay having said there were too many.
	madeAt, dialAt, notBefore time.Time
	mayDial                   float64
	// said is the trouble last logged.
	said string

	// themed is the theme's bytes as last read.
	themed string

	panes  map[string]*pane
	budget *budget
	// changed says the panes are others than was published.
	changed bool
	// shown are the panes on the screen, and in the one the guest is in.
	shown []string
	in    string
	// opens is how many panes may be opened now, as of opensAt, and due
	// the timer for what has to wait.
	opens   float64
	opensAt time.Time
	due     *time.Timer
}

// pane is a terminal of the window.
type pane struct {
	id string
	// cols and rows are its size, as the host listed it last, as lists
	// has it, or as its screen was when it was last opened.
	cols, rows int
	lists      [2]int
	// sh is its terminal, on feed, and nil for one that has none: it has
	// one from when it is opened until it has been closed for a while.
	sh   *screen.Shell
	feed *feed
	// shown says it is on the screen, and since when it has not been.
	shown  bool
	hidden time.Time
	// open says it is open, and opening that it is asked for, until stop.
	// ready says its first screen is on its terminal, which is typed in
	// from then.
	open, opening, ready bool
	stop                 context.CancelFunc
	// again is when one that could not be opened is asked for once more,
	// fails how many times in a row it could not be or was cut off, and
	// openedAt when it was opened. conn is the connection it was last
	// opened over.
	again, openedAt time.Time
	fails, conn     int
	// rang says its bell rang, and is not to ring again yet.
	rang atomic.Bool
}

func newFlow(ctx context.Context, w *gunim.Window, cfg Config) *flow {
	ctx, cancel := context.WithCancel(ctx)
	f := &flow{
		cfg: cfg, ctx: ctx, cancel: cancel, c: w.Client(),
		posts: make(chan func(), 64), wrote: make(chan struct{}, 1),
		shells: screen.NewShells(), panes: map[string]*pane{},
		opens: opensBurst, opensAt: time.Now(),
		budget: newBudget(paces.room), mayDial: float64(paces.dials), dialAt: time.Now(),
	}
	if cfg.Shells != nil {
		f.shells = cfg.Shells
	}
	f.st = State{Name: cfg.Name, Theme: cfg.Theme, Own: cfg.Theme.Theme, Layout: dock.NewLayout(true)}
	switch {
	case cfg.Ended:
		f.st.Page, f.st.Over, f.over = PageOver, overGone(false), true
	case cfg.Broken:
		f.st.Page, f.st.Over, f.over = PageOver, overBroken, true
	default:
		if f.key, f.keyed = cfg.Keep.Load(); f.keyed {
			f.joined, f.st.Page, f.st.Lost, f.lostAt = true, PageWaiting, true, time.Now()
		}
	}
	return f
}

// begin begins what there is to begin: a tab that has joined dials.
func (f *flow) begin() {
	f.told()
	if f.joined && !f.over {
		f.reach()
	}
}

// run does what comes, until the window closes.
func (f *flow) run() error {
	for {
		select {
		case <-f.ctx.Done():
			return nil
		case do := <-f.posts:
			do()
		case <-f.wrote:
			if err := f.c.Patch(topic, outputArrived{}); err != nil {
				return err
			}
		case <-f.cfg.Wake:
			f.woke()
		case e, ok := <-f.c.Intents():
			if !ok {
				return nil
			}
			f.intent(e.Intent)
		}
	}
}

// close lets go of everything.
func (f *flow) close() {
	f.cancel()
	if f.sess != nil {
		_ = f.sess.Close()
	}
	for id := range f.panes {
		f.forget(id)
	}
}

// post has the flow's goroutine do something.
func (f *flow) post(do func()) {
	select {
	case f.posts <- do:
	case <-f.ctx.Done():
	}
}

// after has it do something in a while.
func (f *flow) after(d time.Duration, do func()) *time.Timer {
	return time.AfterFunc(d, func() { f.post(do) })
}

// publish shows the state.
func (f *flow) publish() {
	shut := map[string]bool{}
	for id, p := range f.panes {
		if p.sh != nil && !p.ready {
			shut[id] = true
		}
	}
	f.st.Shut = shut
	if err := f.c.Publish(topic, f.st); err != nil {
		return
	}
	f.told()
}

// told tells the program round the page what state it is in.
func (f *flow) told() {
	if f.cfg.OnState != nil {
		f.cfg.OnState(f.st)
	}
}

// intent does what the guest asked.
func (f *flow) intent(in gunim.Intent) {
	switch in := in.(type) {
	case Join:
		f.join(in.Name)
	case Leave:
		f.leave()
	case Do:
		f.act(in.Act)
	case Shown:
		f.setShown(in.Panes)
	case In:
		if in.Pane == f.in {
			return
		}
		f.in = in.Pane
		if f.sess != nil && f.st.Page == PageWindow {
			f.sess.Focus(f.in)
		}
		// It goes before the others on the screen.
		f.tend()
	case gunim.ClipboardFailed:
		if err := f.c.Patch(topic, notCopied{in.Reason}); err != nil {
			log.Printf("the clipboard could not be written: %v", in.Reason)
		}
	case gunim.FullScreenLeft:
		// The page reads how the window stands as it draws.
	}
}

// join dials, to join as name.
func (f *flow) join(name string) {
	name = strings.TrimSpace(name)
	if name == "" || f.joining || f.joined || f.over {
		return
	}
	if !f.keyed {
		key, err := guest.NewKey()
		if err != nil {
			f.trouble(err)
			return
		}
		f.key, f.keyed = key, true
	}
	if f.cfg.SaveName != nil {
		f.cfg.SaveName(name)
	}
	f.joining, f.tries = true, 0
	f.st.Name, f.st.Busy, f.st.Trouble = name, true, ""
	f.publish()
	f.reach()
}

// trouble says why a Join did not get there.
func (f *flow) trouble(err error) {
	log.Printf("couldn't join: %v", err)
	f.joining = false
	f.st.Busy, f.st.Trouble = false, troubleOf(err)
	f.publish()
}

// reach dials the host, now, or as soon as there may be another dial.
func (f *flow) reach() {
	f.stopRetry()
	if d := f.dialIn(); d > 0 {
		f.reachIn(d)
		return
	}
	if f.drop != nil {
		f.drop()
	}
	f.dial++
	n := f.dial
	f.tried = time.Now()
	ctx, cancel := context.WithCancel(f.ctx)
	f.drop = cancel
	key := f.key
	go func() {
		s, err := f.cfg.Dial(ctx, key)
		// Done with, whatever it came to: a connection does not end with
		// what it was dialled under.
		cancel()
		f.post(func() {
			switch {
			case n != f.dial || f.over:
				if err == nil {
					go func() { _ = s.Close() }()
				}
			case err != nil:
				f.missed(err)
			default:
				f.reached(s)
			}
		})
	}()
}

// stopRetry calls off the next dial there is to be.
func (f *flow) stopRetry() {
	if f.retry != nil {
		f.retry.Stop()
		f.retry = nil
	}
}

// reachIn dials the host in a while.
func (f *flow) reachIn(d time.Duration) {
	f.stopRetry()
	f.retry = f.after(d, func() {
		if f.retry != nil && !f.over {
			f.reach()
		}
	})
}

// dialIn is how long until there may be a dial, which is counted when
// that is now.
func (f *flow) dialIn() time.Duration {
	now := time.Now()
	if d := f.notBefore.Sub(now); d > 0 {
		return d
	}
	f.mayDial = min(float64(paces.dials), f.mayDial+now.Sub(f.dialAt).Seconds()/paces.each.Seconds())
	f.dialAt = now
	if f.mayDial < 1 {
		return time.Duration((1-f.mayDial)*float64(paces.each)) + time.Millisecond
	}
	f.mayDial--
	return 0
}

// missed hears that the host was not reached.
func (f *flow) missed(err error) {
	f.drop = nil
	if o, ok := ending(err, f.st.HostName, f.joined); ok {
		f.end(o, err)
		return
	}
	if tooMany(err) {
		f.notBefore = time.Now().Add(paces.blocked)
	}
	if !f.joined {
		if f.tries++; f.tries < paces.joins && !tooMany(err) {
			f.again()
			return
		}
		f.trouble(err)
		return
	}
	f.lost(err)
}

// tooMany reports whether the relay turned the guest away for trying too
// often.
func tooMany(err error) bool { return errors.Is(err, relay.RefusedTooMany) }

// lost says the connection is lost, and has it made again.
func (f *flow) lost(err error) {
	if f.lostAt.IsZero() {
		f.lostAt = time.Now()
		if f.lostAt.Sub(f.madeAt) >= paces.held {
			// One that keeps being lost is made ever less often.
			f.tries = 0
		}
	}
	if time.Since(f.lostAt) > paces.giveUp {
		f.end(overUnreached, err)
		return
	}
	if said := err.Error(); said != f.said {
		f.said = said
		log.Printf("the connection is lost: %v; trying again", err)
	}
	f.st.Lost, f.st.Why = true, ""
	var refused relay.Refused
	if errors.As(err, &refused) && refused == relay.RefusedAway && f.tries > 0 {
		f.st.Why = hostOr(f.st.HostName) + " is not connected."
	}
	f.publish()
	f.tries++
	f.again()
}

// again has the host dialled once more in a while.
func (f *flow) again() {
	d := paces.tries[min(max(f.tries-1, 0), len(paces.tries)-1)]
	// Not all at the same moment, where many lost the same relay.
	d += time.Duration(rand.Int64N(int64(d)/2 + 1))
	f.reachIn(max(d, time.Until(f.notBefore)))
}

// woke hears that the connection may be back: the next try is now, unless
// the last one was just now. One still on its way may be over a network
// that has gone, and has had its time.
func (f *flow) woke() {
	if f.over {
		return
	}
	since := time.Since(f.tried)
	if f.retry != nil && since >= paces.wakes || f.drop != nil && since >= paces.stuck {
		f.reach()
	}
}

// reached goes on with a connection to the host.
func (f *flow) reached(s Session) {
	f.drop = nil
	f.stopRetry()
	f.sess = s
	n := f.dial
	if s.View().Phase == guest.PhaseJoin {
		if !f.joining {
			// A tab that kept a key it never joined with: the host was
			// not asked, and the link is good still.
			f.letGo()
			f.joined, f.lostAt = false, time.Time{}
			f.st.Page, f.st.Lost = PageJoin, false
			f.publish()
			return
		}
		// Kept first: the moment the host has it, a reload needs it.
		f.cfg.Keep.Save(f.key)
		name := f.st.Name
		go func() {
			err := s.Join(f.ctx, name, f.cfg.About)
			f.post(func() {
				switch {
				case n != f.dial || f.over:
				case err != nil:
					f.letGo()
					f.missed(err)
				default:
					f.follow(s, n)
				}
			})
		}()
		return
	}
	f.follow(s, n)
}

// follow shows what the host says of s from here on.
func (f *flow) follow(s Session, n int) {
	f.cfg.Keep.Joined()
	f.joined, f.joining = true, false
	f.st.Busy, f.st.Trouble = false, ""
	f.lostAt, f.madeAt, f.said = time.Time{}, time.Now(), ""
	f.st.Conn++
	f.look(s)
	go func() {
		for {
			select {
			case <-s.Changed():
				f.post(func() {
					if n == f.dial && !f.over {
						f.look(s)
					}
				})
			case <-s.Done():
				f.post(func() {
					if n == f.dial && !f.over {
						f.gone(s.Err())
					}
				})
				return
			case <-f.ctx.Done():
				return
			}
		}
	}()
}

// gone hears that the connection has ended.
func (f *flow) gone(err error) {
	f.letGo()
	if err == nil {
		err = guest.ErrLost
	}
	if o, ok := ending(err, f.st.HostName, f.joined); ok {
		f.end(o, err)
		return
	}
	f.lost(err)
}

// letGo lets go of the connection, and closes what was open over it.
func (f *flow) letGo() {
	if s := f.sess; s != nil {
		f.sess = nil
		go func() { _ = s.Close() }()
	}
	for _, p := range f.panes {
		f.closePane(p)
		p.again, p.fails = time.Time{}, 0
	}
}

// end shows how it ended. What the tab kept is of no more use, unless the
// guest may come back.
func (f *flow) end(o Over, err error) {
	if err != nil {
		log.Printf("the share is over for this page: %v", err)
	}
	f.over, f.joining = true, false
	f.dial++
	f.stopRetry()
	if f.drop != nil {
		f.drop()
		f.drop = nil
	}
	f.letGo()
	if o.Act == ActNone && !errors.Is(err, guest.ErrWrongHost) {
		// What the address has of it is used up too, however soon the
		// host answered.
		f.cfg.Keep.Joined()
		f.cfg.Keep.Clear()
		f.keyed = false
	}
	f.st.Page, f.st.Over, f.st.Busy, f.st.Lost = PageOver, o, false, false
	f.publish()
}

// act does what the page at the end offers: it goes on where it was.
func (f *flow) act(a Act) {
	if !f.over || a == ActNone || a != f.st.Over.Act {
		return
	}
	f.over, f.joined, f.lostAt = false, true, time.Now()
	f.st.Page, f.st.Lost, f.st.Why = PageWaiting, true, ""
	if f.st.Title != "" || len(f.st.Panes) > 0 {
		f.st.Page = PageWindow
	}
	f.publish()
	f.reach()
}

// leave leaves the share.
func (f *flow) leave() {
	if f.over {
		return
	}
	o := overOf(guest.ReasonLeft, f.st.HostName)
	if f.st.Page == PageWaiting && !f.st.Lost {
		o = overCancelled
	}
	// Where nothing reaches the host, it lets go of the guest by itself.
	if s := f.sess; s != nil && f.joined {
		// Told in its own time, and the page goes on at once.
		f.sess = nil
		go s.Leave()
	}
	f.end(o, nil)
}

// look shows what the host says now.
func (f *flow) look(s Session) {
	v := s.View()
	switch v.Phase {
	case guest.PhaseJoin, guest.PhaseOver:
		// The end comes with its reason.
		return
	case guest.PhaseWaiting:
		f.st.Page, f.st.Lost, f.st.Why = PageWaiting, false, ""
	case guest.PhaseIn:
		came := f.st.Page != PageWindow || f.st.Lost
		f.window(v.State)
		f.st.Page, f.st.Lost, f.st.Why = PageWindow, false, ""
		if came && f.in != "" {
			s.Focus(f.in)
		}
	}
	f.publish()
	f.tend()
}

// window takes the window as the host says it is.
func (f *flow) window(st guest.State) {
	f.st.HostName, f.st.Title, f.st.CanType, f.st.HostIn = st.HostName, st.Title, st.CanType, st.HostIn
	f.st.Panes = st.Panes
	if raw := string(st.Theme); raw != "" && raw != f.themed {
		f.themed = raw
		if th, err := guestlook.ReadTheme(st.Theme); err != nil {
			log.Printf("the host's theme could not be read: %v", err)
		} else {
			f.st.Theme, f.st.Own, f.st.Themes = th.Look, th.Look.Plain, f.st.Themes+1
			if !th.Readable() {
				// What the page says itself is always read.
				f.st.Own = f.cfg.Theme.Theme
			}
			if f.cfg.Still {
				f.st.Theme, f.st.Own = Still(th.Look), still(f.st.Own)
			}
			for _, p := range f.panes {
				if p.sh != nil {
					p.sh.SetPalette(f.st.Theme.Palette)
				}
			}
		}
	}
	here := map[string]bool{}
	var all []string
	for _, p := range st.Panes {
		here[p.ID] = true
		all = append(all, p.ID)
		if p.Kind != guest.KindTerminal {
			continue
		}
		n := f.panes[p.ID]
		if n == nil {
			n = &pane{id: p.ID}
			f.panes[p.ID] = n
		}
		if lists := [2]int{p.Cols, p.Rows}; lists != n.lists {
			n.lists, n.cols, n.rows, n.fails = lists, p.Cols, p.Rows, 0
		}
	}
	for id := range f.panes {
		if !slices.ContainsFunc(st.Panes, func(p guest.Pane) bool { return p.ID == id && p.Kind == guest.KindTerminal }) {
			f.forget(id)
		}
	}
	l, err := guestlook.ReadLayout(st.Layout)
	if err != nil {
		log.Printf("the host's layout could not be read: %v", err)
	}
	l.Mend(func(id string) bool { return here[id] })
	if len(l.Items()) == 0 && len(all) > 0 {
		// None to go by: all in one row of tabs.
		l = oneGroup(all, st.HostIn, "")
	}
	f.st.Layout = l
}

// oneGroup is a layout of one group, named group, with a tab for each of
// ids, and front in front.
func oneGroup(ids []string, front, group string) dock.Layout {
	root := &dock.Node{ID: group, Kind: dock.Group, Front: front}
	for _, id := range ids {
		root.Kids = append(root.Kids, &dock.Node{ID: id, Kind: dock.Item})
	}
	l := dock.Layout{Grouped: true, Windows: []dock.Window{{ID: guestlook.LayoutWindow, Keep: true, Root: root}}}
	l.Mend(nil)
	return l
}

// closePane closes a pane, and leaves its terminal as it is.
func (f *flow) closePane(p *pane) {
	if p.ready {
		p.ready, f.changed = false, true
	}
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	if p.feed != nil {
		p.feed.detach()
	}
	p.open, p.opening = false, false
}

// size is how large a pane is: its terminal, or as it was last heard to
// be.
func (f *flow) size(p *pane) (cols, rows int) {
	if p.sh != nil {
		r := f.budget.of(p.id)
		return r.cols, r.rows
	}
	return p.cols, p.rows
}

// makeTerminal makes the terminal of a pane, in the room it was given.
func (f *flow) makeTerminal(p *pane) {
	fd, id, r := newFeed(), p.id, f.budget.of(p.id)
	fd.fits = func(cols, rows int) bool { return f.budget.resize(id, cols, rows) }
	fd.onEnd = func(tooLarge bool) { f.post(func() { f.paneEnded(p, fd, tooLarge) }) }
	fd.onReady = func(untyped bool) {
		f.post(func() {
			if p.feed != fd {
				return
			}
			if untyped {
				f.st.Untyped = maps.Clone(f.st.Untyped)
				if f.st.Untyped == nil {
					f.st.Untyped = map[string]int{}
				}
				f.st.Untyped[id]++
			}
			p.ready = p.open
			f.shells.Set(id, p.sh)
			f.publish()
		})
	}
	// none is a limit of nothing.
	none := func(n int) int {
		if n == 0 {
			return -1
		}
		return n
	}
	p.feed = fd
	p.sh = screen.Open(fd, f.st.Theme.Palette, screen.Hooks{
		Output: func() {
			// One word is enough, however much was written.
			select {
			case f.wrote <- struct{}{}:
			default:
			}
		},
		// Nothing of a guest's terminal acts on this machine.
		Title: func(string) {}, Exit: func() {}, Clipboard: func(string) {},
		Bell:       func() { f.ring(p) },
		Scrollback: none(r.lines), Images: none(r.images), Links: none(r.links),
	})
}

// ring hears a pane's bell, from any goroutine. However often a program
// rings it, the page hears of it now and then, and of none off the screen.
func (f *flow) ring(p *pane) {
	if !p.rang.CompareAndSwap(false, true) {
		return
	}
	go f.post(func() {
		if p.shown && f.panes[p.id] == p {
			_ = f.c.Patch(topic, rang{p.id})
		}
		time.AfterFunc(paces.bells, func() { p.rang.Store(false) })
	})
}

// dropTerminal lets go of a pane's terminal, with its history.
func (f *flow) dropTerminal(p *pane) {
	f.closePane(p)
	if p.sh == nil {
		return
	}
	// It waits for what reads it.
	go p.sh.Close()
	p.sh, p.feed = nil, nil
	f.shells.Set(p.id, nil)
	f.budget.drop(p.id)
	f.changed = true
}

// forget closes a pane for good.
func (f *flow) forget(id string) {
	if p := f.panes[id]; p != nil {
		f.dropTerminal(p)
		delete(f.panes, id)
	}
}

// setShown hears which panes are on the screen.
func (f *flow) setShown(ids []string) {
	f.shown = ids
	f.tend()
}

// paneEnded hears that a pane was cut off, which one that still stands in
// the window is opened again after. One whose screen outgrew the room its
// terminal was given begins anew, in the room there is.
func (f *flow) paneEnded(p *pane, fd *feed, tooLarge bool) {
	if p.feed != fd {
		return
	}
	f.closePane(p)
	if tooLarge {
		f.dropTerminal(p)
	}
	if time.Since(p.openedAt) >= paces.held {
		p.fails = 0
	}
	p.failed()
	f.tend()
}

// failed puts off the next time a pane is asked for, ever longer.
func (p *pane) failed() {
	d := paces.again
	for range p.fails {
		d = min(2*d, paces.againMost)
	}
	p.fails++
	p.again = time.Now().Add(d)
}

// wanted are the panes on the screen there is room for, the one the guest
// is in before the others, and held those there is none for.
func (f *flow) wanted() (wanted []*pane, held map[string]Held) {
	order := slices.Clone(f.shown)
	if i := slices.Index(order, f.in); i > 0 {
		order = slices.Insert(slices.Delete(order, i, i+1), 0, f.in)
	}
	held = map[string]Held{}
	room := f.budget.most
	for _, id := range order {
		p := f.panes[id]
		if p == nil {
			continue
		}
		size := screenBytes(f.size(p))
		switch {
		case size > room:
			held[id] = HeldLarge
		case len(wanted) == mostOpen:
			held[id] = HeldMany
		default:
			room -= size
			wanted = append(wanted, p)
		}
	}
	return wanted, held
}

// tend opens the panes on the screen, closes those that have been off it
// for a while, and lets go of their terminals a while after.
func (f *flow) tend() {
	if f.due != nil {
		f.due.Stop()
		f.due = nil
	}
	if f.sess == nil || f.st.Page != PageWindow || f.over {
		f.tell()
		return
	}
	now := time.Now()
	next := time.Duration(-1)
	soon := func(at time.Time) {
		if d := max(at.Sub(now), time.Millisecond); next < 0 || d < next {
			next = d
		}
	}
	for _, p := range f.panes {
		was := p.shown
		p.shown = slices.Contains(f.shown, p.id)
		if was && !p.shown {
			p.hidden = now
		}
	}
	wanted, held := f.wanted()
	if !maps.Equal(held, f.st.Held) {
		f.st.Held, f.changed = held, true
	}
	open := len(wanted)
	var idle []*pane
	for _, p := range f.panes {
		switch {
		case slices.Contains(wanted, p):
		case p.shown:
			f.closePane(p)
		case p.open || p.opening:
			idle = append(idle, p)
			open++
		case p.sh != nil:
			if until := p.hidden.Add(paces.kept); until.After(now) {
				soon(until)
			} else {
				f.dropTerminal(p)
			}
		}
	}
	slices.SortFunc(idle, func(a, b *pane) int { return a.hidden.Compare(b.hidden) })
	for _, p := range idle {
		until := p.hidden.Add(paces.hidden)
		if until.After(now) && open <= mostOpen {
			soon(until)
			continue
		}
		f.closePane(p)
		open--
		if p.sh != nil {
			soon(p.hidden.Add(paces.kept))
		}
	}
	f.keepFew()
	for _, p := range wanted {
		switch {
		case p.open || p.opening:
		case p.again.After(now):
			soon(p.again)
		case !f.mayOpen(now):
			soon(now.Add(opensEach))
		default:
			f.openPane(p)
		}
	}
	if next >= 0 {
		f.due = f.after(next, f.tend)
	}
	f.tell()
}

// keepFew lets go of the terminals kept with their panes closed past as
// many as are, those closed the longest first.
func (f *flow) keepFew() {
	var kept []*pane
	for _, p := range f.panes {
		if p.sh != nil && !p.shown && !p.open && !p.opening {
			kept = append(kept, p)
		}
	}
	slices.SortFunc(kept, func(a, b *pane) int { return b.hidden.Compare(a.hidden) })
	for i := mostKept; i < len(kept); i++ {
		f.dropTerminal(kept[i])
	}
}

// tell shows the state, where the panes are others than it showed.
func (f *flow) tell() {
	if f.changed {
		f.changed = false
		f.publish()
	}
}

// mayOpen reports whether a pane may be opened now, and counts it.
func (f *flow) mayOpen(now time.Time) bool {
	f.opens = min(opensBurst, f.opens+now.Sub(f.opensAt).Seconds()/opensEach.Seconds())
	f.opensAt = now
	if f.opens < 1 {
		return false
	}
	f.opens--
	return true
}

// openPane opens a pane over the connection there is, and reads its first
// screen, which says how large it is.
func (f *flow) openPane(p *pane) {
	p.opening = true
	ctx, stop := context.WithCancel(f.ctx)
	p.stop = stop
	s, n := f.sess, f.dial
	go func() {
		var first *session.Screen
		got, err := s.Pane(ctx, p.id)
		if err == nil {
			// Whatever is slow, it is closed when it is let go of.
			done := context.AfterFunc(ctx, func() { _ = got.Close() })
			first, err = firstScreen(got)
			done()
		}
		f.post(func() {
			if n != f.dial || f.panes[p.id] != p || f.sess != s || ctx.Err() != nil {
				stop()
				if got != nil {
					_ = got.Close()
				}
				return
			}
			// Done with: the pane ends as it is closed.
			stop()
			p.stop, p.opening, p.again = nil, false, time.Now().Add(paces.again)
			switch {
			case err != nil:
				if got != nil {
					_ = got.Close()
				}
				p.failed()
				// A connection that went says so itself.
				if !errors.Is(err, guest.ErrLost) {
					log.Printf("a pane could not be shown: %v", err)
				}
			case !f.place(p, first.Cols, first.Rows):
				_ = got.Close()
			default:
				if p.sh == nil {
					f.makeTerminal(p)
				}
				p.open, p.openedAt = true, time.Now()
				fresh := p.conn != n
				p.conn = n
				p.feed.attach(got, first, fresh)
			}
			f.tend()
		})
	}()
}

// firstScreen reads the first screen of a pane.
func firstScreen(p session.ScreenReader) (*session.Screen, error) {
	b := make([]byte, 4<<10)
	for {
		// What comes before it is past.
		_, scr, err := p.ReadScreen(b)
		if err != nil {
			return nil, err
		}
		if scr != nil && scr.Cols > 0 && scr.Rows > 0 {
			return scr, nil
		}
	}
}

// place gives a pane with a screen of cols by rows room for a terminal,
// and reports whether there is that much: it is one of those wanted, as
// large as it is.
func (f *flow) place(p *pane, cols, rows int) bool {
	p.cols, p.rows = cols, rows
	if p.sh != nil {
		if f.budget.resize(p.id, cols, rows) {
			return true
		}
		// It begins anew, with less history.
		sh := p.sh
		p.sh, p.feed = nil, nil
		go sh.Close()
		f.shells.Set(p.id, nil)
		f.budget.drop(p.id)
		f.changed = true
	}
	wanted, _ := f.wanted()
	if !slices.Contains(wanted, p) {
		return false
	}
	need := screenBytes(cols, rows)
	for f.budget.free(p.id) < need {
		q := f.least(p, wanted)
		if q == nil {
			return false
		}
		f.dropTerminal(q)
	}
	// What is left is shared by those wanted, and one more to come.
	rest, n := f.budget.free(p.id)-need, 2
	for _, q := range wanted {
		if q != p && q.sh == nil {
			rest -= screenBytes(f.size(q))
			n++
		}
	}
	return f.budget.take(p.id, roomWith(cols, rows, max(rest, 0)/n))
}

// least is the pane to let go of the terminal of for p: one off the
// screen, the longest there first, then one that is not wanted, and then
// the one with the most besides its screen, which is opened again with
// less. Nil is none.
func (f *flow) least(p *pane, wanted []*pane) *pane {
	var least *pane
	rank := func(q *pane) (kind, by int) {
		switch {
		case !q.shown:
			return 2, int(time.Since(q.hidden) / time.Millisecond)
		case !slices.Contains(wanted, q):
			return 1, 0
		}
		return 0, f.budget.of(q.id).spare()
	}
	for _, q := range f.panes {
		if q == p || q.sh == nil {
			continue
		}
		kind, by := rank(q)
		if kind == 0 && by == 0 {
			continue
		}
		if least != nil {
			if k, b := rank(least); kind < k || kind == k && by <= b {
				continue
			}
		}
		least = q
	}
	return least
}
