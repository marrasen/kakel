// Package webtest runs kakel's browser client in headless Chrome for the
// tests: it builds the client, serves it, opens it in a page, and looks
// at what the page draws and does with what is typed and clicked.
//
// It drives Chrome over the DevTools protocol. Where there is no Chrome
// the tests skip.
package webtest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// Build builds the client into dir, with the page and the files it
// needs.
func Build(dir string) error {
	mod, err := output("go", "env", "GOMOD")
	if err != nil {
		return err
	}
	if mod == "" || mod == os.DevNull {
		return errors.New("building the client: not in kakel's source")
	}
	root := filepath.Dir(mod)
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", filepath.Join(dir, "kakel.wasm"), "./webclient")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm", "CGO_ENABLED=0", "GOFLAGS="+forWasm(os.Getenv("GOFLAGS")))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("building the client: %w\n%s", err, out)
	}
	goroot, err := output("go", "env", "GOROOT")
	if err != nil {
		return err
	}
	for from, to := range map[string]string{
		filepath.Join(goroot, "lib", "wasm", "wasm_exec.js"): "wasm_exec.js",
		filepath.Join(root, "webclient", "index.html"):       "index.html",
	} {
		b, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, to), b, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// forWasm is flags for every go command, less those a build for a
// browser has no support for.
func forWasm(flags string) string {
	var out []string
	for _, f := range strings.Fields(flags) {
		name, _, _ := strings.Cut(strings.TrimLeft(f, "-"), "=")
		if !slices.Contains([]string{"race", "msan", "asan", "cover", "covermode", "coverpkg"}, name) {
			out = append(out, f)
		}
	}
	return strings.Join(out, " ")
}

// output is what a command prints, trimmed.
func output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}

// ErrNoChrome says there is no Chrome here to run the tests in.
var ErrNoChrome = errors.New("no Chrome or Chromium found, and KAKEL_CHROME names none")

// FindChrome is the Chrome to run the tests in: the one KAKEL_CHROME
// names, or the first found. The page is the same whatever system the
// browser is on, so there is none on Windows.
func FindChrome() (string, error) {
	if runtime.GOOS == "windows" {
		// Chrome is driven through files a Windows program is not given.
		return "", ErrNoChrome
	}
	if name := os.Getenv("KAKEL_CHROME"); name != "" {
		path, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("%w: %w", ErrNoChrome, err)
		}
		return path, nil
	}
	for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil && !fromSnap(path) {
			return path, nil
		}
	}
	return "", ErrNoChrome
}

// fromSnap reports whether the browser at path is a snap, or a script
// that starts one. A snap has a /tmp of its own, where its profile would
// be out of the tests' sight.
func fromSnap(path string) bool {
	if at, err := filepath.EvalSymlinks(path); err == nil {
		path = at
	}
	if strings.HasPrefix(path, "/snap/") || filepath.Base(path) == "snap" {
		return true
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > 64<<10 {
		return false
	}
	script, err := os.ReadFile(path)
	return err == nil && bytes.HasPrefix(script, []byte("#!")) && bytes.Contains(script, []byte("/snap/"))
}

// Browser is a headless Chrome.
type Browser struct {
	cmd *exec.Cmd
	// to is where Chrome reads commands.
	to     *os.File
	toMu   sync.Mutex
	cancel context.CancelFunc
	// dir is its profile, ended is closed once it has ended, and says is
	// what it printed.
	dir   string
	ended chan struct{}
	says  lockedBuffer

	mu      sync.Mutex
	id      int
	answers map[int]chan message
	pages   map[string]*Page
	err     error
}

// lockedBuffer is what a program prints, as it prints it.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// message is what Chrome sends: the answer to a command, or an event.
type message struct {
	ID        int             `json:"id"`
	SessionID string          `json:"sessionId"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	Result    json.RawMessage `json:"result"`
	Error     *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// answerWithin is how long Chrome may take to answer a command. A page
// busy with a flood of output answers once it has a moment.
const answerWithin = time.Minute

// Start starts a headless Chrome, until it is closed. The error is
// ErrNoChrome where there is none.
func Start() (*Browser, error) {
	chrome, err := FindChrome()
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "kakel-chrome")
	if err != nil {
		return nil, err
	}
	args := []string{
		"--headless=new", "--remote-debugging-pipe", "--user-data-dir=" + dir,
		"--no-first-run", "--no-default-browser-check", "--disable-extensions", "--disable-background-networking",
		// A container's shared memory is too small for a page's pictures.
		"--disable-dev-shm-usage",
		// Drawn by the processor, as the machines the tests run on have
		// no graphics card, and drawn the same on all of them.
		"--use-angle=swiftshader", "--enable-unsafe-swiftshader", "--force-color-profile=srgb",
		"--window-size=1000,700",
	}
	if runtime.GOOS == "linux" {
		// The machines that run the checks let no program make the
		// namespaces Chrome's sandbox is made of. It opens nothing but
		// the page the test serves.
		args = append(args, "--no-sandbox")
	}
	// Chrome reads commands from file 3 and answers on file 4, and ends
	// when they close: it outlives no test, however that ends.
	commandsR, commandsW, err := os.Pipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	answersR, answersW, err := os.Pipe()
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	b := &Browser{to: commandsW, cancel: cancel, dir: dir, ended: make(chan struct{}),
		answers: map[int]chan message{}, pages: map[string]*Page{}}
	cmd := exec.CommandContext(ctx, chrome, append(args, "about:blank")...)
	// Headless, and with no display to show itself on besides.
	cmd.Env = slices.DeleteFunc(os.Environ(), func(v string) bool {
		return strings.HasPrefix(v, "DISPLAY=") || strings.HasPrefix(v, "WAYLAND_DISPLAY=")
	})
	cmd.ExtraFiles = []*os.File{commandsR, answersW}
	cmd.Stderr = &b.says
	cmd.WaitDelay = 5 * time.Second
	together(cmd)
	b.cmd = cmd
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("starting %s: %w", chrome, err)
	}
	_ = commandsR.Close()
	_ = answersW.Close()
	go func() { _ = cmd.Wait(); close(b.ended) }()
	go b.read(answersR)
	// Its first answer says it is up.
	if err := b.call("", "Browser.getVersion", nil, nil); err != nil {
		b.Close()
		return nil, fmt.Errorf("%s did not answer: %w\n%s", chrome, err, b.says.String())
	}
	return b, nil
}

// Close ends the browser, with every process it started.
func (b *Browser) Close() {
	// Asked to leave first, and let go of, which it leaves at as well.
	_ = b.call("", "Browser.close", nil, nil)
	_ = b.to.Close()
	select {
	case <-b.ended:
	case <-time.After(10 * time.Second):
	}
	b.cancel()
	<-b.ended
	endAll(b.cmd)
	// Its last processes may still be letting go of the folder.
	for range 20 {
		if os.RemoveAll(b.dir) == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// read hands out what Chrome sends: JSON messages, a zero byte after
// each.
func (b *Browser) read(from *os.File) {
	defer func() { _ = from.Close() }()
	in := bufio.NewReader(from)
	for {
		raw, err := in.ReadBytes(0)
		if err != nil {
			b.mu.Lock()
			b.err = err
			for id, ch := range b.answers {
				delete(b.answers, id)
				close(ch)
			}
			b.mu.Unlock()
			return
		}
		raw = raw[:len(raw)-1]
		var m message
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		b.mu.Lock()
		ch := b.answers[m.ID]
		page := b.pages[m.SessionID]
		if m.ID != 0 {
			delete(b.answers, m.ID)
		}
		b.mu.Unlock()
		switch {
		case m.ID != 0 && ch != nil:
			ch <- m
		case m.ID == 0 && page != nil:
			page.event(m)
		}
	}
}

// call sends a command to a page's session, or to the browser for "",
// and reads its answer into out.
func (b *Browser) call(session, method string, params, out any) error {
	if params == nil {
		params = struct{}{}
	}
	ch := make(chan message, 1)
	b.mu.Lock()
	if b.err != nil {
		b.mu.Unlock()
		return fmt.Errorf("%s: %w", method, b.err)
	}
	b.id++
	id := b.id
	b.answers[id] = ch
	b.mu.Unlock()
	raw, err := json.Marshal(struct {
		ID        int    `json:"id"`
		SessionID string `json:"sessionId,omitempty"`
		Method    string `json:"method"`
		Params    any    `json:"params"`
	}{id, session, method, params})
	if err != nil {
		return err
	}
	b.toMu.Lock()
	_, err = b.to.Write(append(raw, 0))
	b.toMu.Unlock()
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	select {
	case m, ok := <-ch:
		switch {
		case !ok:
			return fmt.Errorf("%s: Chrome has gone", method)
		case m.Error != nil:
			return fmt.Errorf("%s: %s", method, m.Error.Message)
		case out == nil:
			return nil
		}
		return json.Unmarshal(m.Result, out)
	case <-time.After(answerWithin):
		return fmt.Errorf("%s: no answer in %v", method, answerWithin)
	}
}

// Page is a page open in the browser.
type Page struct {
	t       testing.TB
	b       *Browser
	session string
	target  string

	mu   sync.Mutex
	said []string
	bad  []string
}

// Open opens url in a page of its own, width by height CSS pixels at
// scale device pixels each, with the keyboard. first, when there is any,
// is JavaScript run before the page's own.
func (b *Browser) Open(t testing.TB, url string, width, height int, scale float64, first string) *Page {
	t.Helper()
	p := &Page{t: t, b: b}
	var target struct {
		TargetID string `json:"targetId"`
	}
	if err := b.call("", "Target.createTarget", map[string]any{"url": "about:blank"}, &target); err != nil {
		t.Fatal(err)
	}
	p.target = target.TargetID
	var attached struct {
		SessionID string `json:"sessionId"`
	}
	if err := b.call("", "Target.attachToTarget", map[string]any{"targetId": target.TargetID, "flatten": true}, &attached); err != nil {
		t.Fatal(err)
	}
	p.session = attached.SessionID
	b.mu.Lock()
	b.pages[p.session] = p
	b.mu.Unlock()
	p.Call("Runtime.enable", nil, nil)
	p.Call("Log.enable", nil, nil)
	p.Call("Page.enable", nil, nil)
	p.Call("Emulation.setFocusEmulationEnabled", map[string]any{"enabled": true}, nil)
	p.Size(width, height, scale)
	if first != "" {
		p.Call("Page.addScriptToEvaluateOnNewDocument", map[string]any{"source": first}, nil)
	}
	p.Call("Page.navigate", map[string]any{"url": url}, nil)
	return p
}

// For has the page's calls fail t from here on, and forgets the errors
// it had: the next test's page.
func (p *Page) For(t testing.TB) {
	p.t = t
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bad, p.said = nil, nil
}

// Close closes the page.
func (p *Page) Close() {
	_ = p.b.call("", "Target.closeTarget", map[string]any{"targetId": p.target}, nil)
}

// event takes what the page says on its own: its console and its errors.
func (p *Page) event(m message) {
	var say struct {
		Type string `json:"type"`
		Args []struct {
			Value       any    `json:"value"`
			Description string `json:"description"`
		} `json:"args"`
		Exception struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
		Entry struct {
			Level string `json:"level"`
			Text  string `json:"text"`
			URL   string `json:"url"`
		} `json:"entry"`
	}
	if json.Unmarshal(m.Params, &say) != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch m.Method {
	case "Runtime.consoleAPICalled":
		var words []string
		for _, a := range say.Args {
			if a.Value != nil {
				words = append(words, fmt.Sprint(a.Value))
			} else {
				words = append(words, a.Description)
			}
		}
		line := strings.Join(words, " ")
		p.said = append(p.said, line)
		// Go's own crashes come as plain lines.
		if say.Type == "error" || strings.Contains(line, "panic:") || strings.Contains(line, "fatal error:") {
			p.bad = append(p.bad, line)
		}
	case "Runtime.exceptionThrown":
		p.bad = append(p.bad, say.Exception.Text+" "+say.Exception.Exception.Description)
	case "Log.entryAdded":
		p.said = append(p.said, say.Entry.Text)
		if say.Entry.Level == "error" {
			p.bad = append(p.bad, say.Entry.Text+" "+say.Entry.URL)
		}
	}
}

// Console is every line the page has said on its console.
func (p *Page) Console() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.said...)
}

// Errors are the errors the page has had.
func (p *Page) Errors() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.bad...)
}

// Call sends the page a command of the DevTools protocol, and reads its
// answer into out when there is one. It fails the test when the command
// does.
func (p *Page) Call(method string, params, out any) {
	p.t.Helper()
	if err := p.b.call(p.session, method, params, out); err != nil {
		p.t.Fatal(err)
	}
}

// Eval runs JavaScript in the page and reads its value into out, after
// waiting for it when it is a promise.
func (p *Page) Eval(js string, out any) {
	p.t.Helper()
	if err := p.TryEval(js, out); err != nil {
		p.t.Fatal(err)
	}
}

// TryEval is Eval, returning what went wrong.
func (p *Page) TryEval(js string, out any) error {
	var got struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		Exception *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
				Value       any    `json:"value"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := p.b.call(p.session, "Runtime.evaluate", map[string]any{"expression": js, "returnByValue": true, "awaitPromise": true}, &got); err != nil {
		return err
	}
	if e := got.Exception; e != nil {
		return fmt.Errorf("%s: %s %s %v", js, e.Text, e.Exception.Description, e.Exception.Value)
	}
	if out == nil || len(got.Result.Value) == 0 {
		return nil
	}
	return json.Unmarshal(got.Result.Value, out)
}

// Await waits until JavaScript js is true, for as long as within.
func (p *Page) Await(what, js string, within time.Duration) {
	p.t.Helper()
	for end := time.Now().Add(within); ; time.Sleep(20 * time.Millisecond) {
		var ok bool
		err := p.TryEval("!!("+js+")", &ok)
		if err == nil && ok {
			return
		}
		if time.Now().After(end) {
			p.t.Fatalf("%s: not in %v (%v). The page's errors: %q. Its console: %q", what, within, err, p.Errors(), p.Console())
		}
	}
}

// Size sizes the page.
func (p *Page) Size(width, height int, scale float64) {
	p.t.Helper()
	p.Call("Emulation.setDeviceMetricsOverride", map[string]any{
		"width": width, "height": height, "deviceScaleFactor": scale, "mobile": false,
	}, nil)
}

// Shot is what the page shows, in device pixels. It is slow where the
// processor draws: a second and more for a large page.
func (p *Page) Shot() image.Image {
	p.t.Helper()
	img, err := png.Decode(bytes.NewReader(p.PNG()))
	if err != nil {
		p.t.Fatal(err)
	}
	return img
}

// PNG is what the page shows, as a PNG file.
func (p *Page) PNG() []byte {
	p.t.Helper()
	var got struct {
		Data string `json:"data"`
	}
	p.Call("Page.captureScreenshot", map[string]any{"format": "png"}, &got)
	raw, err := base64.StdEncoding.DecodeString(got.Data)
	if err != nil {
		p.t.Fatal(err)
	}
	return raw
}

// The modifiers held with a key, as the DevTools protocol counts them.
const (
	Alt   = 1
	Ctrl  = 2
	Meta  = 4
	Shift = 8
)

// Key is a key pressed: its value and its place, as a browser names
// them, the old number some browsers' own shortcuts go by, the text it
// types, if any, and the editing the browser does for it, as "paste" is.
type Key struct {
	Name, Code string
	Number     int
	Text       string
	Mods       int
	Commands   []string
}

// Press presses a key and lets it go.
func (p *Page) Press(k Key) {
	p.t.Helper()
	params := map[string]any{
		"type": "rawKeyDown", "key": k.Name, "code": k.Code, "modifiers": k.Mods,
		"windowsVirtualKeyCode": k.Number, "nativeVirtualKeyCode": k.Number,
	}
	if len(k.Commands) > 0 {
		params["commands"] = k.Commands
	}
	p.Call("Input.dispatchKeyEvent", params, nil)
	delete(params, "commands")
	if k.Text != "" {
		// The character follows the key, as Windows has it, and one AltGr
		// types comes with no Ctrl and no Alt.
		params["type"], params["text"] = "char", k.Text
		params["modifiers"] = k.Mods &^ (Ctrl | Alt)
		p.Call("Input.dispatchKeyEvent", params, nil)
		params["modifiers"] = k.Mods
	}
	delete(params, "text")
	params["type"] = "keyUp"
	p.Call("Input.dispatchKeyEvent", params, nil)
}

// Type types text, a key for each letter.
func (p *Page) Type(text string) {
	p.t.Helper()
	for _, r := range text {
		k := Key{Name: string(r), Text: string(r)}
		switch {
		case r == '\r':
			k = Key{Name: "Enter", Code: "Enter", Number: 13, Text: "\r"}
		case r == ' ':
			k.Code, k.Number = "Space", 32
		case r >= 'a' && r <= 'z':
			k.Code, k.Number = "Key"+strings.ToUpper(string(r)), int(r-'a'+'A')
		case r >= 'A' && r <= 'Z':
			k.Code, k.Number, k.Mods = "Key"+string(r), int(r), Shift
		case r >= '0' && r <= '9':
			k.Code, k.Number = "Digit"+string(r), int(r)
		}
		p.Press(k)
	}
}

// Click clicks a button, "left", "middle" or "right", at a place so many
// times, one right after the other. The times the clicks say they came
// at are the test's, so a machine that is slow to hand them over does
// not make two clicks of a double one.
func (p *Page) Click(x, y float64, button string, times int) {
	p.t.Helper()
	// Past, as a click is by the time the page hears of it.
	at := time.Now().Add(-time.Duration(times) * 100 * time.Millisecond)
	send := func(kind string, held, n int) {
		p.t.Helper()
		at = at.Add(20 * time.Millisecond)
		p.Call("Input.dispatchMouseEvent", map[string]any{
			"type": kind, "x": x, "y": y, "button": button, "buttons": held, "clickCount": n,
			"timestamp": float64(at.UnixNano()) / 1e9,
		}, nil)
	}
	held := map[string]int{"left": 1, "right": 2, "middle": 4}[button]
	for n := 1; n <= times; n++ {
		send("mousePressed", held, n)
		send("mouseReleased", 0, n)
	}
}

// Compose types text through an input method: what it is composing
// first, a step at a time, and then the text itself.
func (p *Page) Compose(text string, steps ...string) {
	p.t.Helper()
	for _, s := range steps {
		p.Call("Input.imeSetComposition", map[string]any{"text": s, "selectionStart": len([]rune(s)), "selectionEnd": len([]rune(s))}, nil)
	}
	p.Call("Input.insertText", map[string]any{"text": text}, nil)
}

// Allow lets pages at origin use what is named, such as
// "clipboardReadWrite", without asking.
func (b *Browser) Allow(t testing.TB, origin string, names ...string) {
	t.Helper()
	if err := b.call("", "Browser.grantPermissions", map[string]any{"origin": origin, "permissions": names}, nil); err != nil {
		t.Fatal(err)
	}
}
