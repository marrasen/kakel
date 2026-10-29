package app

import (
	"net/url"
	"strings"
	"testing"

	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"

	"github.com/marrasen/kakel/screen"
)

func TestALastRunIsFoundToHaveCrashedByWhatCameAfterItsHeading(t *testing.T) {
	heading := func(n string) string { return crashHeading + n + ", started then, process 1\n" }
	crash := "panic: oh no\n\ngoroutine 1 [running]:\nmain.main()\n"
	for _, c := range []struct {
		name, log, want string
	}{
		{"empty", "", ""},
		{"a quiet run", heading("a"), ""},
		{"a crash", heading("a") + crash, heading("a") + crash},
		{"a quiet run after a crash", heading("a") + crash + heading("b"), ""},
		{"a crash after a crash", heading("a") + crash + heading("b") + crash, heading("b") + crash},
		{"blank lines only", heading("a") + "\n\n", ""},
		{"something else", "not kakel's\n", ""},
		{"too big", heading("a") + strings.Repeat("x", mostCrashLog), ""},
	} {
		if got := lastRunCrash([]byte(c.log)); got != c.want {
			t.Errorf("%s: found %q, want %q", c.name, got, c.want)
		}
	}
}

// What a crash shows: its trace without the words in memory, which
// kakel it was and where the log is, and the buttons.
func TestACrashIsShownTidiedWithItsRunAndButtons(t *testing.T) {
	crash := crashHeading + "v1.2.3, started 2026-10-29T13:04:42+01:00, process 7\n" +
		"panic: runtime error: index out of range [9] with length 9\n\n" +
		"goroutine 166 [running]:\n" +
		"github.com/marrasen/gunim/widget.(*Form).Layout(0x9c8d22a4108, {{0x0?, 0x0?}}, ...)\n" +
		"\tgithub.com/marrasen/gunim@v0.0.0/widget/form.go:77 +0xa05\n" +
		"github.com/marrasen/gunim/widget.Sync[...](0x2943c2c6a680, {0x2943c2fcc008, 0xa})\n" +
		"main.(*ownWindows).open.func2(_, {0x2, 0x0})\n" +
		"panic({0x7ff7213be2e0?, 0x7ff720b53e60?})\n" +
		"created by github.com/marrasen/gunim.(*App).NewWindow in goroutine 62\n"
	q := crashAsk(crash, "/where/crash.log")
	want := "panic: runtime error: index out of range [9] with length 9\n\n" +
		"goroutine 166 [running]:\n" +
		"github.com/marrasen/gunim/widget.(*Form).Layout(…)\n" +
		"\tgithub.com/marrasen/gunim@v0.0.0/widget/form.go:77\n" +
		"github.com/marrasen/gunim/widget.Sync[...](…)\n" +
		"main.(*ownWindows).open.func2(…)\n" +
		"panic(…)\n" +
		"created by github.com/marrasen/gunim.(*App).NewWindow in goroutine 62"
	if q.Text != want {
		t.Errorf("the trace shown is\n%s\nwant\n%s", q.Text, want)
	}
	if !q.Preformatted || q.Copy != crash || q.Yes != "Close" || !q.Plain {
		t.Errorf("the question is %+v, want the trace preformatted, all of it to copy, and Close", q)
	}
	if len(q.Small) != 1 || !strings.HasPrefix(q.Small[0], "kakel v1.2.3, started 13:04 on 29 October,") || !strings.Contains(q.Small[0], "/where/crash.log") {
		t.Errorf("under it, %q", q.Small)
	}
	u, err := url.Parse(q.Link)
	if err != nil {
		t.Fatal(err)
	}
	if body := u.Query().Get("body"); !strings.HasPrefix(q.Link, newIssue+"?") || !strings.Contains(body, "widget.(*Form).Layout(…)") || !strings.Contains(body, "kakel v1.2.3 crashed") {
		t.Errorf("the issue's body is %q, want the version and the tidied trace", body)
	}
}

// A trace too long for a link is cut short to fit, and says so.
func TestACrashTooLongForALinkIsCutShort(t *testing.T) {
	trace := "panic: oh no\n\ngoroutine 1 [running]:\n" + strings.Repeat("main.deeper()\n\t/src/main.go:10\n", 2000)
	at := crashIssue("v1.2.3", trace)
	if len(at) > mostIssueLink {
		t.Fatalf("the link is %d long, past %d", len(at), mostIssueLink)
	}
	u, err := url.Parse(at)
	if err != nil {
		t.Fatal(err)
	}
	if body := u.Query().Get("body"); !strings.Contains(body, "panic: oh no") || !strings.Contains(body, "Cut short to fit") {
		t.Fatalf("the issue's body is %q", body)
	}
	if got := u.Query().Get("title"); got != "Crash: oh no" {
		t.Fatalf("the issue's title is %q", got)
	}
}

// The crash the last run left is shown as kakel starts, once, and its
// Report opens the issue's link.
func TestTheLastRunsCrashIsShownOnceAndReported(t *testing.T) {
	w := gunimtest.New(t, geom.Sz(400, 300), nil)
	a := newApp(w.Client(), screen.NewShells())
	a.ctx = t.Context()
	lastCrash, crashPath = crashHeading+"v1.2.3, started 2026-10-29T13:04:42+01:00, process 7\npanic: oh no\n", "/where/crash.log"
	t.Cleanup(func() { lastCrash, crashPath = "", "" })
	opened := make(chan string, 1)
	was := openInBrowser
	openInBrowser = func(at string) error { opened <- at; return nil }
	t.Cleanup(func() { openInBrowser = was })

	a.sayCrashed()
	waitFor(t, a, "the crash to be shown", func() bool { return len(a.st.Asks) == 1 })
	q := a.st.Asks[0]
	if q.Title != "kakel crashed" || q.Text != "panic: oh no" {
		t.Fatalf("the question is %+v", q)
	}
	a.handle(AskAction{ID: q.ID, Action: reportCrash})
	select {
	case at := <-opened:
		if at != q.Link {
			t.Fatalf("Report opened %q, want %q", at, q.Link)
		}
	default:
		t.Fatal("Report opened nothing")
	}
	a.sayCrashed()
	a.handle(AskAnswered{ID: q.ID})
	waitFor(t, a, "the question to go", func() bool { return len(a.st.Asks) == 0 })
	for range 20 {
		select {
		case f := <-a.events:
			f()
		default:
		}
	}
	if len(a.st.Asks) != 0 {
		t.Fatalf("shown again: %+v", a.st.Asks)
	}
}
