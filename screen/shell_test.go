package screen

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/marrasen/kakel/grid"
	"github.com/marrasen/kakel/meter"
	"github.com/marrasen/kakel/session"
	"github.com/marrasen/kakel/vt"
)

// fakeSession reads what it holds and takes what is written.
type fakeSession struct {
	io.Reader
	late func(error)
}

func (f *fakeSession) Write(b []byte) (int, error)   { return len(b), nil }
func (f *fakeSession) Close() error                  { return nil }
func (f *fakeSession) Resize(int, int) error         { return nil }
func (f *fakeSession) Wait() error                   { return nil }
func (f *fakeSession) ReportLate(report func(error)) { f.late = report }

// What crosses a session is counted, and a failure it reports late
// still reaches whoever asked for it.
func TestASessionIsCountedAndStillReportsLate(t *testing.T) {
	m := meter.New()
	f := &fakeSession{Reader: strings.NewReader("hello")}
	sess := counted(f, m)
	buf := make([]byte, 16)
	if _, err := sess.Read(buf); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.Write([]byte("ab")); err != nil {
		t.Fatal(err)
	}
	if in, out := m.Totals(); in != 5 || out != 2 {
		t.Fatalf("counted %d in and %d out", in, out)
	}
	var got error
	sess.(interface{ ReportLate(func(error)) }).ReportLate(func(err error) { got = err })
	f.late(errors.New("resize failed"))
	if got == nil {
		t.Fatal("the late failure was lost")
	}
}

// mirrored is a session that mirrors another terminal.
type mirrored struct{ session.Session }

func (mirrored) Mirrors() bool { return true }

// Counting what a session moves keeps whether it mirrors another
// terminal, which its terminal must know to answer nothing.
func TestCountingKeepsAMirror(t *testing.T) {
	if !session.Mirrors(counted(mirrored{}, meter.New())) {
		t.Error("a counted mirror is no mirror")
	}
	if session.Mirrors(counted(nil, meter.New())) {
		t.Error("a counted session is a mirror")
	}
}

// screensOf hands over one screen, and then nothing.
type screensOf struct {
	mirrored
	scr  chan *session.Screen
	gone chan struct{}
}

func (s screensOf) ReadScreen([]byte) (int, *session.Screen, error) {
	select {
	case scr := <-s.scr:
		return 0, scr, nil
	case <-s.gone:
		return 0, nil, io.EOF
	}
}

// A session that hands over whole screens does so through the shell's
// counting of it, and the shell is drawn at the screen's size at once.
func TestAShellHasTheSizeOfItsSessionsScreens(t *testing.T) {
	s := screensOf{mirrored: mirrored{&fakeSession{}}, scr: make(chan *session.Screen, 1), gone: make(chan struct{})}
	wrote := make(chan struct{}, 8)
	sh := Open(s, vt.DefaultPalette(), Hooks{Output: func() { wrote <- struct{}{} }})
	defer close(s.gone)
	s.scr <- &session.Screen{Cols: 31, Rows: 7, Draw: []byte("hello")}
	<-wrote
	sh.Drawn(func(g *grid.Grid) {
		if cols, rows := g.Size(); cols != 31 || rows != 7 || g.At(0, 0).Rune != 'h' {
			t.Errorf("it is drawn %dx%d, beginning with %q", cols, rows, g.At(0, 0).Rune)
		}
	})
	sh.Resize(80, 24)
	if size := sh.T.Size(); size.Cols != 31 || size.Rows != 7 {
		t.Errorf("a layout made it %v", size)
	}
	if in, _ := sh.Traffic().Totals(); in != 5 {
		t.Errorf("%d bytes were counted", in)
	}
}
