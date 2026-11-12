package serve

import (
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/kakel/session"
	"golang.org/x/crypto/ssh"
)

// A layout's pane is asked for in a folder: the folder crosses for a
// terminal too, what cannot be opened is refused with the reason rather
// than said in a pane, on one line, and a folder not there is refused
// as such.
func TestAPaneIsOpenedInAFolder(t *testing.T) {
	type asked struct{ host, command, dir string }
	got := make(chan asked, 1)
	_, w := takenOverServing(t, Config{OpenIn: func(host, command, dir string, cols, rows int) (session.Session, Attached, error) {
		got <- asked{host, command, dir}
		switch dir {
		case "/gone":
			return nil, Attached{}, ErrNoFolder
		case "/shut":
			return nil, Attached{}, errors.New("srv is not\r\n  connected\x1b here")
		}
		return newEchoSession(cols, rows), Attached{ID: "p9", Kind: "Terminal"}, nil
	}})
	sess, err := w.OpenIn("", "", "/src", 90, 30, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	read(t, sess, "started at 90x30")
	if a := <-got; a != (asked{"", "", "/src"}) {
		t.Fatalf("the other window was asked for %+v", a)
	}

	if _, err := w.OpenIn("", "", "/gone", 80, 24, nil); !errors.Is(err, ErrNoFolder) {
		t.Fatalf("a folder not there gave %v", err)
	}
	<-got
	if _, err := w.OpenIn("srv", "", "/shut", 80, 24, nil); err == nil || err.Error() != "srv is not connected here" {
		t.Fatalf("a refusal gave %v", err)
	}
}

// A window of a build before a pane could be asked for in a folder is
// told apart, for the client to open it as that window can.
func TestAWindowThatCannotOpenInAFolderSaysSo(t *testing.T) {
	_, w := takenOverServing(t, Config{})
	if _, err := w.OpenIn("", "", "/src", 80, 24, nil); !errors.Is(err, ErrCannotOpenIn) {
		t.Fatalf("asked of a window that cannot, it gave %v", err)
	}
}

// What was opened for a session is closed there when the client asks,
// and only that: an attach names nothing to close.
func TestWhatWasOpenedIsClosedThereWhenAsked(t *testing.T) {
	closed := make(chan Attached, 1)
	_, w := takenOverServing(t, Config{
		OpenIn: func(_, _, _ string, cols, rows int) (session.Session, Attached, error) {
			return newEchoSession(cols, rows), Attached{ID: "p9", Kind: "Terminal"}, nil
		},
		Attach: func(_ Attached, cols, rows int) (session.Session, error) {
			return newEchoSession(cols, rows), nil
		},
		CloseOpened: func(what Attached) error { closed <- what; return nil },
	})
	sess, err := w.OpenIn("", "", "", 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	read(t, sess, "started at 80x24")
	if err := sess.(*remoteSession).CloseThere(); err != nil {
		t.Fatal(err)
	}
	select {
	case what := <-closed:
		if what.ID != "p9" {
			t.Fatalf("it closed %+v", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("nothing was closed there")
	}
	_ = sess.Close()

	attached, err := w.Attach(Open{ID: "p1", Kind: "Terminal"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer attached.Close()
	read(t, attached, "started at 80x24")
	if err := attached.(*remoteSession).CloseThere(); !errors.Is(err, ErrNotClosedThere) {
		t.Fatalf("asked to close what it attached to, it gave %v", err)
	}
}

// Each session closes only what was opened for it: the request names
// nothing, so one cannot close what another asked for.
func TestASessionClosesOnlyWhatWasOpenedForIt(t *testing.T) {
	closed := make(chan Attached, 2)
	var n atomic.Int32
	_, w := takenOverServing(t, Config{
		OpenIn: func(_, _, _ string, cols, rows int) (session.Session, Attached, error) {
			return newEchoSession(cols, rows), Attached{ID: "p" + strconv.Itoa(int(n.Add(1))), Kind: "Terminal"}, nil
		},
		CloseOpened: func(what Attached) error { closed <- what; return nil },
	})
	one, err := w.OpenIn("", "", "", 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	two, err := w.OpenIn("", "", "", 80, 24, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer two.Close()
	read(t, one, "started at 80x24")
	read(t, two, "started at 80x24")
	if err := two.(*remoteSession).CloseThere(); err != nil {
		t.Fatal(err)
	}
	if what := <-closed; what.ID != "p2" {
		t.Fatalf("the second session closed %+v", what)
	}
	select {
	case what := <-closed:
		t.Fatalf("it also closed %+v", what)
	case <-time.After(100 * time.Millisecond):
	}
}

// A client gone while what it asked for was being opened: what was
// opened is closed, there too, rather than left with nobody watching,
// whether taking the channel fails or still works, as it can before
// this end hears the connection close.
func TestWhatWasOpenedForAClientGoneIsClosed(t *testing.T) {
	release, opening := make(chan struct{}), make(chan struct{})
	opened := newEchoSession(80, 24)
	closed := make(chan Attached, 1)
	_, w := takenOverServing(t, Config{
		OpenIn: func(string, string, string, int, int) (session.Session, Attached, error) {
			close(opening)
			<-release
			return opened, Attached{ID: "p9", Kind: "Terminal"}, nil
		},
		CloseOpened: func(what Attached) error { closed <- what; return nil },
	})
	asked := make(chan error, 1)
	go func() {
		_, err := w.OpenIn("", "", "", 80, 24, nil)
		asked <- err
	}()
	// Asked, and gone before the answer.
	<-opening
	_ = w.Close()
	<-asked
	close(release)
	select {
	case what := <-closed:
		if what.ID != "p9" {
			t.Fatalf("it closed %+v", what)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("what was opened for the client gone was left open")
	}
	select {
	case <-opened.done:
	case <-time.After(5 * time.Second):
		t.Fatal("the session opened for the client gone was not closed")
	}
}

// A client that takes what it asked for and closes it at once, as a
// pane closed as soon as it opened, leaves it open there, as any pane
// closed there does: only a client gone before it took it has it closed.
// The client here never answers what it is asked, as one that closed
// the channel before reading it, which is what the window must not take
// for a client gone.
func TestWhatAClientTookAndClosedStaysOpen(t *testing.T) {
	opened := newEchoSession(80, 24)
	closed := make(chan Attached, 1)
	_, w := takenOverServing(t, Config{
		OpenIn: func(string, string, string, int, int) (session.Session, Attached, error) {
			return opened, Attached{ID: "p9", Kind: "Terminal"}, nil
		},
		CloseOpened: func(what Attached) error { closed <- what; return nil },
	})
	ch, _, err := w.client.OpenChannel(SessionInChannel, ssh.Marshal(openOn{Cols: 80, Rows: 24}))
	if err != nil {
		t.Fatal(err)
	}
	_ = ch.Close()
	select {
	case <-opened.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the session was not let go of")
	}
	select {
	case what := <-closed:
		t.Fatalf("it closed %+v there", what)
	default:
	}
}

// An attach that has nothing to attach to is refused, saying why,
// before any session is shown; one that has is carried as Attach's is;
// and a window that cannot tell first says so.
func TestAnAttachToWhatIsGoneIsRefused(t *testing.T) {
	_, w := takenOverServing(t, Config{Attach: func(want Attached, cols, rows int) (session.Session, error) {
		if want.ID != "p1" {
			return nil, errors.New("there is no terminal called \"" + want.ID + "\" open here any more")
		}
		return newEchoSession(cols, rows), nil
	}})
	if _, err := w.AttachIn(Open{ID: "p3", Kind: "Terminal"}, 80, 24); err == nil || err.Error() != `there is no terminal called "p3" open here any more` {
		t.Fatalf("attached to what is gone, it gave %v", err)
	}
	sess, err := w.AttachIn(Open{ID: "p1", Kind: "Terminal"}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	read(t, sess, "started at 80x24")
}
