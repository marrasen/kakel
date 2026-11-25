//go:build js && wasm

package main

import (
	"sync"
	"syscall/js"
)

// drawn tells the page that there is something to see in the window:
// what it showed while the client loaded fades out, and the window in.
var drawn = sync.OnceFunc(func() {
	js.Global().Get("document").Get("body").Get("classList").Call("add", "ready")
	js.Global().Get("performance").Call("mark", "kakel-first-frame")
})

// dropSecret takes what follows the # off the page's address, where
// anybody who looks at the screen can read it.
//
// It is for the client to call once the share has taken its guest in,
// and once the tab keeps the guest's own key in sessionStorage, which a
// reload then comes back with. Before that a reload has nothing but the
// address. Nothing calls it yet.
func dropSecret() {
	loc := js.Global().Get("location")
	js.Global().Get("history").Call("replaceState", js.Null(), "", loc.Get("pathname").String()+loc.Get("search").String())
}
