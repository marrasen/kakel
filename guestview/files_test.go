package guestview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/filemanager"
	"github.com/marrasen/gunim/geom"
	"github.com/marrasen/gunim/gunimtest"
	"github.com/marrasen/gunim/widget/dock"
	"github.com/pkg/sftp"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
)

// filesRig is a page in a window of its own, joined to a real host that
// shares a folder, over a real connection.
type filesRig struct {
	t    *testing.T
	host *guest.Host
	dir  string
	win  *gunim.Window
	f    *flow
	st   guest.State
	// logged is what the page logged, and did what the guest did in the
	// folder, as the host heard it.
	mu     sync.Mutex
	logged bytes.Buffer
	did    []guest.FileEvent
	// files stands between the page and its files channel, as a host
	// slow to answer or failing is.
	files func(ctx context.Context, open func(context.Context) (*sftp.Client, error)) (*sftp.Client, error)
}

// slowFiles is a session whose files channel is as r.files has it.
type slowFiles struct {
	Session
	r *filesRig
}

func (s slowFiles) Files(ctx context.Context) (*sftp.Client, error) {
	if s.r.files == nil {
		return s.Session.Files(ctx)
	}
	return s.r.files(ctx, s.Session.Files)
}

// events is what the host heard the guest do in the folder.
func (r *filesRig) events() []guest.FileEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.did)
}

// onUI runs a function on the window's UI goroutine, as a patch.
type onUI struct{ do func(u *gunim.UI) }

func newFilesRig(t *testing.T, may guest.FilesMay, panes ...guest.Pane) *filesRig {
	t.Helper()
	return newFilesRigWith(t, nil, may, panes...)
}

// newFilesRigWith is newFilesRig, its files channel as files has it.
func newFilesRigWith(t *testing.T, files func(context.Context, func(context.Context) (*sftp.Client, error)) (*sftp.Client, error),
	may guest.FilesMay, panes ...guest.Pane) *filesRig {
	t.Helper()
	quick(t)
	r := &filesRig{t: t, dir: t.TempDir(), files: files}
	tree := map[string]string{"a.txt": "AAA", "sub/b.txt": "BBB", "sub/deep/c.txt": "CCC"}
	for name, body := range tree {
		p := filepath.Join(r.dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	host, err := guest.NewHost(guest.HostConfig{
		Ask:  func(context.Context, guest.Request) bool { return true },
		Open: func(string) (guest.PaneSource, error) { return nil, errors.New("no terminals here") },
		OnFiles: func(e guest.FileEvent) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.did = append(r.did, e)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.host = host
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan struct{})
	go func() { defer close(served); _ = host.Serve(ln) }()
	t.Cleanup(func() { host.End(guest.ReasonStopped); <-served })

	var ids []string
	for _, p := range panes {
		ids = append(ids, p.ID)
	}
	l := dock.NewLayout(true, ids...)
	r.st = guest.State{HostName: "Anna", Panes: panes, HostIn: ids[0], Folder: "Share", Files: may, Folders: 1}
	r.st.Layout, _ = guestlook.Layout(l.Windows[0].Root, func(string) bool { return true })
	r.share(may)

	was := log.Writer()
	log.SetOutput(&lockedWriter{r: r})
	t.Cleanup(func() { log.SetOutput(was) })

	keep := &fakeKeep{}
	cfg := Config{
		Dial: func(ctx context.Context, key guest.Key) (Session, error) {
			nc, err := net.Dial("tcp", ln.Addr().String())
			if err != nil {
				return nil, err
			}
			s, err := guest.Dial(ctx, nc, guest.Options{Secret: host.Secret(), Key: key})
			if err != nil {
				return nil, err
			}
			return slowFiles{Dialed(s), r}, nil
		},
		Keep: keep, Theme: themeAt(t, 0), FilesPrefs: filepath.Join(t.TempDir(), "prefs.json"),
	}
	r.win = gunimtest.NewWith(t, gunim.WindowOptions{Size: geom.Sz(1100, 700), Theme: Own(cfg.Theme.Theme, false)})
	gunim.RegisterPatch(r.win, "guest", func(_ *page, o onUI, u *gunim.UI) { o.do(u) })
	ctx, cancel := context.WithCancel(context.Background())
	r.f = newFlow(ctx, r.win, cfg)
	if err := r.f.show(r.win); err != nil {
		t.Fatal(err)
	}
	r.f.begin()
	done := make(chan struct{})
	go func() { defer close(done); _ = r.f.run(); r.f.close() }()
	t.Cleanup(func() { cancel(); <-done })
	r.f.post(func() { r.f.intent(Join{Name: "Bea"}) })
	r.until("the guest is in the window", func() bool { return r.page() == PageWindow })
	return r
}

// lockedWriter keeps what the page logs.
type lockedWriter struct{ r *filesRig }

func (w *lockedWriter) Write(b []byte) (int, error) {
	w.r.mu.Lock()
	defer w.r.mu.Unlock()
	return w.r.logged.Write(b)
}

func (r *filesRig) log() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.logged.String()
}

// share has the host share the folder as may says, and tell the guest.
func (r *filesRig) share(may guest.FilesMay) {
	r.t.Helper()
	if err := r.host.SetFiles(r.dir, may); err != nil {
		r.t.Fatal(err)
	}
	r.st.Files = may
	if may == guest.FilesNone {
		r.st.Folder = ""
	} else {
		r.st.Folder = "Share"
	}
	if err := r.host.Publish(r.st); err != nil {
		r.t.Fatal(err)
	}
}

// read runs fn on the flow's goroutine, and waits for it.
func (r *filesRig) read(fn func()) {
	done := make(chan struct{})
	r.f.post(func() { fn(); close(done) })
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		r.t.Fatal("the flow is stuck")
	}
}

func (r *filesRig) page() Page {
	var p Page
	r.read(func() { p = r.f.st.Page })
	return p
}

// until draws frames until ok.
func (r *filesRig) until(what string, ok func() bool) {
	r.t.Helper()
	for end := time.Now().Add(20 * time.Second); !ok(); {
		if time.Now().After(end) {
			r.t.Fatalf("waited for %s; the page logged:\n%s", what, r.log())
		}
		r.win.Frame(time.Second / 60)
		time.Sleep(2 * time.Millisecond)
	}
}

// pane is the file manager of Files pane id, nil for none.
func (r *filesRig) pane(id string) *filesPane {
	var p *filesPane
	r.read(func() { p = r.f.files.panes[id] })
	return p
}

// mounted waits for the file manager of pane id to show in its place,
// and returns it.
func (r *filesRig) mounted(id string) *filesPane {
	r.t.Helper()
	var p *filesPane
	r.until("the file manager is mounted", func() bool {
		var at gunim.ID
		r.read(func() {
			if p = r.f.files.panes[id]; p != nil {
				at = p.at
			}
		})
		return at != "" && r.onUI(func(u *gunim.UI) bool { return filemanager.FocusIn(u, p.host) != nil })
	})
	return p
}

// onUI asks something on the UI goroutine.
func (r *filesRig) onUI(ask func(u *gunim.UI) bool) bool {
	got := make(chan bool, 1)
	if err := r.win.Client().Patch(topic, onUI{func(u *gunim.UI) { got <- ask(u) }}); err != nil {
		r.t.Fatal(err)
	}
	for range 10 {
		r.win.Frame(time.Second / 60)
		select {
		case ok := <-got:
			return ok
		default:
		}
	}
	return false
}

// run runs a command of pane id's file manager, as its menu does.
func (r *filesRig) run(id, cmd string) {
	r.t.Helper()
	var host string
	r.read(func() {
		if p := r.f.files.panes[id]; p != nil {
			host = p.host
		}
	})
	if !r.onUI(func(u *gunim.UI) bool { return filemanager.Run(u, host, cmd) }) {
		r.t.Fatalf("%s did not run", cmd)
	}
}

// frames draws n frames.
func (r *filesRig) frames(n int) {
	for range n {
		r.win.Frame(time.Second / 60)
		time.Sleep(time.Millisecond)
	}
}

// shot writes what the window shows to the folder KAKEL_SHOTS names.
func (r *filesRig) shot(name string) {
	r.t.Helper()
	if dir := os.Getenv("KAKEL_SHOTS"); dir != "" {
		gunimtest.WritePNG(r.t, filepath.Join(dir, name+".png"), gunimtest.Pixels(r.t, r.win))
	}
}

// listed waits until pane id's file manager lists want, in its order,
// as Select all has it.
func (r *filesRig) listed(id string, want ...string) {
	r.t.Helper()
	var p *filesPane
	r.read(func() { p = r.f.files.panes[id] })
	r.until(fmt.Sprintf("%q listed", want), func() bool {
		r.run(id, filemanager.CmdSelectAll)
		r.frames(2)
		_, picked := p.showing()
		return slices.Equal(picked, want)
	})
}

// filesPaneAt is a Files pane, in the host's folder at.
func filesPaneAt(id, at string) guest.Pane {
	return guest.Pane{ID: id, Title: "Files", Kind: guest.KindFiles, Icon: "filemanager", Folder: at}
}

// The guest's Files pane is the file manager of the shared folder, in
// the folder the host's pane shows, with nothing above the folder's top,
// and at Look nothing that changes it.
func TestAFilesPaneShowsTheSharedFolder(t *testing.T) {
	r := newFilesRig(t, guest.FilesLook, filesPaneAt("f1", "/sub"))
	p := r.mounted("f1")
	r.until("the host's folder shows", func() bool { at, _ := p.showing(); return at == "/sub" })
	var held int
	r.read(func() { held = r.f.budget.held() })
	if held != filesMemory {
		t.Errorf("the budget keeps %d for the file manager", held)
	}
	r.frames(60)
	r.shot("files-look")
	r.listed("f1", "/sub/deep", "/sub/b.txt")

	// Up goes to the top, and no further.
	r.run("f1", filemanager.CmdUp)
	r.until("the top shows", func() bool { at, _ := p.showing(); return at == "/" })
	r.run("f1", filemanager.CmdUp)
	r.listed("f1", "/sub", "/a.txt")

	// Look: a change is refused by the file manager itself, and the host
	// hears of nothing done.
	r.run("f1", filemanager.CmdDelete)
	r.until("the refusal is logged", func() bool { return strings.Contains(r.log(), "can only be looked at") })
	if _, err := os.Stat(filepath.Join(r.dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if did := r.events(); slices.ContainsFunc(did, func(e guest.FileEvent) bool { return e.Op != guest.FileDownloaded }) {
		t.Errorf("the host heard %+v", did)
	}

	// Change: the same file manager changes the folder.
	r.share(guest.FilesChange)
	r.until("the file manager may change the folder", func() bool {
		var may guest.FilesMay
		r.read(func() { may = r.f.files.may })
		return may == guest.FilesChange
	})
	r.run("f1", filemanager.CmdSelectNone)
	r.run("f1", filemanager.CmdSelectAll)
	r.run("f1", filemanager.CmdDuplicate)
	r.until("the copies are on the host", func() bool {
		_, err := os.Stat(filepath.Join(r.dir, "a (2).txt"))
		_, err2 := os.Stat(filepath.Join(r.dir, "sub (2)", "deep", "c.txt"))
		return err == nil && err2 == nil
	})
	if r.pane("f1") != p {
		t.Error("the file manager was made anew")
	}
	r.frames(60)
	r.shot("files-change")
}

// Files that go down mid-share take the file manager with them, at once,
// and the pane says it is not shared; back up, it is there again, and a
// folder of another count is shown anew.
func TestFilesGoingDownTakeTheFileManagerAway(t *testing.T) {
	r := newFilesRig(t, guest.FilesChange, filesPaneAt("f1", ""))
	p := r.mounted("f1")
	r.until("the top shows", func() bool { at, _ := p.showing(); return at == "/" })

	r.frames(60)
	r.share(guest.FilesNone)
	for i := range 4 {
		// Going, a frame at a time.
		r.frames(4)
		r.shot("files-going-" + strconv.Itoa(i))
	}
	r.until("the file manager is let go of", func() bool {
		var gone bool
		r.read(func() { gone = r.f.files.hub == nil && r.f.budget.held() == 0 })
		return gone
	})
	if !r.onUI(func(u *gunim.UI) bool {
		pg, ok := u.Mounted("guest").(*page)
		return ok && pg.win != nil && pg.win.panes["f1"] != nil && pg.win.panes["f1"].face.says == notShared
	}) {
		t.Fatal("the pane does not say it is not shared")
	}
	r.until("the file manager has gone", func() bool {
		select {
		case <-p.win.Done():
			return true
		default:
			return false
		}
	})

	r.share(guest.FilesLook)
	q := r.mounted("f1")
	if q == p {
		t.Fatal("the file manager that went is back")
	}
	r.st.Folders = 2
	r.share(guest.FilesLook)
	r.until("another folder is shown anew", func() bool { n := r.pane("f1"); return n != nil && n != q })
}

// A host slow to give the files channel, slower than the view is to give
// the file manager its place, still has the file manager shown there.
func TestASlowFilesChannelStillShowsTheFolder(t *testing.T) {
	r := newFilesRigWith(t, func(ctx context.Context, open func(context.Context) (*sftp.Client, error)) (*sftp.Client, error) {
		time.Sleep(300 * time.Millisecond)
		return open(ctx)
	}, guest.FilesLook, filesPaneAt("f1", ""))
	r.mounted("f1")
	r.listed("f1", "/sub", "/a.txt")

	// None, and back, and another folder: each shows again.
	r.share(guest.FilesNone)
	r.until("the file manager is let go of", func() bool {
		var gone bool
		r.read(func() { gone = r.f.files.hub == nil })
		return gone
	})
	r.share(guest.FilesLook)
	r.mounted("f1")
	r.listed("f1", "/sub", "/a.txt")
	r.st.Folders = 2
	r.share(guest.FilesLook)
	r.until("the other folder's file manager", func() bool {
		var folders uint32
		r.read(func() { folders = r.f.files.folders })
		return folders == 2
	})
	r.mounted("f1")
	r.listed("f1", "/sub", "/a.txt")
}

// A files channel that could not be had is asked for again, each new
// reason logged, and meanwhile the file manager says why it shows
// nothing.
func TestAFailedFilesChannelIsAskedForAgain(t *testing.T) {
	var mu sync.Mutex
	fails := 3
	r := newFilesRigWith(t, func(ctx context.Context, open func(context.Context) (*sftp.Client, error)) (*sftp.Client, error) {
		mu.Lock()
		defer mu.Unlock()
		if fails > 0 {
			fails--
			return nil, errors.New("the host is busy")
		}
		return open(ctx)
	}, guest.FilesLook, filesPaneAt("f1", ""))
	r.mounted("f1")
	r.listed("f1", "/sub", "/a.txt")
	if n := len(regexp.MustCompile(`(?m)^\S+ \S+ couldn't reach the shared folder: the host is busy$`).FindAllString(r.log(), -1)); n != 1 {
		t.Errorf("the reason is logged %d times:\n%s", n, r.log())
	}
	if !strings.Contains(r.log(), "Listing") {
		t.Errorf("the file manager did not say why it showed nothing:\n%s", r.log())
	}
}
