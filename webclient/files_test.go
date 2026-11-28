//go:build !js

package main_test

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marrasen/gunim/widget/dock"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestlook"
	"github.com/marrasen/kakel/internal/webtest"
)

// A guest downloads a file of the shared folder from its Files pane, and
// the page's save worker writes it to the disk as it comes, under the
// relay's own policy: a browser with no dialog for saving, as Firefox
// and Safari, has nothing but the worker for a large file.
func TestAGuestDownloadsThroughTheSaveWorker(t *testing.T) {
	h := newHost(t, "p1")
	dir := t.TempDir()
	body := make([]byte, 3<<20)
	if _, err := rand.Read(body); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "big.bin"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := h.host.SetFiles(dir, guest.FilesLook); err != nil {
		t.Fatal(err)
	}
	l := dock.NewLayout(true, "p1", "files")
	l.Show("files")
	raw, err := guestlook.Layout(l.Windows[0].Root, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	h.change(func(st *guest.State) {
		st.Layout, st.HostIn, st.Files, st.Folder, st.Folders = raw, "files", guest.FilesLook, "Share", 1
		st.Panes = append(st.Panes[:1], guest.Pane{ID: "files", Title: "Files", Kind: guest.KindFiles, Icon: "filemanager"})
	})

	saved := t.TempDir()
	shared.browser.Downloads(t, saved)
	shared.browser.Allow(h.t, relayed.server.URL, "clipboardReadWrite", "clipboardSanitizedWrite")
	// No dialog for saving, as in Firefox and Safari: the worker it is.
	// Each frame a download is asked for in is kept, to see where it went.
	p := shared.browser.Open(t, h.link.String(), 1000, 700, 1, watchedGuest+`; delete Window.prototype.showSaveFilePicker; delete window.showSaveFilePicker;
window.framed = []; { const d = Object.getOwnPropertyDescriptor(HTMLIFrameElement.prototype, "src");
Object.defineProperty(HTMLIFrameElement.prototype, "src", {get() { return d.get.call(this) }, set(v) { framed.push(String(v)); d.set.call(this, v) }}); }`)
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("its console: %q", p.Console())
		}
		p.Close()
	})
	h.join(p, "Bea")
	row := mustPlace(t, p, "big.bin")
	p.Click(row[0], row[1], "left", 1)
	p.Click(row[0], row[1], "right", 1)
	item := mustPlace(t, p, "Download")
	p.Click(item[0], item[1], "left", 1)
	if shots := os.Getenv("KAKEL_SHOTS"); shots != "" {
		steady()
		if err := os.WriteFile(filepath.Join(shots, "guest-files-download.png"), p.PNG(), 0o644); err != nil {
			t.Error(err)
		}
	}

	await(t, func() string {
		got, err := os.ReadFile(filepath.Join(saved, "big.bin"))
		switch {
		case err != nil:
			return fmt.Sprint("nothing downloaded yet: ", err)
		case !bytes.Equal(got, body):
			return fmt.Sprintf("%d bytes of %d downloaded", len(got), len(body))
		}
		return ""
	})
	// The download went through the worker: its frame asked the
	// worker's own folder for the file, and the worker is there.
	var scopes, framed []string
	p.Eval(`navigator.serviceWorker.getRegistrations().then(rs => rs.map(r => r.scope))`, &scopes)
	if len(scopes) != 1 || !strings.HasSuffix(scopes[0], "/gunim-save/") {
		t.Errorf("the save worker is not there: %q", scopes)
	}
	p.Eval(`framed`, &framed)
	if len(framed) != 1 || !strings.Contains(framed[0], "/gunim-save/") || !strings.HasSuffix(framed[0], "/big.bin") {
		t.Errorf("the download was asked for in %q", framed)
	}
	for _, line := range p.Console() {
		if strings.Contains(line, "save worker") || strings.Contains(line, "in memory") {
			t.Errorf("the page said: %s", line)
		}
	}
	h.sound(p, false)
}

// mustPlace is where the part of the page called name is, and ends the
// test where there is none.
func mustPlace(t *testing.T, p *webtest.Page, name string) []float64 {
	t.Helper()
	at := placeOfPart(t, p, name)
	if len(at) != 2 {
		t.Fatalf("nothing on the page is called %q", name)
	}
	return at
}
