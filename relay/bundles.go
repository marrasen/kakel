//go:build !js

package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"time"
)

// signatureHeader carries a bundle's signature, as gunimsign writes it.
const signatureHeader = "Kakel-Signature"

// The limits of the web clients kept. They are kept in memory: a host
// gives its own again to a relay that started over.
const (
	// mostBundles is how many there may be, and mostBundleBytes how much
	// that may come to.
	mostBundles     = 12
	mostBundleBytes = 192 << 20
	// mostBundlesFrom and mostBundleBytesFrom are the same for those
	// uploaded from one address, so none of them has all the room.
	mostBundlesFrom     = 4
	mostBundleBytesFrom = 64 << 20
	// mostUploads is how many may be uploaded at once, one from each
	// address.
	mostUploads = 4
	// uploadTurn is how long one waits for its turn.
	uploadTurn = 10 * time.Second
	// uploadPart is how much of one is to come in each uploadPartWithin.
	uploadPart = 256 << 10
)

// uploadWithin is how long an upload may take, and uploadPartWithin how
// long each part of it: one that trickles is let go long before.
var (
	uploadWithin     = 5 * time.Minute
	uploadPartWithin = 15 * time.Second
)

// keptBundle is a web client the relay keeps.
type keptBundle struct {
	*Bundle
	// from is the address it was uploaded from, as addrKey has it.
	from string
	// shares is how many shares it is in use by, and used is when it was
	// last asked for.
	shares int
	used   time.Time
}

// About is what a relay can do.
type About struct {
	// Shares is the version of the doors that share a window, and 0 for a
	// relay with none.
	Shares int `json:"shares"`
	// Bundle is how large a web client it takes, in bytes.
	Bundle int64 `json:"bundle"`
	// Unsigned says it takes one that is not signed.
	Unsigned bool `json:"unsigned,omitempty"`
}

// sharesVersion is About.Shares of this relay.
const sharesVersion = 1

// serveAbout says what the relay can do.
func (s *Server) serveAbout(w http.ResponseWriter, r *http.Request) {
	if !allowed(w, r, http.MethodGet, http.MethodHead) {
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(About{Shares: sharesVersion, Bundle: MaxBundle, Unsigned: s.AllowUnsigned})
}

// allowed reports whether a request's method is one of methods, and
// answers it when it is not.
func allowed(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, m := range methods {
		if r.Method == m {
			return true
		}
	}
	for _, m := range methods {
		w.Header().Add("Allow", m)
	}
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

// serveBundle says whether the relay has the web client hash, and takes
// it.
func (s *Server) serveBundle(w http.ResponseWriter, r *http.Request, hash string) {
	if !allowed(w, r, http.MethodHead, http.MethodPut) {
		return
	}
	if !isHash(hash) {
		http.Error(w, "a web client is known by its SHA-256", http.StatusBadRequest)
		return
	}
	if r.Method == http.MethodHead {
		if !s.touchBundle(hash) {
			http.Error(w, "the relay does not have that web client", http.StatusNotFound)
		}
		return
	}
	s.takeBundle(w, r, hash)
}

// takeBundle takes the web client hash.
func (s *Server) takeBundle(w http.ResponseWriter, r *http.Request, hash string) {
	sig := r.Header.Get(signatureHeader)
	switch {
	case sig == "" && !s.AllowUnsigned:
		// Before it is sent.
		http.Error(w, releasedOnly, http.StatusForbidden)
		return
	case r.ContentLength <= 0:
		http.Error(w, "a web client is uploaded with its length", http.StatusLengthRequired)
		return
	case r.ContentLength > MaxBundle:
		http.Error(w, fmt.Sprintf("the relay takes a web client of %d bytes at most", MaxBundle), http.StatusRequestEntityTooLarge)
		return
	}
	from := addrKey(clientAddr(r))
	done := s.uploadFrom(r.Context(), from)
	if done == nil {
		http.Error(w, "the relay is taking as many web clients as it takes at once; try again", http.StatusServiceUnavailable)
		return
	}
	defer done()
	if s.touchBundle(hash) {
		// Another kakel gave it meanwhile.
		w.WriteHeader(http.StatusOK)
		return
	}
	zipped, err := readUpload(w, r)
	if err != nil {
		http.Error(w, "the web client did not arrive in full", http.StatusBadRequest)
		return
	}
	if BundleHash(zipped) != hash {
		http.Error(w, "the web client that arrived is another than was said", http.StatusBadRequest)
		return
	}
	b, err := OpenBundle(zipped)
	if err != nil {
		s.logf("relay: a web client from %s was turned away: %v", clientAddr(r), err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if !s.AllowUnsigned && !b.signedWith(s.key, sig) {
		s.logf("relay: a web client from %s was turned away: its signature is not by kakel's release key", clientAddr(r))
		http.Error(w, releasedOnly, http.StatusForbidden)
		return
	}
	if !s.keepBundle(b, from) {
		http.Error(w, "the relay has no room for another web client", http.StatusInsufficientStorage)
		return
	}
	s.logf("relay: web client %s, %d bytes, from %s", hash[:12], len(zipped), clientAddr(r))
	w.WriteHeader(http.StatusCreated)
}

// uploadFrom waits for the turn of an upload from the address key, and
// returns what is called once it is done, or nil when it had none.
func (s *Server) uploadFrom(ctx context.Context, key string) (done func()) {
	ctx, cancel := context.WithTimeout(ctx, uploadTurn)
	defer cancel()
	for {
		s.mu.Lock()
		if len(s.uploading) < mostUploads && !s.uploading[key] {
			s.uploading[key] = true
			s.mu.Unlock()
			return func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				delete(s.uploading, key)
				close(s.uploaded)
				s.uploaded = make(chan struct{})
			}
		}
		next := s.uploaded
		s.mu.Unlock()
		select {
		case <-next:
		case <-ctx.Done():
			return nil
		}
	}
}

// readUpload reads an upload, in as much room as has come of it and no
// more, as long as it keeps coming.
func readUpload(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	c := http.NewResponseController(w)
	last := time.Now().Add(uploadWithin)
	_ = c.SetWriteDeadline(last.Add(replyWithin))
	var zipped bytes.Buffer
	for left := r.ContentLength; left > 0; {
		by := time.Now().Add(uploadPartWithin)
		if by.After(last) {
			by = last
		}
		_ = c.SetReadDeadline(by)
		n, err := io.CopyN(&zipped, r.Body, min(uploadPart, left))
		if err != nil {
			return nil, err
		}
		left -= n
	}
	_ = c.SetReadDeadline(time.Time{})
	return zipped.Bytes(), nil
}

// releasedOnly is said of a web client without the release key's
// signature.
const releasedOnly = "this relay takes only a released kakel's web client"

// touchBundle reports whether the relay has the web client hash, and
// notes that it was asked for.
func (s *Server) touchBundle(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bundles[hash]
	if b == nil {
		return false
	}
	b.used = time.Now()
	return true
}

// keepBundle keeps a web client uploaded from the address key, in the
// room of those longest unused that no share has, and reports whether
// there was room. One address makes room of its own, past its part.
func (s *Server) keepBundle(b *Bundle, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old := s.bundles[b.Hash]; old != nil {
		old.used = time.Now()
		return true
	}
	// Seen to first that there is room to make, so one that is not kept
	// takes no other's.
	n, size := 0, 0
	for _, o := range s.bundles {
		if o.from == key {
			n, size = n+1, size+o.size
		}
	}
	var out []*keptBundle
	for n >= mostBundlesFrom || size+b.size > mostBundleBytesFrom {
		o := s.longestUnusedLocked(key, out)
		if o == nil {
			return false
		}
		n, size = n-1, size-o.size
		out = append(out, o)
	}
	all, used := len(s.bundles), s.bundleBytes
	for _, o := range out {
		all, used = all-1, used-o.size
	}
	for all >= mostBundles || used+b.size > mostBundleBytes {
		o := s.longestUnusedLocked("", out)
		if o == nil {
			return false
		}
		all, used = all-1, used-o.size
		out = append(out, o)
	}
	for _, o := range out {
		delete(s.bundles, o.Hash)
	}
	s.bundles[b.Hash] = &keptBundle{Bundle: b, from: key, used: time.Now()}
	s.bundleBytes = used + b.size
	return true
}

// longestUnusedLocked is the web client longest unused that no share
// has, of those from the address key, or from any for "", and none of
// those in but, or nil. s.mu is held.
func (s *Server) longestUnusedLocked(key string, but []*keptBundle) *keptBundle {
	var oldest *keptBundle
	for _, o := range s.bundles {
		if o.shares == 0 && (key == "" || o.from == key) && !slices.Contains(but, o) &&
			(oldest == nil || o.used.Before(oldest.used)) {
			oldest = o
		}
	}
	return oldest
}

// useLocked has a share use b, and let go of the one it used, nil for
// none. s.mu is held.
func (s *Server) useLocked(sh *share, b *keptBundle) {
	if old := sh.bundle; old != nil {
		old.shares--
		old.used = time.Now()
	}
	if b != nil {
		b.shares++
	}
	sh.bundle = b
}
