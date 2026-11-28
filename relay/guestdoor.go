//go:build !js

package relay

import (
	"crypto/sha256"
	"encoding/base64"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// What a guest is sent of the web client may take a while over a slow
// network, as long as it moves: each part of it is held to partWithin,
// and all of it to fileWithin.
const (
	partBytes  = 64 << 10
	fileWithin = 20 * time.Minute
)

var partWithin = 30 * time.Second

// serveGuest answers on a guest's doors: path is what follows /s/.
func (s *Server) serveGuest(w http.ResponseWriter, r *http.Request, path string) {
	id, file, slash := strings.Cut(path, "/")
	guestHeaders(w, r)
	if slash && file == doorJoin {
		s.serveGuestJoin(w, r, id)
		return
	}
	if !allowed(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	from := clientAddr(r)
	sh, b := s.shared(id)
	switch {
	case sh == nil && s.settling() && isShareID(id):
		settlingPage.serve(w, r)
		return
	case sh == nil && s.missing(s.lost, from, time.Now()):
		w.Header().Set("Retry-After", strconv.Itoa(int(missWindow/time.Second)))
		tooManyPage.serve(w, r)
		return
	case sh == nil:
		s.missed(s.lost, from, time.Now())
		expiredPage.serve(w, r)
		return
	case !slash:
		// Relative, so a proxy's folder before it stays. What follows the
		// # stays too, in the browser.
		w.Header().Set("Location", id+"/")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusTemporaryRedirect)
		return
	case file == "":
		file = indexName
	}
	f := b.files[file]
	if f == nil {
		http.NotFound(w, r)
		return
	}
	done := s.sendingTo(sh, from)
	if done == nil {
		w.Header().Set("Retry-After", "5")
		http.Error(w, "too many are downloading this at once; try again", http.StatusServiceUnavailable)
		return
	}
	defer done()
	w.Header().Set("Content-Security-Policy", clientPolicy(askedHost(r)))
	serveFile(w, r, f, file == indexName)
}

// guestHeaders are on every answer to a guest. The policy lets nothing
// in: a page that needs something says so itself.
func guestHeaders(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", closedPolicy)
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cross-Origin-Opener-Policy", "same-origin")
	h.Set("Cross-Origin-Resource-Policy", "same-origin")
	h.Set("X-Robots-Tag", "noindex, nofollow")
	// Which way a device is held is let through: gunim listens for it as
	// it starts, and a browser logs that as an error where it may not.
	h.Set("Permissions-Policy", "clipboard-read=(self), clipboard-write=(self), fullscreen=(self), "+
		"autoplay=(), camera=(), display-capture=(), geolocation=(), hid=(), microphone=(), midi=(), payment=(), serial=(), usb=()")
	if r.TLS != nil {
		h.Set("Strict-Transport-Security", "max-age=31536000")
	}
}

// closedPolicy is a Content-Security-Policy that lets a page do nothing.
const closedPolicy = "default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'"

// plainHost is a host as it may stand in a policy: nothing that ends
// the host and begins something else.
var plainHost = regexp.MustCompile(`^(\[[0-9a-fA-F:.]+\]|[a-zA-Z0-9._-]+)(:[0-9]+)?$`)

// clientPolicy is the Content-Security-Policy of the web client, on the
// relay reached as host: its own scripts, styles, images and fonts, its
// own stream, and its own frames, which a download is asked for in. Some
// browsers read 'self' as https alone, so the stream's host is named too.
// Its save worker, a service worker, is a script of its own: with no
// worker-src, script-src is what lets it in.
func clientPolicy(host string) string {
	connect := "'self'"
	if plainHost.MatchString(host) {
		connect += " wss://" + host + " ws://" + host
	}
	return closedPolicy + "; script-src 'self' 'wasm-unsafe-eval'; style-src 'self'; img-src 'self'; font-src 'self'; " +
		"frame-src 'self'; connect-src " + connect
}

// serveFile serves a file of a bundle. The index is never kept by a
// browser, and the rest is asked about each time.
func serveFile(w http.ResponseWriter, r *http.Request, f *bundleFile, index bool) {
	h := w.Header()
	h.Set("Content-Type", f.kind)
	if index {
		h.Set("Cache-Control", "no-store")
	} else {
		tag := `"` + f.hash + `"`
		h.Set("Cache-Control", "private, no-cache")
		h.Set("ETag", tag)
		if namesTag(r.Header.Get("If-None-Match"), tag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	if f.gzipped {
		h.Set("Vary", "Accept-Encoding")
		// Never unpacked here, which would cost the relay more than it
		// costs whoever asks. Every browser the client runs in takes it.
		if refusesGzip(r) {
			http.Error(w, "this takes a browser that reads gzip", http.StatusNotAcceptable)
			return
		}
		h.Set("Content-Encoding", "gzip")
	}
	h.Set("Content-Length", strconv.Itoa(len(f.data)))
	if r.Method == http.MethodHead {
		return
	}
	c := http.NewResponseController(w)
	last := time.Now().Add(fileWithin)
	for data := f.data; len(data) > 0; {
		part := data[:min(partBytes, len(data))]
		data = data[len(part):]
		by := time.Now().Add(partWithin)
		if by.After(last) {
			by = last
		}
		_ = c.SetWriteDeadline(by)
		if _, err := w.Write(part); err != nil {
			return
		}
	}
}

// refusesGzip reports whether a request says what it takes and gzip is
// not among it. One that says nothing takes anything, and some proxies
// take what a browser says away.
func refusesGzip(r *http.Request) bool {
	takes, said := false, false
	for _, v := range r.Header.Values("Accept-Encoding") {
		for part := range strings.SplitSeq(v, ",") {
			name, params, _ := strings.Cut(part, ";")
			name = strings.ToLower(strings.TrimSpace(name))
			said = said || name != ""
			if name != "gzip" && name != "x-gzip" && name != "*" {
				continue
			}
			ok := true
			if q, has := strings.CutPrefix(strings.ReplaceAll(strings.ToLower(params), " ", ""), "q="); has {
				f, err := strconv.ParseFloat(q, 64)
				ok = err == nil && f > 0
			}
			if name != "*" {
				return !ok
			}
			takes = ok
		}
	}
	return said && !takes
}

// namesTag reports whether an If-None-Match names tag.
func namesTag(header, tag string) bool {
	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimPrefix(strings.TrimSpace(part), "W/")
		if part == tag || part == "*" {
			return true
		}
	}
	return false
}

// notice is a page of the relay's own: a sentence.
type notice struct {
	status       int
	body, policy string
}

// The relay's own pages. None has a script but the one that tries again
// by itself.
var (
	expiredPage = newNotice(http.StatusNotFound, "This link has expired or was already used",
		"Ask for a new link.", "")
	tooManyPage  = newNotice(http.StatusTooManyRequests, "Too many tries", "Wait a minute, then reload the page.", "")
	settlingPage = newNotice(http.StatusServiceUnavailable, "Connecting", "The page reloads by itself.", noticeAgain)
)

// noticeAgain reloads the page a moment later. A browser asked to by the
// page's head does not, as what follows the # in a link makes it read as
// a place on the page it is on.
const noticeAgain = "setTimeout(() => location.reload(), 3000)"

// noticeStyle is what the relay's own pages look like.
const noticeStyle = `
:root { color-scheme: light dark; --page: #f6f7f9; --text: #1b2027; --quiet: #596270; }
@media (prefers-color-scheme: dark) { :root { --page: #14171c; --text: #e6e9ee; --quiet: #97a1b0; } }
html, body { margin: 0; height: 100%; }
body { display: grid; place-items: center; background: var(--page); color: var(--text); font: 16px/1.5 system-ui, sans-serif; }
main { max-width: 32rem; padding: 24px; text-align: center; animation: in .35s ease-out; }
h1 { margin: 0 0 8px; font-size: 1.35rem; font-weight: 600; line-height: 1.3; }
p { margin: 0; color: var(--quiet); }
@keyframes in { from { opacity: 0; transform: translateY(8px); } }
@media (prefers-reduced-motion: reduce) { main { animation: none; } }
`

// newNotice is the page saying title, and text under it, with script
// when there is any. The web client's page reads both out of it, as its
// main's h1 and p, when its download is answered with one.
func newNotice(status int, title, text, script string) notice {
	// Let through by their hashes, and nothing else.
	hash := func(s string) string {
		sum := sha256.Sum256([]byte(s))
		return "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
	}
	policy := closedPolicy + "; style-src " + hash(noticeStyle)
	if script != "" {
		policy += "; script-src " + hash(script)
		script = "<script>" + script + "</script>\n"
	}
	return notice{
		status: status,
		body: "<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n" +
			"<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n" +
			"<title>kakel</title>\n<style>" + noticeStyle + "</style>\n</head>\n<body>\n<main>\n<h1>" +
			html.EscapeString(title) + "</h1>\n<p>" + html.EscapeString(text) + "</p>\n</main>\n" + script + "</body>\n</html>\n",
		policy: policy,
	}
}

// serve answers a request with the page.
func (n notice) serve(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Security-Policy", n.policy)
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(n.body)))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(n.status)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, n.body)
	}
}
