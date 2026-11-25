//go:build !js

package main_test

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/webtest"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/webclient/demo"
)

// The tests share one browser, one server and one page: a page takes a
// second or more to start where the processor draws, and a browser as
// long.
var shared struct {
	dir     string
	server  *httptest.Server
	browser *webtest.Browser
	page    *webtest.Page
	// host is what is at the other end of the relay the server runs.
	host *relay.Listener
	// up is closed once the relay has host, and err is why there is no
	// browser.
	up  chan struct{}
	err error
}

// pageW and pageH are the page's size as a test starts: small, as the
// processor draws it, and wide enough for the demo's lines.
const pageW, pageH = 800, 320

// longest is how long a test waits for what is to come, which is seldom
// long: a machine can be slow.
const longest = 60 * time.Second

func TestMain(m *testing.M) {
	code := m.Run()
	if shared.host != nil {
		_ = shared.host.Close()
	}
	if shared.browser != nil {
		shared.browser.Close()
	}
	if shared.server != nil {
		shared.server.CloseClientConnections()
		shared.server.Close()
	}
	if shared.dir != "" {
		_ = os.RemoveAll(shared.dir)
	}
	os.Exit(code)
}

// start builds and serves the client, with a relay beside it, and
// starts the browser, once for all the tests.
var start = sync.OnceFunc(func() {
	if _, shared.err = webtest.FindChrome(); shared.err != nil {
		return
	}
	if shared.dir, shared.err = os.MkdirTemp("", "kakel-web"); shared.err != nil {
		return
	}
	if shared.err = webtest.Build(shared.dir); shared.err != nil {
		return
	}
	secret := relay.NewSecret()
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(shared.dir)))
	mux.Handle("/k/", relay.NewServer(secret))
	shared.server = httptest.NewServer(mux)
	var at relay.URL
	if at, shared.err = relay.ParseURL(shared.server.URL + "/k/" + secret); shared.err != nil {
		return
	}
	var once sync.Once
	shared.up = make(chan struct{})
	shared.host, shared.err = relay.Listen(relay.HostConfig{
		Relay: at, Code: relay.NewCode(), Token: relay.NewToken(),
		OnState: func(_ string, err error) {
			if err == nil {
				once.Do(func() { close(shared.up) })
			}
		},
	})
	if shared.err != nil {
		return
	}
	go func() {
		for {
			c, err := shared.host.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	if shared.browser, shared.err = webtest.Start(); shared.err != nil {
		return
	}
})

// before runs in the page ahead of its own code, and has gunim log what
// its frames took.
const before = `{ let G; Object.defineProperty(globalThis, "Go", {get() { return G }, set(v) { G = class extends v { constructor() { super(); this.env.GUNIM_DEBUG_FRAMES = "1"; } } }}); }`

// demoPage is the demo as it starts, in the page the tests share.
func demoPage(t *testing.T) *webtest.Page {
	t.Helper()
	start()
	switch {
	case errors.Is(shared.err, webtest.ErrNoChrome):
		t.Skipf("the browser client is not tested here: %v", shared.err)
	case shared.err != nil:
		t.Fatal(shared.err)
	}
	shows := `window.kakelDemo && /~\$ *\n/.test(kakelDemo.screen() || "")`
	p := shared.page
	if p == nil {
		shared.browser.Allow(t, shared.server.URL, "clipboardReadWrite", "clipboardSanitizedWrite")
		p = shared.browser.Open(t, shared.server.URL+"/?demo=1", pageW, pageH, 1, before)
		shared.page = p
		p.Await("the terminal's prompt", shows, longest)
	} else {
		p.For(t)
		p.Size(pageW, pageH, 1)
		p.Eval(`kakelDemo.fit(0, 0)`, nil)
		p.Await("the page at its own size", fits(pageW, pageH), longest)
		p.Eval(`kakelDemo.reset()`, nil)
		p.Await("the terminal's prompt", `kakelDemo.sent() == "" && `+shows, longest)
	}
	// At the end, the page has had no errors. After a failure the next
	// test has a page of its own.
	t.Cleanup(func() {
		if bad := p.Errors(); len(bad) > 0 {
			t.Errorf("the page had errors: %q", bad)
		}
		if t.Failed() {
			t.Logf("the page's console: %q", p.Console())
			p.Close()
			shared.page = nil
		}
	})
	return p
}

// fits is JavaScript that is true once the program has as many cells as
// fit a page of a size.
func fits(width, height int) string {
	return fmt.Sprintf(`(() => { const s = kakelDemo.size(), w = %d - 2 * s.x, h = %d - 2 * s.y; return s.cols == Math.floor(w / s.w + 1e-3) && s.rows == Math.floor(h / s.h + 1e-3) })()`, width, height)
}

// place is where the terminal's cells are on the page.
type place struct{ Cols, Rows, X, Y, W, H float64 }

// of is the middle of a cell, in CSS pixels.
func (g place) of(col, row int) (x, y float64) {
	return g.X + (float64(col)+0.5)*g.W, g.Y + (float64(row)+0.5)*g.H
}

func placeOf(p *webtest.Page) place {
	var g place
	p.Eval("kakelDemo.size()", &g)
	return g
}

// spot is a cell, what it shows and the colour it is to be drawn in.
type spot struct {
	what     string
	col, row int
	want     color.RGBA
}

// coloursOf are the colours the window draws at places in CSS pixels, an
// x and a y each.
func coloursOf(p *webtest.Page, at []float64) ([]color.RGBA, error) {
	js, err := json.Marshal(at)
	if err != nil {
		return nil, err
	}
	var got []int
	if err := p.TryEval(fmt.Sprintf("kakelDemo.colours(%s)", js), &got); err != nil {
		return nil, err
	}
	out := make([]color.RGBA, len(got))
	for i, c := range got {
		out[i] = color.RGBA{uint8(c >> 16), uint8(c >> 8), uint8(c), 255}
	}
	return out, nil
}

// near reports whether two colours are the same to the eye.
func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int { return max(int(x)-int(y), int(y)-int(x)) }
	return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2
}

// await waits until ok returns "", and fails with what it returned last:
// what is wrong. It looks once more after its time is up, as one look
// can take all of it.
func await(t *testing.T, ok func() string) {
	t.Helper()
	for end := time.Now().Add(longest); ; time.Sleep(50 * time.Millisecond) {
		late := time.Now().After(end)
		wrong := ok()
		if wrong == "" {
			return
		}
		if late {
			t.Error(wrong)
			return
		}
	}
}

// awaitColours waits until each spot is drawn in its colour.
func awaitColours(t *testing.T, p *webtest.Page, spots ...spot) {
	t.Helper()
	await(t, func() string {
		g := placeOf(p)
		var at []float64
		for _, s := range spots {
			x, y := g.of(s.col, s.row)
			at = append(at, x, y)
		}
		got, err := coloursOf(p, at)
		if err != nil || len(got) != len(spots) {
			return fmt.Sprintf("%d colours, %v", len(got), err)
		}
		var wrong []string
		for i, s := range spots {
			if !near(got[i], s.want) {
				wrong = append(wrong, fmt.Sprintf("%s, in cell %d,%d, is drawn %v, want %v", s.what, s.col, s.row, got[i], s.want))
			}
		}
		return strings.Join(wrong, "; ")
	})
}

// red is the demo's true red, and where it is.
var red = spot{"true red", 2, 1, color.RGBA{255, 0, 0, 255}}

// keep writes a page's picture to the folder KAKEL_SHOTS names, to look
// at.
func keep(t *testing.T, p *webtest.Page, name string) {
	t.Helper()
	dir := os.Getenv("KAKEL_SHOTS")
	if dir == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), p.PNG(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// sent is what the demo's program has been sent since it had n bytes.
func sent(t *testing.T, p *webtest.Page, n int) string {
	t.Helper()
	var s string
	p.Eval("kakelDemo.sent()", &s)
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw[min(n, len(raw)):])
}

// awaitSent waits until the program has been sent want past its first n
// bytes.
func awaitSent(t *testing.T, p *webtest.Page, n int, want, what string) {
	t.Helper()
	await(t, func() string {
		if got := sent(t, p, n); got != want {
			return fmt.Sprintf("%s sent %q, want %q", what, got, want)
		}
		return ""
	})
}

// The page draws the terminal in kakel's default theme: each colour in
// its place, boxes whose lines meet, and letters.
func TestThePageDrawsTheTerminal(t *testing.T) {
	p := demoPage(t)
	var first float64
	p.Eval(`performance.getEntriesByName("kakel-first-frame")[0].startTime`, &first)
	t.Logf("the first frame came %.0f ms after the page was asked for", first)
	ground, ink := color.RGBA{0x14, 0x17, 0x1c, 255}, color.RGBA{0xc8, 0xd0, 0xda, 255}
	spots := []spot{red, {"true green", 6, 1, color.RGBA{0, 255, 0, 255}}, {"true blue", 10, 1, color.RGBA{0, 0, 255, 255}},
		{"the ground", 70, 1, ground}, {"a full block", 42, 6, ink}}
	for i, hexed := range []string{"1c2026", "e06c75", "8fd46a", "e6b450", "61afef", "c678dd", "56b6c2", "abb2bf",
		"5c6370", "ff8b94", "a9e88a", "ffd074", "84c5ff", "db9af0", "76d4df", "ffffff"} {
		c, _ := hex.DecodeString(hexed)
		spots = append(spots, spot{fmt.Sprintf("colour %d", i), i*2 + 1, 0, color.RGBA{c[0], c[1], c[2], 255}})
	}
	awaitColours(t, p, spots...)
	p.Await("the page faded in", `getComputedStyle(document.querySelector("#gunim")).opacity == 1`, longest)

	// And what the browser itself shows, once: the window is not only
	// drawn, it is on the page, and where it says.
	keep(t, p, "demo")
	g := placeOf(p)
	img := p.Shot()
	shown := func(x, y float64) color.RGBA {
		return color.RGBAModel.Convert(img.At(int(x), int(y))).(color.RGBA)
	}
	for _, s := range spots {
		x, y := g.of(s.col, s.row)
		if got := shown(x, y); !near(got, s.want) {
			t.Errorf("%s, in cell %d,%d, is shown %v, want %v", s.what, s.col, s.row, got, s.want)
		}
	}
	// The box's line down its left side has no gap where rows meet.
	x, _ := g.of(0, 4)
	for y := g.Y + 4*g.H + 0.5; y < g.Y+6*g.H; y++ {
		if c := shown(x, y); c.R < 0x60 {
			t.Errorf("the box's side has a gap at %v, %v, coloured %v", x, y, c)
			break
		}
	}
	// Letters: some of the pixels of a word are lit, and some are not.
	lit := 0
	for dx := range int(4 * g.W) {
		for dy := range int(g.H) {
			if c := shown(g.X+float64(dx), g.Y+2*g.H+float64(dy)); c.R > 0x80 {
				lit++
			}
		}
	}
	if all := int(4*g.W) * int(g.H); lit < all/20 || lit > all*3/4 {
		t.Errorf("of the word's %d pixels %d are lit", all, lit)
	}
}

// What is typed reaches the program, and shows: letters, keys with Ctrl,
// the arrows, and characters AltGr types.
func TestTypingReachesTheProgramAndShows(t *testing.T) {
	p := demoPage(t)
	p.Type("hello Web 42")
	awaitSent(t, p, 0, "hello Web 42", "typing")
	p.Await("what was typed, on the screen", `(kakelDemo.screen() || "").includes("~$ hello Web 42")`, longest)
	keep(t, p, "typed")

	for _, c := range []struct {
		what string
		key  webtest.Key
		want string
	}{
		{"Ctrl+C", webtest.Key{Name: "c", Code: "KeyC", Number: 'C', Mods: webtest.Ctrl}, "\x03"},
		{"Ctrl+D", webtest.Key{Name: "d", Code: "KeyD", Number: 'D', Mods: webtest.Ctrl}, "\x04"},
		{"Ctrl+Z", webtest.Key{Name: "z", Code: "KeyZ", Number: 'Z', Mods: webtest.Ctrl}, "\x1a"},
		{"Ctrl+A", webtest.Key{Name: "a", Code: "KeyA", Number: 'A', Mods: webtest.Ctrl}, "\x01"},
		// Those a browser would reload, find and paste with.
		{"Ctrl+R", webtest.Key{Name: "r", Code: "KeyR", Number: 'R', Mods: webtest.Ctrl}, "\x12"},
		{"Ctrl+L", webtest.Key{Name: "l", Code: "KeyL", Number: 'L', Mods: webtest.Ctrl}, "\x0c"},
		{"Ctrl+F", webtest.Key{Name: "f", Code: "KeyF", Number: 'F', Mods: webtest.Ctrl}, "\x06"},
		{"Ctrl+V", webtest.Key{Name: "v", Code: "KeyV", Number: 'V', Mods: webtest.Ctrl, Commands: []string{"paste"}}, "\x16"},
		{"F5", webtest.Key{Name: "F5", Code: "F5", Number: 116}, "\x1b[15~"},
		{"Alt+Left", webtest.Key{Name: "ArrowLeft", Code: "ArrowLeft", Number: 37, Mods: webtest.Alt}, "\x1b[1;3D"},
		{"Ctrl+Space", webtest.Key{Name: " ", Code: "Space", Number: ' ', Mods: webtest.Ctrl}, "\x00"},
		{"Up", webtest.Key{Name: "ArrowUp", Code: "ArrowUp", Number: 38}, "\x1b[A"},
		{"Down", webtest.Key{Name: "ArrowDown", Code: "ArrowDown", Number: 40}, "\x1b[B"},
		{"Right", webtest.Key{Name: "ArrowRight", Code: "ArrowRight", Number: 39}, "\x1b[C"},
		{"Left", webtest.Key{Name: "ArrowLeft", Code: "ArrowLeft", Number: 37}, "\x1b[D"},
		{"Ctrl+Left", webtest.Key{Name: "ArrowLeft", Code: "ArrowLeft", Number: 37, Mods: webtest.Ctrl}, "\x1b[1;5D"},
		{"Escape", webtest.Key{Name: "Escape", Code: "Escape", Number: 27}, "\x1b"},
		{"Tab", webtest.Key{Name: "Tab", Code: "Tab", Number: 9}, "\t"},
		{"Shift+Tab", webtest.Key{Name: "Tab", Code: "Tab", Number: 9, Mods: webtest.Shift}, "\x1b[Z"},
		{"Backspace", webtest.Key{Name: "Backspace", Code: "Backspace", Number: 8}, "\x7f"},
		{"Delete", webtest.Key{Name: "Delete", Code: "Delete", Number: 46}, "\x1b[3~"},
		{"Home", webtest.Key{Name: "Home", Code: "Home", Number: 36}, "\x1b[H"},
		{"Page Up", webtest.Key{Name: "PageUp", Code: "PageUp", Number: 33}, "\x1b[5~"},
		{"F2", webtest.Key{Name: "F2", Code: "F2", Number: 113}, "\x1bOQ"},
		{"Enter", webtest.Key{Name: "Enter", Code: "Enter", Number: 13, Text: "\r"}, "\r"},
		// AltGr, as Windows tells of it: Ctrl and Alt together.
		{"AltGr+2, an @ from Windows", webtest.Key{Name: "@", Code: "Digit2", Number: '2', Text: "@", Mods: webtest.Ctrl | webtest.Alt}, "@"},
		// And as Linux does, with neither.
		{"AltGr+7, a brace", webtest.Key{Name: "{", Code: "Digit7", Number: '7', Text: "{"}, "{"},
		{"a letter with a ring", webtest.Key{Name: "å", Code: "BracketLeft", Number: 219, Text: "å"}, "å"},
	} {
		n := len(sent(t, p, 0))
		p.Press(c.key)
		awaitSent(t, p, n, c.want, c.what)
	}
}

// An input method's text reaches the program once it is done, and
// nothing of it before.
func TestAnInputMethodTypes(t *testing.T) {
	p := demoPage(t)
	p.Compose("é", "´")
	awaitSent(t, p, 0, "é", "an input method")
	p.Type("x")
	awaitSent(t, p, 0, "éx", "typing after it")
}

// Text pasted reaches the program, by the keys a terminal pastes with.
// Ctrl+V is a program's own key: see TestTypingReachesTheProgramAndShows.
func TestAPasteReachesTheProgram(t *testing.T) {
	p := demoPage(t)
	p.Eval(`navigator.clipboard.writeText("pasted words")`, nil)
	for _, c := range []struct {
		what string
		key  webtest.Key
	}{
		// Chrome pastes on this one by itself: told to as well, it pastes
		// twice.
		{"Ctrl+Shift+V", webtest.Key{Name: "V", Code: "KeyV", Number: 'V', Mods: webtest.Ctrl | webtest.Shift}},
		{"Shift+Insert", webtest.Key{Name: "Insert", Code: "Insert", Number: 45, Mods: webtest.Shift, Commands: []string{"paste"}}},
	} {
		n := len(sent(t, p, 0))
		p.Press(c.key)
		awaitSent(t, p, n, "pasted words", c.what)
	}
}

// Text selected with the mouse is copied with the terminal's shortcut.
func TestASelectionIsCopied(t *testing.T) {
	p := demoPage(t)
	g := placeOf(p)
	// A double click picks a word.
	x, y := g.of(5, 2)
	p.Click(x, y, "left", 2)
	keep(t, p, "selected")
	p.Press(webtest.Key{Name: "Insert", Code: "Insert", Number: 45, Mods: webtest.Ctrl})
	p.Await("the word on the clipboard", `navigator.clipboard.readText().then(s => s == "italic")`, longest)
}

// The program is given the size that fits the page, and the page draws
// it whole at any size.
func TestThePageIsResized(t *testing.T) {
	p := demoPage(t)
	for _, c := range []struct {
		width, height int
		scale         float64
	}{{600, 420, 1}, {pageW, pageH, 1.5}} {
		p.Size(c.width, c.height, c.scale)
		p.Await(fmt.Sprintf("a screen that fits %+v", c), fits(c.width, c.height), longest)
		p.Await(fmt.Sprintf("a canvas of %+v", c), fmt.Sprintf(`document.querySelector("#gunim").width == %v`, float64(c.width)*c.scale), longest)
		awaitColours(t, p, red)
		keep(t, p, fmt.Sprintf("size-%dx%d@%v", c.width, c.height, c.scale))
	}
}

// A screen whose size the far end sets is drawn whole in the middle of
// the page.
func TestAScreenOfTheFarEndsSizeIsDrawnToFit(t *testing.T) {
	p := demoPage(t)
	const cols, rows = 110, 30
	p.Eval(fmt.Sprintf(`kakelDemo.fit(%d, %d)`, cols, rows), nil)
	p.Await("a screen of its own size that fills the page", fmt.Sprintf(
		`(() => { const s = kakelDemo.size(), w = %d * s.w / %d, h = %d * s.h / %d; return s.cols == %d && s.rows == %d && w <= 1 && h <= 1 && Math.max(w, h) > 0.75 })()`,
		cols, pageW, rows, pageH, cols, rows), longest)
	awaitColours(t, p, red)
	keep(t, p, "fit")
	p.Eval(`kakelDemo.fit(0, 0)`, nil)
	p.Await("the page at its own size", fits(pageW, pageH), longest)
	awaitColours(t, p, red)
}

// gaps has a page note the time between each two frames, until they are
// taken.
const gaps = `{ let last = 0; const all = window.frameGaps = []; const step = now => { if (last) all.push(now - last); last = now; if (window.frameGaps == all) requestAnimationFrame(step); }; requestAnimationFrame(step); } `

// A flood of output comes to its end, with the page drawing on the way.
// KAKEL_FLOOD is how many megabytes of it there are, in place of a
// quarter of one.
func TestAFloodOfOutputComesToItsEnd(t *testing.T) {
	p := demoPage(t)
	mb, err := strconv.ParseFloat(os.Getenv("KAKEL_FLOOD"), 64)
	if err != nil || mb <= 0 {
		mb = 0.25
	}
	var took float64
	p.Eval(fmt.Sprintf(gaps+`kakelDemo.burst(%d)`, int(mb*(1<<20))), &took)
	var frames []float64
	p.Eval(`(() => { const all = frameGaps; window.frameGaps = null; return all })()`, &frames)
	p.Await("the end of the flood", fmt.Sprintf(`(kakelDemo.screen() || "").includes("%s, ")`, demo.Done), longest)
	if slices.Sort(frames); len(frames) == 0 {
		// A short flood can be over before a slow machine draws twice.
		if took > 3000 {
			t.Errorf("%v MB took %.0f ms and the page drew nothing on the way", mb, took)
		}
	} else {
		t.Logf("%v MB took %.0f ms, %.1f MB/s; %d frames, %.0f ms apart in the middle, %.0f ms at most",
			mb, took, mb*1000/took, len(frames), frames[len(frames)/2], frames[len(frames)-1])
	}
	for _, line := range p.Console() {
		if strings.Contains(line, "gunim ") {
			t.Log(line)
		}
	}
	keep(t, p, "flood")
	// It is still there to be typed in.
	p.Type("after")
	awaitSent(t, p, 0, "after", "typing after the flood")
}

// A page reaches the other end of a relay, and bytes go both ways.
func TestAPageJoinsARelay(t *testing.T) {
	p := demoPage(t)
	select {
	case <-shared.up:
	case <-time.After(longest):
		t.Fatal("the relay never had the host")
	}
	// More than one message holds, so it is put together in order.
	text := strings.Repeat("through the relay and back, ", 10000)
	var back string
	p.Eval(fmt.Sprintf("kakelDemo.echo(%q, %q)", shared.host.Link(), text), &back)
	if back != text {
		t.Fatalf("of %d bytes %d came back", len(text), len(back))
	}
}
