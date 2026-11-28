//go:build !js

package relay

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// pageOf is the URL of the page of the share id on u.
func pageOf(u URL, id string) string {
	return strings.TrimSuffix(u.u.Scheme+"://"+u.Host()+u.u.Path[:strings.LastIndex(u.u.Path, secretPath)], "/") + sharePath + id + "/"
}

// get gets url, with headers in pairs.
func get(t *testing.T, url string, headers ...string) *http.Response {
	t.Helper()
	return asked(t, http.MethodGet, url, headers...)
}

// asked makes a request, with headers in pairs, and reads its answer.
func asked(t *testing.T, method, url string, headers ...string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true, DisableKeepAlives: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	return resp
}

// body is what an answer holds.
func body(t *testing.T, resp *http.Response) []byte {
	t.Helper()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAGuestIsSentTheWebClient(t *testing.T) {
	_, u, bundle := sharing(t)
	id := NewShareID()
	sharedAs(t, u, id, NewToken(), bundle)
	page := pageOf(u, id)
	files := clientFiles(t)
	for name, want := range map[string]struct {
		kind, cache string
		data        []byte
	}{
		"":                                  {"text/html; charset=utf-8", "no-store", files[indexName]},
		indexName:                           {"text/html; charset=utf-8", "no-store", files[indexName]},
		execName:                            {"text/javascript; charset=utf-8", "private, no-cache", files[execName]},
		"icon.svg":                          {"image/svg+xml", "private, no-cache", files["icon.svg"]},
		wasmName[:len(wasmName)-len(".gz")]: {"application/wasm", "private, no-cache", files[wasmName]},
	} {
		resp := get(t, page+name)
		got := body(t, resp)
		if resp.StatusCode != http.StatusOK || !bytes.Equal(got, want.data) {
			t.Errorf("%q: %s, %d bytes", name, resp.Status, len(got))
		}
		if k, c := resp.Header.Get("Content-Type"), resp.Header.Get("Cache-Control"); k != want.kind || c != want.cache {
			t.Errorf("%q: %s, %s", name, k, c)
		}
		if resp.ContentLength != int64(len(want.data)) {
			t.Errorf("%q: its length is said to be %d", name, resp.ContentLength)
		}
	}
	for _, name := range []string{wasmName, manifestName, "nothing.js", "a/b.js", "../", "%2e%2e/x", "join/"} {
		if resp := get(t, page+name); resp.StatusCode == http.StatusOK {
			t.Errorf("%q: %s", name, resp.Status)
		}
	}
}

func TestTheClientIsSentGzippedToAnyThatTakesIt(t *testing.T) {
	_, u, bundle := sharing(t)
	id := NewShareID()
	sharedAs(t, u, id, NewToken(), bundle)
	at := pageOf(u, id) + "client.wasm"
	for accepts, takes := range map[string]bool{
		"gzip, deflate, br, zstd": true, "GZIP": true, "br;q=1.0, gzip;q=0.8": true, "*": true, "br, *;q=0.1": true, "": true,
		"identity": false, "gzip;q=0": false, "gzip; q=0.0, *": false, "br": false, "*;q=0": false,
	} {
		resp := get(t, at, "Accept-Encoding", accepts)
		got := body(t, resp)
		if resp.Header.Get("Vary") != "Accept-Encoding" {
			t.Errorf("for %q: Vary %q", accepts, resp.Header.Get("Vary"))
		}
		if !takes {
			if resp.StatusCode != http.StatusNotAcceptable || resp.Header.Get("Content-Encoding") != "" || len(got) > 1000 {
				t.Errorf("for %q: %s, %d bytes", accepts, resp.Status, len(got))
			}
			continue
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Encoding") != "gzip" {
			t.Errorf("for %q: %s, Content-Encoding %q", accepts, resp.Status, resp.Header.Get("Content-Encoding"))
			continue
		}
		zr, err := gzip.NewReader(bytes.NewReader(got))
		if err != nil {
			t.Fatal(err)
		}
		if got, err = io.ReadAll(zr); err != nil || !bytes.Equal(got, wasmIn) {
			t.Errorf("for %q: another client, %v", accepts, err)
		}
	}
	// Asked about again, it is not sent again.
	tag := get(t, at).Header.Get("ETag")
	for header, status := range map[string]int{tag: 304, "W/" + tag: 304, `"x", ` + tag: 304, "*": 304, `"x"`: 200} {
		if resp := get(t, at, "If-None-Match", header); resp.StatusCode != status {
			t.Errorf("If-None-Match %s: %s", header, resp.Status)
		}
	}
}

func TestAShareIsDownloadedSoManyTimesAtOnce(t *testing.T) {
	s, u, bundle := sharing(t)
	id := NewShareID()
	sharedAs(t, u, id, NewToken(), bundle)
	sh, _ := s.shared(id)
	var done []func()
	for range mostDownloadsFrom {
		done = append(done, s.sendingTo(sh, "2001:db8::1"))
	}
	if s.sendingTo(sh, "2001:db8::2") != nil {
		t.Error("one network is sent more than it may be")
	}
	for i := 0; sh.downloads < mostDownloads; i++ {
		if d := s.sendingTo(sh, fmt.Sprintf("192.0.2.%d", i)); d != nil {
			done = append(done, d)
		} else {
			t.Fatal("another address has no room")
		}
	}
	if resp := get(t, pageOf(u, id)); resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Errorf("one more: %s", resp.Status)
	}
	for _, d := range done {
		if d == nil {
			t.Fatal("one had no room")
		}
		d()
	}
	if sh.downloads != 0 || len(sh.sending) != 0 {
		t.Errorf("%d are counted still, of %d addresses", sh.downloads, len(sh.sending))
	}
	if resp := get(t, pageOf(u, id)); resp.StatusCode != http.StatusOK {
		t.Errorf("after: %s", resp.Status)
	}
}

func TestLookingAtALinkUsesNothingUp(t *testing.T) {
	s, u, bundle := sharing(t)
	id := NewShareID()
	l, _ := sharedAs(t, u, id, NewToken(), bundle)
	knocked := make(chan struct{}, 1)
	go func() {
		if _, err := l.Accept(); err == nil {
			knocked <- struct{}{}
		}
	}()
	page := pageOf(u, id)
	for range mostMisses + 5 {
		for _, name := range []string{"", "client.wasm"} {
			resp := asked(t, http.MethodHead, page+name)
			if resp.StatusCode != http.StatusOK || len(body(t, resp)) != 0 || resp.ContentLength <= 0 {
				t.Fatalf("HEAD %q: %s, %d", name, resp.Status, resp.ContentLength)
			}
		}
		if resp := get(t, page, "User-Agent", "Slackbot-LinkExpanding 1.0"); resp.StatusCode != http.StatusOK {
			t.Fatal(resp.Status)
		}
	}
	// Its stream's door wants a stream.
	if resp := get(t, page+doorJoin); resp.StatusCode != http.StatusUpgradeRequired {
		t.Errorf("the join door, looked at: %s", resp.Status)
	}
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		if resp := asked(t, method, page); resp.StatusCode != http.StatusMethodNotAllowed || resp.Header.Get("Allow") == "" {
			t.Errorf("%s: %s", method, resp.Status)
		}
	}
	select {
	case <-knocked:
		t.Error("the host was knocked for")
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lost) != 0 || len(s.tickets) != 0 || s.shares[id].waiting != 0 {
		t.Error("something was used up")
	}
}

func TestAnExpiredLinkIsSaidToHave(t *testing.T) {
	_, u, _ := sharing(t)
	for _, id := range []string{NewShareID(), "nonsense", strings.ToUpper(NewShareID()), ""} {
		for _, at := range []string{pageOf(u, id), strings.TrimSuffix(pageOf(u, id), "/"), pageOf(u, id) + "client.wasm"} {
			resp := get(t, at)
			if resp.StatusCode != http.StatusNotFound || resp.Header.Get("Content-Type") != "text/html; charset=utf-8" ||
				!bytes.Contains(body(t, resp), []byte(`<h1>`)) {
				// A path that ends in /s is no link's.
				if id == "" && strings.HasSuffix(at, "/s") {
					continue
				}
				t.Errorf("%s: %s", at, resp.Status)
			}
		}
	}
}

func TestEveryAnswerToAGuestIsClosedUp(t *testing.T) {
	_, u, bundle := sharing(t)
	id := NewShareID()
	sharedAs(t, u, id, NewToken(), bundle)
	policy := func(resp *http.Response) map[string]string {
		out := map[string]string{}
		for part := range strings.SplitSeq(resp.Header.Get("Content-Security-Policy"), ";") {
			name, value, _ := strings.Cut(strings.TrimSpace(part), " ")
			if _, twice := out[name]; twice {
				// A browser reads the first alone.
				t.Errorf("%s: %s twice", resp.Request.URL, name)
			}
			out[name] = value
		}
		return out
	}
	for _, at := range []string{pageOf(u, id), pageOf(u, id) + "client.wasm", pageOf(u, id) + "nothing.js", pageOf(u, id) + doorJoin,
		strings.TrimSuffix(pageOf(u, id), "/"), pageOf(u, NewShareID())} {
		resp := get(t, at)
		for header, want := range map[string]string{
			"Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY",
			"Cross-Origin-Opener-Policy": "same-origin", "Strict-Transport-Security": "",
		} {
			if got := resp.Header.Get(header); got != want {
				t.Errorf("%s: %s is %q", at, header, got)
			}
		}
		if !strings.Contains(resp.Header.Get("X-Robots-Tag"), "noindex") {
			t.Errorf("%s: X-Robots-Tag is %q", at, resp.Header.Get("X-Robots-Tag"))
		}
		p := policy(resp)
		for name, want := range map[string]string{"default-src": "'none'", "frame-ancestors": "'none'", "base-uri": "'none'", "form-action": "'none'"} {
			if p[name] != want {
				t.Errorf("%s: %s is %q", at, name, p[name])
			}
		}
		for name, value := range p {
			if strings.Contains(value, "unsafe-inline") || strings.Contains(value, "'unsafe-eval'") || strings.Contains(value, "*") {
				t.Errorf("%s: %s is %q", at, name, value)
			}
		}
	}
	p := policy(get(t, pageOf(u, id)))
	if want := "'self' wss://" + u.Host() + " ws://" + u.Host(); p["connect-src"] != want {
		t.Errorf("connect-src is %q, not %q", p["connect-src"], want)
	}
	if p["script-src"] != "'self' 'wasm-unsafe-eval'" {
		t.Errorf("script-src is %q", p["script-src"])
	}
	// The save worker's download frames, and the worker itself, which
	// script-src lets in where no worker-src or child-src says otherwise.
	if p["frame-src"] != "'self'" || p["worker-src"] != "" || p["child-src"] != "" {
		t.Errorf("frame-src is %q, worker-src %q, child-src %q", p["frame-src"], p["worker-src"], p["child-src"])
	}
	// The expired page has a style and no script.
	if p := policy(get(t, pageOf(u, NewShareID()))); p["script-src"] != "" || !strings.HasPrefix(p["style-src"], "'sha256-") {
		t.Errorf("the expired page: %v", p)
	}
}

func TestAHostIsInThePolicyOnlyAsOne(t *testing.T) {
	for _, host := range []string{"relay.example.com", "relay.example.com:8443", "127.0.0.1:8080", "[::1]:8080"} {
		if !strings.HasSuffix(clientPolicy(host), "wss://"+host+" ws://"+host) {
			t.Errorf("%s is not: %s", host, clientPolicy(host))
		}
	}
	for _, host := range []string{"", "x; script-src *", "x *", "*", "x:80:90", "x/y", "x'"} {
		if !strings.HasSuffix(clientPolicy(host), "connect-src 'self'") {
			t.Errorf("%q is: %s", host, clientPolicy(host))
		}
	}
}

func TestARelayThatDoesTLSItselfSaysToKeepToIt(t *testing.T) {
	s := NewServer(NewSecret())
	for name, secure := range map[string]*tls.ConnectionState{"plain": nil, "TLS": {}} {
		r := httptest.NewRequest(http.MethodGet, "/s/"+NewShareID()+"/", nil)
		r.TLS = secure
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if got := w.Header().Get("Strict-Transport-Security") != ""; got != (secure != nil) {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestACallerSendingItsBodySlowlyIsLetGo(t *testing.T) {
	old := bodyWithin
	bodyWithin = 200 * time.Millisecond
	t.Cleanup(func() { bodyWithin = old })
	_, u, _ := sharing(t)
	c := raw(t, u, "POST /s/x/ HTTP/1.1\r\nHost: x\r\nContent-Length: 100\r\n\r\nslowly")
	if _, err := io.Copy(io.Discard, c); err != nil {
		t.Fatalf("it is held still: %v", err)
	}
}

// raw writes a request to u's relay as it is, and returns the connection.
func raw(t *testing.T, u URL, request string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", u.Host())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	if _, err := io.WriteString(c, request); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(30 * time.Second))
	return c
}

func TestAGuestMayBeSlowAndNotStopped(t *testing.T) {
	old := partWithin
	partWithin = 500 * time.Millisecond
	t.Cleanup(func() { partWithin = old })
	s, u := started(t)
	files := clientFiles(t)
	// More than a connection holds by itself.
	files["large.txt"] = make([]byte, 24<<20)
	zipped, _ := made(t, files)
	b, err := OpenBundle(zipped)
	if err != nil {
		t.Fatal(err)
	}
	s.keepBundle(b, "192.0.2.99")
	id := NewShareID()
	sharedAs(t, u, id, NewToken(), b.Hash)
	ask := "GET /s/" + id + "/large.txt HTTP/1.1\r\nHost: x\r\nConnection: close\r\n\r\n"

	// One that takes several times as long as it may stand still.
	slow := raw(t, u, ask)
	got, part := 0, make([]byte, 1<<20)
	for {
		n, err := io.ReadFull(slow, part)
		got += n
		if err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if got < len(files["large.txt"]) {
		t.Errorf("a slow guest got %d bytes", got)
	}
	// One that stands still is let go.
	still := raw(t, u, ask)
	time.Sleep(3 * time.Second)
	if n, _ := io.Copy(io.Discard, still); n >= int64(len(files["large.txt"])) {
		t.Error("one that stood still got it all")
	}
}

func TestAnUploadThatTricklesIsLetGo(t *testing.T) {
	old := uploadPartWithin
	uploadPartWithin = 300 * time.Millisecond
	t.Cleanup(func() { uploadPartWithin = old })
	s, u := started(t)
	s.AllowUnsigned = true
	c := raw(t, u, "PUT "+u.u.Path+"/bundle/"+strings.Repeat("0", 64)+" HTTP/1.1\r\nHost: x\r\nContent-Length: 20000000\r\n\r\n")
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_, _ = io.Copy(io.Discard, c)
	}()
	for end := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		_, err := c.Write([]byte("x"))
		select {
		case <-ended:
			err = io.EOF
		default:
		}
		if err != nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("it is held still")
		}
	}
	until(t, "its place is taken still", func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return len(s.uploading) == 0
	})
}

func TestAnUploadThatKeepsComingIsTaken(t *testing.T) {
	old := uploadPartWithin
	uploadPartWithin = 500 * time.Millisecond
	t.Cleanup(func() { uploadPartWithin = old })
	s, u := started(t)
	s.AllowUnsigned = true
	files := clientFiles(t)
	files["large.txt"] = make([]byte, 2<<20)
	zipped, _ := made(t, files)
	c := raw(t, u, fmt.Sprintf("PUT %s/bundle/%s HTTP/1.1\r\nHost: x\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", u.u.Path, BundleHash(zipped), len(zipped)))
	// Several times as long in all as each part of it may take.
	for rest := zipped; len(rest) > 0; time.Sleep(100 * time.Millisecond) {
		n := min(len(rest), uploadPart/2)
		if _, err := c.Write(rest[:n]); err != nil {
			t.Fatal(err)
		}
		rest = rest[n:]
	}
	if got, err := io.ReadAll(c); err != nil || !bytes.HasPrefix(got, []byte("HTTP/1.1 201 ")) {
		t.Fatalf("got %.100q, %v", got, err)
	}
}
