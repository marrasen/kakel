//go:build !js

package relay

import (
	"context"
	"errors"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/clientbuild"
	"github.com/marrasen/kakel/internal/webtest"
)

// wasmDoor is a relay whose client.wasm can be tampered with.
type wasmDoor struct {
	http.Handler
	do atomic.Pointer[func(w http.ResponseWriter, r *http.Request)]
}

func (d *wasmDoor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if do := d.do.Load(); do != nil && strings.HasSuffix(r.URL.Path, "/client.wasm") {
		// Sent whole, whatever the browser has of it from before.
		r.Header.Del("If-None-Match")
		(*do)(w, r)
		return
	}
	d.Handler.ServeHTTP(w, r)
}

// halfway is what a file is sent through, with then called once half of
// it has been.
type halfway struct {
	http.ResponseWriter
	half int
	then func()
}

func (h *halfway) Unwrap() http.ResponseWriter { return h.ResponseWriter }

func (h *halfway) Write(p []byte) (int, error) {
	n, err := h.ResponseWriter.Write(p)
	if h.half -= n; h.half <= 0 && h.then != nil {
		_ = http.NewResponseController(h.ResponseWriter).Flush()
		h.then()
		h.then = nil
	}
	return n, err
}

// theClient is the web client as it is released, made into a bundle.
func theClient(t *testing.T) (zipped, manifest []byte) {
	t.Helper()
	files, err := clientbuild.Build()
	if err != nil {
		t.Fatal(err)
	}
	packed, err := clientbuild.Packed(files)
	if err != nil {
		t.Fatal(err)
	}
	return made(t, packed)
}

// watched runs in the page ahead of its own code, and keeps what its
// policy stops, and what fades.
const watched = `window.stopped = []; addEventListener("securitypolicyviolation", e => stopped.push(e.violatedDirective + " " + e.blockedURI));
window.faded = []; addEventListener("transitionrun", e => { if (e.propertyName === "opacity") faded.push(e.target.id) });`

// TestTheWebClientRunsFromARelay builds the client that is released,
// gives it to a relay, and opens it as a guest does: under the policy a
// relay serves it with.
func TestTheWebClientRunsFromARelay(t *testing.T) {
	if testing.Short() {
		t.Skip("the client and a browser take a while")
	}
	browser, err := webtest.Start()
	if errors.Is(err, webtest.ErrNoChrome) {
		t.Skipf("the web client is not tested here: %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(browser.Close)
	zipped, manifest := theClient(t)
	key, sig := signed(t, manifest)
	if _, err := CheckBundle(zipped, sig, key); err != nil {
		t.Fatal(err)
	}
	const long = 60 * time.Second

	// shared is the client on a relay, at its guests' link.
	shared := func(t *testing.T, sig string, as func(*Server)) (*wasmDoor, string) {
		t.Helper()
		s := NewServer(NewSecret())
		as(s)
		door := &wasmDoor{Handler: s}
		ts := httptest.NewServer(door)
		t.Cleanup(ts.Close)
		u, err := ParseURL(ts.URL + "/k/" + s.secret)
		if err != nil {
			t.Fatal(err)
		}
		if err := (Client{Relay: u}).PutBundle(context.Background(), zipped, sig); err != nil {
			t.Fatal(err)
		}
		id := NewShareID()
		sharedAs(t, u, id, NewToken(), BundleHash(zipped))
		return door, linkTo(t, u, id).String()
	}
	shows := func(p *webtest.Page) {
		t.Helper()
		p.Await("the window", `document.body.matches(".ready:not(.turning)")`, long)
		p.Await("the window alone", `getComputedStyle(document.querySelector("#loading")).visibility === "hidden" && getComputedStyle(document.querySelector("#gunim")).opacity == 1`, long)
	}
	scheme := func(p *webtest.Page, name string) {
		p.Call("Emulation.setEmulatedMedia", map[string]any{"features": []map[string]string{{"name": "prefers-color-scheme", "value": name}}}, nil)
	}

	for _, c := range []struct {
		name, sig string
		as        func(*Server)
	}{
		{"signed", sig, func(s *Server) { s.key = key }},
		{"unsigned", "", func(s *Server) { s.AllowUnsigned = true }},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, link := shared(t, c.sig, c.as)
			page, secret, _ := strings.Cut(link, "#")
			// Asked for the demo too, which is not in it.
			p := browser.Open(t, page+"?demo=1#"+secret, 800, 600, 1, watched)
			defer p.Close()
			shows(p)
			shot(t, p, "window-"+c.name)
			if !drawsSomething(p) {
				t.Error("the window is of one colour")
			}
			p.Await("one fading over the other", `faded.includes("loading") && faded.includes("gunim")`, long)
			var stopped []string
			p.Eval(`stopped`, &stopped)
			if bad := p.Errors(); len(bad)+len(stopped) > 0 {
				t.Errorf("its errors: %q, and what its policy stopped: %q", bad, stopped)
			}
			var demo bool
			p.Eval(`"kakelDemo" in window`, &demo)
			if demo {
				t.Error("the demo is in the client that is released")
			}
			// The secret is in its address, for a reload, and nowhere else.
			var said, asked string
			p.Eval(`document.title + document.documentElement.outerHTML`, &said)
			p.Eval(`performance.getEntriesByType("resource").map(e => e.name).join(" ")`, &asked)
			for _, where := range append(p.Console(), said, asked) {
				if strings.Contains(where, secret) {
					t.Errorf("the link's secret is in %q", where)
				}
			}
			var hash string
			p.Eval(`location.hash`, &hash)
			if hash != "#"+secret {
				t.Errorf("a reload would open %q", hash)
			}
		})
	}

	door, link := shared(t, sig, func(s *Server) { s.key = key })
	_, secret, _ := strings.Cut(link, "#")
	// through has client.wasm answered by do, until the test is done.
	through := func(t *testing.T, do func(w http.ResponseWriter, r *http.Request)) {
		door.do.Store(&do)
		t.Cleanup(func() { door.do.Store(nil) })
	}

	t.Run("loading", func(t *testing.T) {
		on := make(chan struct{})
		through(t, func(w http.ResponseWriter, r *http.Request) {
			h := &halfway{ResponseWriter: w, half: len(zipped) / 2, then: func() { <-on }}
			door.Handler.ServeHTTP(h, r)
		})
		p := browser.Open(t, link, 900, 600, 1, watched)
		defer p.Close()
		p.Await("half of it", `(n => n > 20 && n < 80)(Number(document.querySelector("#bar").getAttribute("aria-valuenow")))`, long)
		time.Sleep(400 * time.Millisecond)
		scheme(p, "light")
		shot(t, p, "loading-light")
		scheme(p, "dark")
		shot(t, p, "loading-dark")
		p.Size(390, 844, 2)
		shot(t, p, "loading-phone")
		p.Size(900, 600, 1)
		close(on)
		shows(p)
		if bad := p.Errors(); len(bad) > 0 {
			t.Errorf("its errors: %q", bad)
		}
	})

	// failed waits until the page says why there is no client, and
	// reports whether it can be tried again.
	failed := func(p *webtest.Page) (again bool) {
		t.Helper()
		p.Await("what went wrong", `document.body.matches(".failed:not(.turning)")`, long)
		p.Await("what went wrong, to be seen", `(e => getComputedStyle(e).visibility === "visible" && getComputedStyle(e).opacity == 1)(document.querySelector("#loading")) && getComputedStyle(document.querySelector("#card")).opacity == 1`, long)
		p.Await("what went wrong, faded to", `faded.includes("card") || faded.filter(id => id === "loading").length > 1`, long)
		p.Eval(`document.querySelector("#retry").offsetWidth > 0 && document.activeElement.id === "retry"`, &again)
		return again
	}
	tryAgain := func(p *webtest.Page) {
		t.Helper()
		p.Press(webtest.Key{Name: "Enter", Code: "Enter", Number: 13, Text: "\r"})
	}

	for _, c := range []struct {
		name  string
		do    func(w http.ResponseWriter, r *http.Request)
		again bool
	}{
		{"expired", func(w http.ResponseWriter, r *http.Request) { expiredPage.serve(w, r) }, false},
		{"not found", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }, true},
		{"broken", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "broken", http.StatusBadGateway) }, true},
		{"too many", func(w http.ResponseWriter, r *http.Request) { tooManyPage.serve(w, r) }, true},
		{"cut", func(w http.ResponseWriter, r *http.Request) {
			door.Handler.ServeHTTP(&halfway{ResponseWriter: w, half: len(zipped) / 2, then: func() { panic(http.ErrAbortHandler) }}, r)
		}, true},
		{"another", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/wasm")
			_, _ = w.Write([]byte("\x00asm\x01\x00\x00\x00"))
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			through(t, c.do)
			p := browser.Open(t, link, 900, 600, 1, watched)
			defer p.Close()
			if got := failed(p); got != c.again {
				t.Errorf("it can be tried again: %v", got)
			}
			// What the relay says, the page says as it does.
			var says string
			p.Eval(`document.querySelector("#title").textContent`, &says)
			for _, n := range []struct {
				name string
				page notice
			}{{"expired", expiredPage}, {"too many", tooManyPage}} {
				if has := strings.Contains(n.page.body, "<h1>"+says+"</h1>"); has != (n.name == c.name) {
					t.Errorf("it says %q, and the page of %s as much: %v", says, n.name, has)
				}
			}
			scheme(p, "dark")
			shot(t, p, "failed-"+strings.ReplaceAll(c.name, " ", "-"))
			if !c.again {
				return
			}
			// And now it does.
			door.do.Store(nil)
			tryAgain(p)
			shows(p)
		})
	}

	t.Run("busy", func(t *testing.T) {
		// The relay has too much to send: the page waits, by itself.
		var asked atomic.Int32
		through(t, func(w http.ResponseWriter, r *http.Request) {
			if asked.Add(1) > 2 {
				door.Handler.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Retry-After", "1")
			http.Error(w, "busy", http.StatusServiceUnavailable)
		})
		p := browser.Open(t, link, 900, 600, 1, watched)
		defer p.Close()
		shows(p)
		if n := asked.Load(); n != 3 {
			t.Errorf("it asked %d times", n)
		}
	})

	// What the page meets in a browser, as a script run ahead of it has
	// it. left is whether it can be tried again, which is then done.
	for _, c := range []struct {
		name, first string
		again       bool
	}{
		{"too old", `WebAssembly.compileStreaming = () => Promise.reject(new WebAssembly.CompileError("unknown opcode"));`, false},
		{"no streaming", `WebAssembly.compileStreaming = undefined; WebAssembly.compile = () => Promise.reject(new WebAssembly.CompileError("unknown opcode"));`, false},
		{"no wasm", `delete window.WebAssembly;`, false},
		{"no memory", `WebAssembly.instantiate = () => Promise.reject(new RangeError("could not allocate memory"));`, true},
		{"exits at once", `{ let G; Object.defineProperty(globalThis, "Go", {get() { return G }, set(v) { G = class extends v { async run() { this.exit(2); } } }}); }`, true},
		// Later, with the window up.
		{"exits", `{ let G; Object.defineProperty(globalThis, "Go", {get() { return G }, set(v) { G = class extends v { run(i) { return Promise.race([super.run(i), new Promise(r => window.end = r)]); } } }}); }`, true},
		{"traps", `window.end = () => setTimeout(() => { throw new WebAssembly.RuntimeError("unreachable"); });`, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := browser.Open(t, link, 900, 600, 1, watched+c.first)
			defer p.Close()
			if strings.Contains(c.first, "window.end") {
				shows(p)
				p.Await("the window, faded to", `faded.includes("loading")`, long)
				p.Eval(`end()`, nil)
			}
			if got := failed(p); got != c.again {
				t.Errorf("it can be tried again: %v", got)
			}
			scheme(p, "light")
			shot(t, p, "failed-"+strings.ReplaceAll(c.name, " ", "-"))
			for _, line := range append(p.Console(), p.Errors()...) {
				if strings.Contains(line, secret) {
					t.Errorf("it logged the link's secret: %q", line)
				}
			}
		})
	}

	t.Run("no WebGL", func(t *testing.T) {
		without, err := webtest.Start("--disable-3d-apis")
		if err != nil {
			t.Fatal(err)
		}
		defer without.Close()
		p := without.Open(t, link, 900, 600, 1, watched)
		defer p.Close()
		if !failed(p) {
			t.Error("it cannot be tried again")
		}
		// And nothing is downloaded that cannot run.
		var asked string
		p.Eval(`performance.getEntriesByType("resource").map(e => e.name).join(" ")`, &asked)
		if strings.Contains(asked, "client.wasm") {
			t.Errorf("it downloaded %s", asked)
		}
		scheme(p, "dark")
		p.Size(390, 844, 2)
		shot(t, p, "failed-no-webgl")
	})
}

// drawsSomething reports whether a page shows more than one colour.
func drawsSomething(p *webtest.Page) bool {
	img := p.Shot()
	b := img.Bounds()
	first := color.RGBAModel.Convert(img.At(b.Min.X, b.Min.Y))
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if color.RGBAModel.Convert(img.At(x, y)) != first {
				return true
			}
		}
	}
	return false
}

// shot writes a page's picture to the folder KAKEL_SHOTS names, to look
// at.
func shot(t *testing.T, p *webtest.Page, name string) {
	t.Helper()
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), p.PNG(), 0o600); err != nil {
		t.Fatal(err)
	}
}
