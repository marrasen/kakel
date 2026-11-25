//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"sync/atomic"
	"syscall/js"

	"github.com/marrasen/gunim"

	"github.com/marrasen/kakel/guest"
	"github.com/marrasen/kakel/guestview"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/screen"
)

// drawn tells the page that there is something to see in the window:
// what it showed while the client loaded fades out, and the window in.
var drawn = sync.OnceFunc(func() {
	js.Global().Get("document").Get("body").Get("classList").Call("add", "ready")
	js.Global().Get("performance").Call("mark", "kakel-first-frame")
})

// opened is the link the page was opened with, and the secret in it. The
// error says nothing of either.
func opened() (relay.ShareLink, guest.Secret, error) {
	link, err := relay.ParseShareLink(js.Global().Get("location").Get("href").String())
	if err != nil {
		return relay.ShareLink{}, guest.Secret{}, errors.New("not a share's page")
	}
	secret, err := guest.ParseSecret(link.Secret)
	// Nothing needs the link's copy from here.
	link.Secret = ""
	return link, secret, err
}

// dropSecret takes what follows the # off the page's address, where
// anybody who looks at the screen can read it.
func dropSecret() {
	loc := js.Global().Get("location")
	js.Global().Get("history").Call("replaceState", js.Null(), "", loc.Get("pathname").String()+loc.Get("search").String())
}

// nameKey is what the browser keeps the guest's name under.
const nameKey = "kakel-guest-name"

// storage does something with one of the browser's storages, which a
// browser may refuse a page: it then throws, or has none.
func storage(name string, do func(js.Value)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			thrown, ok := r.(js.Error)
			if !ok {
				panic(r)
			}
			err = thrown
		}
	}()
	s := js.Global().Get(name)
	if s.IsUndefined() || s.IsNull() {
		return errors.New("no storage")
	}
	do(s)
	return nil
}

// load is what the storage named keeps under key, "" for nothing.
func load(name, key string) string {
	var got string
	_ = storage(name, func(s js.Value) {
		if v := s.Call("getItem", key); v.Type() == js.TypeString {
			got = v.String()
		}
	})
	return got
}

// store keeps value under key.
func store(name, key, value string) error {
	return storage(name, func(s js.Value) { s.Call("setItem", key, value) })
}

// kept is what the tab keeps of its share, in sessionStorage: the secret,
// which the host is known by, and the guest's own key.
type kept struct {
	id     string
	secret guest.Secret
	// saved says the tab kept it.
	saved bool
}

// keptText is what is kept, as it is written.
type keptText struct {
	Secret string `json:"secret,omitempty"`
	Key    string `json:"key,omitempty"`
	// Over says the share has come to its end in this tab, which has
	// nothing else of it: a reload says that, rather than that the
	// address is not whole.
	Over bool `json:"over,omitempty"`
}

func (k *kept) at() string { return "kakel-share-" + k.id }

// read is what was kept before.
func (k *kept) read() (guest.Secret, guest.Key, error) {
	var t keptText
	if k.id == "" {
		return guest.Secret{}, guest.Key{}, errors.New("no share")
	}
	if err := json.Unmarshal([]byte(load("sessionStorage", k.at())), &t); err != nil {
		return guest.Secret{}, guest.Key{}, errors.New("nothing was kept")
	}
	secret, err := guest.ParseSecret(t.Secret)
	if err != nil {
		return guest.Secret{}, guest.Key{}, errors.New("what was kept is not a secret")
	}
	key, err := guest.ParseKey(t.Key)
	if err != nil {
		return guest.Secret{}, guest.Key{}, errors.New("what was kept is not a key")
	}
	return secret, key, nil
}

// loadSecret is the secret kept before, for a page whose address has none.
func (k *kept) loadSecret() (guest.Secret, error) {
	secret, _, err := k.read()
	if err == nil {
		k.secret = secret
	}
	return secret, err
}

// Load implements guestview.Keep. A key kept with another secret is for
// another host.
func (k *kept) Load() (guest.Key, bool) {
	secret, key, err := k.read()
	if err != nil || secret != k.secret {
		return guest.Key{}, false
	}
	k.saved = true
	return key, true
}

// Save implements guestview.Keep.
func (k *kept) Save(key guest.Key) {
	text, err := json.Marshal(keptText{Secret: k.secret.String(), Key: key.String()})
	if err != nil {
		return
	}
	k.saved = store("sessionStorage", k.at(), string(text)) == nil && load("sessionStorage", k.at()) == string(text)
}

// Joined implements guestview.Keep. Where the tab keeps nothing, the
// address keeps its secret: a reload then finds out that the link was
// used, rather than that it is not whole.
func (k *kept) Joined() {
	if k.saved {
		dropSecret()
	}
}

// Clear implements guestview.Keep.
func (k *kept) Clear() {
	k.saved = false
	_ = storage("sessionStorage", func(s js.Value) { s.Call("removeItem", k.at()) })
	if text, err := json.Marshal(keptText{Over: true}); err == nil {
		_ = store("sessionStorage", k.at(), string(text))
	}
}

// ended reports whether the share has come to its end in this tab.
func (k *kept) ended() bool {
	var t keptText
	return k.id != "" && json.Unmarshal([]byte(load("sessionStorage", k.at())), &t) == nil && t.Over
}

// yielding is a function that lets the browser have its thread, to draw
// and to hear of keys, and returns once it comes back. Go gives it back
// only when all its goroutines wait, and a sleep too short to wait for
// is none. A message to the page itself comes back as soon as it may,
// as a message from the network does.
//
// Many goroutines may wait for it at once.
func yielding() func() {
	ch := js.Global().Get("MessageChannel").New()
	var mu sync.Mutex
	var waiting []chan struct{}
	ch.Get("port1").Set("onmessage", js.FuncOf(func(js.Value, []js.Value) any {
		mu.Lock()
		first := waiting[0]
		waiting = waiting[1:]
		mu.Unlock()
		close(first)
		return nil
	}))
	return func() {
		back := make(chan struct{})
		mu.Lock()
		waiting = append(waiting, back)
		ch.Get("port2").Call("postMessage", js.Null())
		mu.Unlock()
		<-back
	}
}

// waking has word each time the network may be back, or the page is shown
// again, as after the machine slept.
func waking(ctx context.Context) <-chan struct{} {
	wake := make(chan struct{}, 1)
	word := js.FuncOf(func(js.Value, []js.Value) any {
		if js.Global().Get("document").Get("visibilityState").String() != "hidden" {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		return nil
	})
	win := js.Global()
	win.Call("addEventListener", "online", word)
	win.Call("addEventListener", "pageshow", word)
	win.Get("document").Call("addEventListener", "visibilitychange", word)
	context.AfterFunc(ctx, func() {
		win.Call("removeEventListener", "online", word)
		win.Call("removeEventListener", "pageshow", word)
		win.Get("document").Call("removeEventListener", "visibilitychange", word)
	})
	return wake
}

// prefers reports whether the browser's user prefers value for feature.
func prefers(feature, value string) bool {
	m := js.Global().Call("matchMedia", "(prefers-"+feature+": "+value+")")
	return !m.IsUndefined() && m.Get("matches").Bool()
}

// still reports whether the browser's user asked for fewer animations.
func still() bool { return prefers("reduced-motion", "reduce") }

// coarse reports whether a finger is what the page is mostly worked with.
func coarse() bool {
	m := js.Global().Call("matchMedia", "(pointer: coarse)")
	return !m.IsUndefined() && m.Get("matches").Bool()
}

// mac reports whether the keys are a Mac's, or an iPad's.
func mac() bool {
	p := js.Global().Get("navigator").Get("platform")
	return p.Type() == js.TypeString && (strings.HasPrefix(p.String(), "Mac") || strings.HasPrefix(p.String(), "iP"))
}

// canFill reports whether the page can fill the screen.
func canFill() bool {
	on := js.Global().Get("document").Get("fullscreenEnabled")
	return on.Type() == js.TypeBoolean && on.Bool()
}

// about is the browser and the system, as the guest says them.
func about() string { return guestview.About(js.Global().Get("navigator").Get("userAgent").String()) }

// in says the guest is on its way into the window, or in it: another link
// opened in the tab does not take it away.
var in atomic.Bool

// onState has the page round the window follow it.
func onState(st guestview.State) {
	in.Store(st.Page == guestview.PageWaiting || st.Page == guestview.PageWindow || st.Busy)
	paintGround(st.Theme.Palette.BG)
}

// told tells the tests what the page shows, in a build for them, shells
// has them read the window's terminals, and opens hands them the window.
var (
	told   func(guestview.State)
	opens  = func(*gunim.Window) {}
	shells *screen.Shells
)

// ground is the colour the page is in round the window.
var ground string

// paintGround has the page itself in the window's colour, which shows
// while a window is sized, and at the edges of a phone.
func paintGround(c color.RGBA) {
	hex := fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
	if hex == ground {
		return
	}
	ground = hex
	doc := js.Global().Get("document")
	doc.Get("documentElement").Get("style").Set("backgroundColor", hex)
	doc.Get("body").Get("style").Set("backgroundColor", hex)
	metas := doc.Call("querySelectorAll", `meta[name="theme-color"]`)
	for i := range metas.Length() {
		metas.Index(i).Set("content", hex)
	}
}

// Another link pasted over the address of the same page changes what
// follows the # alone, which no browser loads the page anew for.
func init() {
	js.Global().Call("addEventListener", "hashchange", js.FuncOf(func(js.Value, []js.Value) any {
		if loc := js.Global().Get("location"); loc.Get("hash").String() != "" && !in.Load() {
			loc.Call("reload")
		}
		return nil
	}))
}
