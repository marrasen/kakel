package guest

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"golang.org/x/crypto/ssh"
)

// sized is the state with the guest setting the size, or not.
func sized(on bool, ids ...string) State {
	st := terminals(true, ids...)
	st.GuestSizes = on
	return st
}

// sizeOf is the size the host is told pane id has, and whether it has one.
func (r *rig) sizeOf(id string) (Cells, bool) {
	c, ok := r.host.Status().Sizes[id]
	return c, ok
}

// open is pane id, shown to the guest last let in.
func (r *rig) open(id string) *ShownPane {
	r.t.Helper()
	p, err := r.last.Pane(context.Background(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, p) }()
	return p
}

// onPage has the guest last let in say ids are on its page, and waits for
// the host to have it.
func (r *rig) onPage(ids ...string) {
	r.t.Helper()
	r.last.Shown(ids)
	r.until("the host hears what is on the page", func() bool {
		r.host.mu.Lock()
		defer r.host.mu.Unlock()
		if len(r.host.onPage) != len(ids) {
			return false
		}
		for _, id := range ids {
			if !r.host.onPage[id] {
				return false
			}
		}
		return true
	})
}

// through types on p after what was sent on it before, and waits for src
// to have it: all that came before has been looked at.
func (r *rig) through(p *ShownPane, src *recorder) {
	r.t.Helper()
	was := src.got()
	if _, err := p.Write([]byte(".")); err != nil {
		r.t.Fatal(err)
	}
	r.until("what was typed after", func() bool { return src.got() > was })
}

// The host takes the guest's sizes only while the share has the guest set
// them, and only of panes on its page, and gives its own back as the
// choice changes.
func TestTheGuestSetsTheSizeOnlyWhenItMay(t *testing.T) {
	r := newRig(t, false)
	src := &recorder{}
	r.share("a", src)
	r.publish(sized(false, "a"))
	r.in(key(t))
	p := r.open("a")
	r.onPage("a")

	p.Fit(120, 40)
	r.through(p, src)
	if c, ok := r.sizeOf("a"); ok {
		t.Fatalf("a size of %v was taken while the host sets the size", c)
	}

	r.publish(sized(true, "a"))
	r.onPage()
	p.Fit(110, 40)
	r.through(p, src)
	if c, ok := r.sizeOf("a"); ok {
		t.Fatalf("a size of %v was taken for a pane not on the page", c)
	}
	r.onPage("a")
	p.Fit(120, 40)
	r.until("the guest's size is taken", func() bool { c, _ := r.sizeOf("a"); return c == Cells{120, 40} })
	p.Fit(0, 0)
	r.until("the host's size is back", func() bool { _, ok := r.sizeOf("a"); return !ok })

	p.Fit(90, 20)
	r.until("the guest's size is taken", func() bool { c, _ := r.sizeOf("a"); return c == Cells{90, 20} })
	r.publish(sized(false, "a"))
	if _, ok := r.sizeOf("a"); ok {
		t.Error("the guest's size stayed as the host took the size back")
	}
}

// A size goes with the pane: as it leaves the window, as the guest closes
// it, and as the share ends.
func TestASizeGoesWithThePane(t *testing.T) {
	r := newRig(t, false)
	r.share("a", &recorder{})
	r.share("b", &recorder{})
	r.publish(sized(true, "a", "b"))
	r.in(key(t))
	a, b := r.open("a"), r.open("b")
	r.onPage("a", "b")
	a.Fit(100, 30)
	b.Fit(70, 20)
	r.until("both are sized", func() bool { return len(r.host.Status().Sizes) == 2 })
	r.onPage("a")
	if _, ok := r.sizeOf("b"); ok {
		t.Error("a pane off the page kept the guest's size")
	}
	b.Fit(75, 20)
	r.onPage("a", "b")
	r.until("b on the page again is sized", func() bool { c, _ := r.sizeOf("b"); return c == Cells{75, 20} })

	r.publish(sized(true, "a"))
	if _, ok := r.sizeOf("b"); ok {
		t.Error("a pane that left the window kept the guest's size")
	}
	_ = a.Close()
	r.until("a closed pane has the host's size", func() bool { _, ok := r.sizeOf("a"); return !ok })

	a = r.open("a")
	a.Fit(100, 30)
	r.until("it is sized again", func() bool { _, ok := r.sizeOf("a"); return ok })
	r.host.End(ReasonStopped)
	if st := r.host.Status(); len(st.Sizes) != 0 {
		t.Errorf("a share that ended has sizes %v", st.Sizes)
	}
	r.mu.Lock()
	last := r.statuss[len(r.statuss)-1]
	r.mu.Unlock()
	if last.Phase == PhaseOver && len(last.Sizes) != 0 {
		t.Errorf("the end is told with sizes %v", last.Sizes)
	}
}

// A guest whose connection is lost keeps its sizes a while, and once it is
// gone longer, or back without saying them again, the host's are back.
func TestALostGuestsSizesGoAfterAWhile(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t, true)
		k := key(t)
		var mu sync.Mutex
		var line *memConn
		was := r.dial
		r.dial = func() (c net.Conn) {
			c = was()
			mu.Lock()
			line = c.(*memConn)
			mu.Unlock()
			return c
		}
		r.share("a", &recorder{})
		r.publish(sized(true, "a"))
		s := r.in(k)
		p := r.open("a")
		r.onPage("a")
		p.Fit(100, 30)
		synctest.Wait()
		if _, ok := r.sizeOf("a"); !ok {
			t.Fatal("the size was not taken")
		}
		mu.Lock()
		line.cut()
		mu.Unlock()
		<-s.Done()
		for r.host.Status().Here {
			time.Sleep(time.Second)
		}
		time.Sleep(fitsAwayFor / 2)
		if _, ok := r.sizeOf("a"); !ok {
			t.Error("the size went as soon as the guest was lost")
		}
		// Back, and the pane is not said again.
		if _, err := r.guest(k); err != nil {
			t.Fatal(err)
		}
		time.Sleep(fitsAwayFor)
		synctest.Wait()
		if _, ok := r.sizeOf("a"); ok {
			t.Error("the size stayed though the guest never said it again")
		}
		r.stop()
	})
}

// However fast a guest says sizes, a pane is sized no more often than
// fitEvery, and to the latest.
func TestSizesAreHeldToNoMoreOftenThanFitEvery(t *testing.T) {
	r := newRig(t, false)
	r.share("a", &recorder{})
	r.publish(sized(true, "a"))
	k := key(t)
	_ = r.in(k).Close()
	conn, err := r.raw(signer(t, k))
	if err != nil {
		t.Fatal(err)
	}
	ctl, reqs, err := conn.OpenChannel(chanControl, nil)
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(io.Discard, ctl) }()
	r.until("it is here", func() bool { return r.host.Status().Here })
	ch, reqs, err := conn.OpenChannel(chanPane, ssh.Marshal(openPane{"a"}))
	if err != nil {
		t.Fatal(err)
	}
	go ssh.DiscardRequests(reqs)
	go func() { _, _ = io.Copy(io.Discard, ch) }()
	if _, err := ctl.Write(frame(frameShown, []byte("a"))); err != nil {
		t.Fatal(err)
	}
	r.until("a is on the page", func() bool { r.host.mu.Lock(); defer r.host.mu.Unlock(); return r.host.onPage["a"] })

	r.mu.Lock()
	r.statuss = nil
	r.mu.Unlock()
	start := time.Now()
	// 2000 frames of 9 bytes.
	const n = 2000
	for i := range n {
		if _, err := ch.Write(fitFrame(Cells{Cols: 20 + i%500, Rows: 10 + i%50})); err != nil {
			t.Fatal(err)
		}
	}
	last := Cells{Cols: 20 + (n-1)%500, Rows: 10 + (n-1)%50}
	r.until("the latest is held to", func() bool { c, _ := r.sizeOf("a"); return c == last })
	took := time.Since(start)
	r.mu.Lock()
	defer r.mu.Unlock()
	sizes := 0
	var was Cells
	for _, st := range r.statuss {
		if c := st.Sizes["a"]; c != was {
			sizes++
			was = c
		}
	}
	if most := int(took/fitEvery) + 2; sizes > most {
		t.Errorf("the pane was sized %d times in %v, more than %d", sizes, took, most)
	}
}

// However fast a guest says a pane is on its page and off it, the pane is
// sized no more often than the sizes themselves are.
func TestAPageThatFlickersIsPaced(t *testing.T) {
	r := newRig(t, false)
	src := &recorder{}
	r.share("a", src)
	r.publish(sized(true, "a"))
	r.in(key(t))
	p := r.open("a")
	r.onPage("a")
	p.Fit(100, 30)
	r.until("sized", func() bool { _, ok := r.sizeOf("a"); return ok })
	r.mu.Lock()
	r.statuss = nil
	r.mu.Unlock()
	start := time.Now()
	// 2000 frames of a few bytes, each said straight away.
	for i := range 2000 {
		if err := r.last.say(frame(frameShown, []byte([]string{"a", ""}[i%2]))); err != nil {
			t.Fatal(err)
		}
	}
	r.onPage("a")
	r.until("sized at the end", func() bool { _, ok := r.sizeOf("a"); return ok })
	took := time.Since(start)
	r.mu.Lock()
	defer r.mu.Unlock()
	changes := 0
	had := true
	for _, st := range r.statuss {
		if _, has := st.Sizes["a"]; has != had {
			changes++
			had = has
		}
	}
	if most := 2*int(took/fitEvery) + 4; changes > most {
		t.Errorf("the pane was sized and unsized %d times in %v, more than %d", changes, took, most)
	}
}
