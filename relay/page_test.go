//go:build !js

package relay

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/webtest"
)

// guestPage is a bundle of the test's own page.
func guestPage(t *testing.T) (zipped, manifest []byte) {
	t.Helper()
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", execName))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "client.wasm")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out, "./testdata/guestpage")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS=")
	if said, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the page: %v\n%s", err, said)
	}
	wasm, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return made(t, map[string][]byte{
		indexName: []byte(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>guest</title>` +
			`<link rel="icon" href="icon.svg"><link rel="stylesheet" href="page.css">` +
			`</head><body><script src="wasm_exec.js"></script><script src="start.js"></script></body></html>`),
		"start.js":   []byte(`const go = new Go(); WebAssembly.instantiateStreaming(fetch("client.wasm"), go.importObject).then(r => go.run(r.instance));`),
		"page.css":   []byte(`body { background: rgb(1, 2, 3); }`),
		"icon.svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1 1"/>`),
		execName:     exe,
		wasmName:     packed(t, wasm),
		"notes.txt":  []byte("nothing"),
		"plain.html": []byte("<!doctype html><title>plain</title>"),
	})
}

// TestAGuestsBrowserReachesTheHost runs all of it in a browser: the page
// as it is served, under the policy it is served with, and its stream.
func TestAGuestsBrowserReachesTheHost(t *testing.T) {
	if testing.Short() {
		t.Skip("a browser takes a while")
	}
	browser, err := webtest.Start()
	if errors.Is(err, webtest.ErrNoChrome) {
		t.Skipf("the guest's page is not tested here: %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(browser.Close)
	quick(t, time.Minute, 300*time.Millisecond)
	s, u := started(t)
	zipped, manifest := guestPage(t)
	key, sig := signed(t, manifest)
	s.key = key
	client := Client{Relay: u}
	if err := client.PutBundle(context.Background(), zipped, sig); err != nil {
		t.Fatal(err)
	}
	id, token, bundle := NewShareID(), NewToken(), BundleHash(zipped)
	link := linkTo(t, u, id)
	long := 60 * time.Second

	// Opened while the relay has just started and its host is not back:
	// the page tries again by itself, and nothing of the link is lost.
	s.settled = time.Now().Add(time.Hour)
	p := browser.Open(t, link.String(), 800, 600, 1, "")
	p.Await("no page", `document.querySelector("h1")`, long)
	l, _ := sharedAs(t, u, id, token, bundle)
	serve := func(l *ShareListener) {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				b := make([]byte, 64<<10)
				for {
					n, err := c.Read(b)
					if err != nil {
						return
					}
					if string(b[:n]) == "end" {
						_, _ = c.Write(make([]byte, 300000))
						return
					}
					if _, err := c.Write(b[:n]); err != nil {
						return
					}
				}
			}(c)
		}
	}
	go serve(l)
	p.Await("no client", `window.kakelJoin`, long)
	s.settled = time.Time{}

	tell := func(do, want string) {
		t.Helper()
		var got string
		p.Eval(`kakelJoin("`+do+`")`, &got)
		if got != want {
			t.Errorf("%s: %s, not %s", do, got, want)
		}
	}
	tell("echo", "echoed true for "+link.Secret)
	tell("late", "late, then <nil>")
	tell("close", "closed, "+net.ErrClosed.Error())
	var got string
	p.Eval(`kakelJoin("end")`, &got)
	if !strings.HasPrefix(got, "read 300000, ") {
		t.Errorf("until the host closed, it %s", got)
	}

	// What it is served as lets it do no more.
	var look string
	p.Eval(`getComputedStyle(document.body).backgroundColor`, &look)
	if look != "rgb(1, 2, 3)" {
		t.Errorf("its style is not its own: %s", look)
	}
	var could []string
	p.Eval(`(async () => {
		const could = [];
		const s = document.createElement("script"); s.textContent = "window.inline = 1"; document.body.append(s);
		if (window.inline) could.push("inline script");
		try { await fetch("http://`+strings.Replace(u.Host(), "127.0.0.1", "localhost", 1)+`/"); could.push("fetch elsewhere"); } catch {}
		const f = document.createElement("iframe"); f.srcdoc = "x"; document.body.append(f);
		const i = new Image(); const bad = new Promise(r => { i.onload = () => r(false); i.onerror = () => r(true); });
		i.src = "data:image/gif;base64,R0lGODlhAQABAAAAACH5BAEKAAEALAAAAAABAAEAAAICTAEAOw=="; if (!await bad) could.push("a data image");
		// Last, as the test itself may until it has waited once.
		await new Promise(r => setTimeout(r, 50));
		try { eval("1"); could.push("eval"); } catch {}
		return could;
	})()`, &could)
	if len(could) > 0 {
		t.Errorf("its policy let it: %q", could)
	}
	p.Close()

	// Nothing of its own was against its policy.
	p = browser.Open(t, link.String(), 800, 600, 1, "")
	p.Await("no client", `window.kakelJoin`, long)
	tell("", "joined")
	if bad := p.Errors(); len(bad) > 0 {
		t.Errorf("its errors: %q", bad)
	}
	if said := p.Console(); len(said) > 0 {
		t.Logf("its console: %q", said)
	}

	// Why it is turned away.
	_ = l.Close()
	away(t, s, id)
	tell("", "refused 4002")
	s.back = 10 * time.Second
	tell("give up", context.DeadlineExceeded.Error())
	until(t, "the page that gave up waits still", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.shares[id].waiting == 0
	})
	l, _ = sharedAs(t, u, id, token, bundle)
	if err := l.End(context.Background()); err != nil {
		t.Fatal(err)
	}
	tell("", "refused 4003")
	p.Close()

	// And the page of a link that has expired.
	p = browser.Open(t, link.String(), 800, 600, 1, "")
	p.Await("no page", `document.querySelector("h1") && getComputedStyle(document.body).display === "grid"`, long)
	p.Close()
}
