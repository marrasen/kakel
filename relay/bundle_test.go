//go:build !js

package relay

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marrasen/kakel/internal/build"
)

// wasmIn is what the test's web client is, unpacked.
var wasmIn = bytes.Repeat([]byte("wasm "), 40000)

// packed is data gzipped.
func packed(t testing.TB, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// clientFiles are the files of a web client.
func clientFiles(t testing.TB) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		indexName:  []byte("<!doctype html><title>a client</title><script src=\"wasm_exec.js\"></script>"),
		execName:   []byte("// runs it"),
		wasmName:   packed(t, wasmIn),
		"icon.svg": []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>"),
	}
}

// made is a bundle of files, and its manifest.
func made(t testing.TB, files map[string][]byte) (zipped, manifest []byte) {
	t.Helper()
	zipped, manifest, err := MakeBundle(files)
	if err != nil {
		t.Fatal(err)
	}
	return zipped, manifest
}

// entry is a file of a zip made by hand.
type entry struct {
	name   string
	data   []byte
	method uint16
	mode   fs.FileMode
}

// zipOf is a zip of entries, whatever they are.
func zipOf(t testing.TB, entries ...entry) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(e.data); err != nil && !strings.HasSuffix(e.name, "/") {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// entriesOf are files as a bundle holds them, with a manifest of them.
func entriesOf(files map[string][]byte) []entry {
	out := []entry{{name: manifestName, data: manifestOf(files)}}
	for name, data := range files {
		out = append(out, entry{name: name, data: data})
	}
	return out
}

func TestABundleIsMadeTheSameEachTime(t *testing.T) {
	one, manifest := made(t, clientFiles(t))
	two, _ := made(t, clientFiles(t))
	if !bytes.Equal(one, two) {
		t.Fatal("the same files made two zips")
	}
	b, err := OpenBundle(one)
	if err != nil {
		t.Fatal(err)
	}
	if b.Hash != BundleHash(one) || !bytes.Equal(b.Manifest, manifest) {
		t.Errorf("opened as %s with the manifest %q", b.Hash, b.Manifest)
	}
	if f := b.files["client.wasm"]; f == nil || f.kind != "application/wasm" || !f.gzipped {
		t.Errorf("the client is %+v", f)
	}
}

func TestABundleThatIsNotSoundIsTurnedAway(t *testing.T) {
	with := func(name string, data []byte) map[string][]byte {
		files := clientFiles(t)
		files[name] = data
		return files
	}
	without := func(name string) map[string][]byte {
		files := clientFiles(t)
		delete(files, name)
		return files
	}
	plus := func(files map[string][]byte, more ...entry) []byte {
		return zipOf(t, append(entriesOf(files), more...)...)
	}
	many := clientFiles(t)
	for i := range maxFiles {
		many[fmt.Sprintf("f%d.txt", i)] = []byte("x")
	}
	whole, _ := made(t, clientFiles(t))
	// The same files under a manifest of others.
	lies := entriesOf(clientFiles(t))
	lies[0].data = bytes.Replace(lies[0].data, []byte("icon.svg"), []byte("icon.png"), 1)
	// A manifest of the same files, written another way.
	crlf := entriesOf(clientFiles(t))
	crlf[0].data = bytes.ReplaceAll(crlf[0].data, []byte("\n"), []byte("\r\n"))
	otherPurpose := entriesOf(clientFiles(t))
	otherPurpose[0].data = bytes.Replace(otherPurpose[0].data, []byte(bundlePurpose), []byte("kakel web client 2"), 1)

	bad := map[string][]byte{
		"nothing":              {},
		"no zip":               bytes.Repeat([]byte("x"), 1000),
		"cut short":            whole[:len(whole)-30],
		"something after":      append(bytes.Clone(whole), "more"...),
		"something before":     append([]byte("more"), whole...),
		"no index":             zipOf(t, entriesOf(without(indexName))...),
		"no wasm_exec.js":      zipOf(t, entriesOf(without(execName))...),
		"no client":            zipOf(t, entriesOf(without(wasmName))...),
		"no manifest":          zipOf(t, entriesOf(clientFiles(t))[1:]...),
		"another's manifest":   zipOf(t, lies...),
		"a manifest with CRs":  zipOf(t, crlf...),
		"another purpose":      zipOf(t, otherPurpose...),
		"a file not listed":    plus(clientFiles(t), entry{name: "more.js", data: []byte("x")}),
		"a file twice":         plus(clientFiles(t), entry{name: execName, data: clientFiles(t)[execName]}),
		"the manifest twice":   plus(clientFiles(t), entry{name: manifestName, data: manifestOf(clientFiles(t))}),
		"a folder":             plus(clientFiles(t), entry{name: "more/"}),
		"a symlink":            plus(clientFiles(t), entry{name: "more.js", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777}),
		"too many files":       zipOf(t, entriesOf(many)...),
		"wasm not gzipped":     zipOf(t, entriesOf(with("other.wasm", []byte("x")))...),
		"a page gzipped":       zipOf(t, entriesOf(with("other.html.gz", packed(t, []byte("x"))))...),
		"no gzip":              zipOf(t, entriesOf(with(wasmName, []byte("not gzip")))...),
		"gzip cut short":       zipOf(t, entriesOf(with(wasmName, packed(t, wasmIn)[:60]))...),
		"gzip with more after": zipOf(t, entriesOf(with(wasmName, append(packed(t, wasmIn), "more"...)))...),
	}
	for _, name := range []string{"../up.js", "/root.js", "a/b.js", `a\b.js`, "A.js", ".hidden.js", "noending", "run.sh", "run.exe",
		"a b.js", "å.js", "a..js", "x\x00.js", "nul", "con.js\n", strings.Repeat("a", maxName) + ".js", "page.php", "manifest", "join"} {
		bad["a file named "+name] = zipOf(t, entriesOf(with(name, []byte("x")))...)
	}
	for what, zipped := range bad {
		if _, err := OpenBundle(zipped); err == nil {
			t.Errorf("%s: taken", what)
		}
	}
	if _, _, err := MakeBundle(with("../up.js", nil)); err == nil {
		t.Error("made one a relay would not take")
	}
}

func TestABundleThatUnpacksWithoutEndIsTurnedAway(t *testing.T) {
	files := clientFiles(t)
	// A file the zip packs, however little.
	bomb := bytes.Repeat([]byte{0}, 1<<20)
	entries := append(entriesOf(files), entry{name: "more.js", data: bomb, method: zip.Deflate})
	if _, err := OpenBundle(zipOf(t, entries...)); err == nil {
		t.Error("a packed file was taken")
	}

	// A client gzipped that is too large unpacked.
	old := maxUnpacked
	maxUnpacked = 4 << 20
	t.Cleanup(func() { maxUnpacked = old })
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	part := make([]byte, 1<<20)
	for range maxUnpacked>>20 + 1 {
		if _, err := w.Write(part); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	big := maps.Clone(files)
	big[wasmName] = b.Bytes()
	if _, err := OpenBundle(zipOf(t, entriesOf(big)...)); err == nil || !strings.Contains(err.Error(), "too large unpacked") {
		t.Errorf("a client of %d bytes that unpacks to more than %d: %v", b.Len(), maxUnpacked, err)
	}

	// A zip with a list of files longer than its end says it is, as one
	// is made to have a reader keep a great many of them.
	whole, _ := made(t, files)
	long := zipOf(t, func() (all []entry) {
		for i := range 2000 {
			all = append(all, entry{name: fmt.Sprintf("f%d.txt", i)})
		}
		return all
	}()...)
	if _, err := OpenBundle(long); err == nil {
		t.Error("a zip of 2000 files was taken")
	}
	at := len(long) - 22
	binary.LittleEndian.PutUint16(long[at+8:], 4)
	binary.LittleEndian.PutUint16(long[at+10:], 4)
	binary.LittleEndian.PutUint32(long[at+12:], 46)
	if _, err := OpenBundle(long); err == nil {
		t.Error("a zip of 2000 files that says it has 4 was taken")
	}

	// Two files made of the same bytes of the zip: each is all of it.
	same := bytes.Clone(whole)
	at = len(same) - 22
	for dir := int(binary.LittleEndian.Uint32(same[at+16:])); dir < at; {
		binary.LittleEndian.PutUint32(same[dir+20:], uint32(len(same)-100))
		binary.LittleEndian.PutUint32(same[dir+24:], uint32(len(same)-100))
		dir += 46 + int(binary.LittleEndian.Uint16(same[dir+28:])) + int(binary.LittleEndian.Uint16(same[dir+30:])) + int(binary.LittleEndian.Uint16(same[dir+32:]))
	}
	if _, err := OpenBundle(same); err == nil {
		t.Error("files made of the same bytes were taken")
	}
	if _, err := OpenBundle(make([]byte, MaxBundle+1)); err == nil {
		t.Error("a zip that is too large was taken")
	}
}

func TestABundlesSignatureIsTheReleaseKeysAndItsOwn(t *testing.T) {
	zipped, manifest := made(t, clientFiles(t))
	key, sig := signed(t, manifest)
	if _, err := CheckBundle(zipped, sig, key); err != nil {
		t.Fatal(err)
	}
	other, otherSig := signed(t, manifest)
	for what, c := range map[string]struct {
		sig string
		key ed25519.PublicKey
	}{
		"no signature":          {"", key},
		"nonsense":              {"nonsense", key},
		"another's signature":   {otherSig, key},
		"under another's key":   {sig, other},
		"under no key":          {sig, nil},
		"a signature cut short": {sig[:40], key},
	} {
		if _, err := CheckBundle(zipped, c.sig, c.key); err == nil {
			t.Errorf("%s was taken", what)
		}
	}
	// Other files than were signed.
	files := clientFiles(t)
	files[execName] = []byte("// runs another")
	changed, _ := made(t, files)
	if _, err := CheckBundle(changed, sig, key); err == nil {
		t.Error("a signature of other files was taken")
	}
}

func TestASignatureForOneUseIsOfNoOther(t *testing.T) {
	files := clientFiles(t)
	zipped, manifest := made(t, files)
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	key := priv.Public().(ed25519.PublicKey)
	sign := func(b []byte) string { return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b)) }

	// A release's checksums name the same files, as sha256sum writes
	// them, and are signed with the same key. Neither that signature nor
	// one of the zip itself is a bundle's.
	var sums strings.Builder
	for line := range strings.Lines(string(manifest)) {
		if f := strings.Fields(line); len(f) == 3 {
			fmt.Fprintf(&sums, "%s  %s\n", f[0], f[2])
		}
	}
	for what, b := range map[string][]byte{"the checksums": []byte(sums.String()), "the zip": zipped} {
		if _, err := CheckBundle(zipped, sign(b), key); err == nil {
			t.Errorf("a signature of %s was taken", what)
		}
	}
	// Nor is a zip whose manifest is the checksums, with their signature.
	entries := entriesOf(files)
	entries[0].data = []byte(sums.String())
	if _, err := CheckBundle(zipOf(t, entries...), sign(entries[0].data), key); err == nil {
		t.Error("checksums as a manifest were taken")
	}
	// And a manifest read as checksums names nothing: the updater reads
	// lines of two words, and it has none.
	for line := range strings.Lines(string(manifest)) {
		if len(strings.Fields(line)) == 2 {
			t.Errorf("a manifest's line reads as a checksum: %q", line)
		}
	}
}

func TestTheReleaseKeyIsAKey(t *testing.T) {
	if n := len(NewServer(NewSecret()).key); n != ed25519.PublicKeySize {
		t.Fatalf("build.ReleaseKey, %q, is a key of %d bytes", build.ReleaseKey, n)
	}
}

func TestAWebClientIsTakenOnce(t *testing.T) {
	s, u := started(t)
	zipped, manifest := made(t, clientFiles(t))
	key, sig := signed(t, manifest)
	s.key = key
	ctx := context.Background()
	c := Client{Relay: u}
	hash := BundleHash(zipped)
	if has, err := c.HasBundle(ctx, hash); has || err != nil {
		t.Fatalf("before: %v, %v", has, err)
	}
	// Several kakels of the same build, at once.
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if err := c.PutBundle(ctx, zipped, sig); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if has, err := c.HasBundle(ctx, hash); !has || err != nil {
		t.Fatalf("after: %v, %v", has, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bundles) != 1 || s.bundleBytes != s.bundles[hash].size {
		t.Errorf("%d web clients are kept, of %d bytes", len(s.bundles), s.bundleBytes)
	}
}

func TestAWebClientNobodyReleasedIsRefused(t *testing.T) {
	s, u := started(t)
	zipped, manifest := made(t, clientFiles(t))
	_, sig := signed(t, manifest)
	c := Client{Relay: u}
	for what, sig := range map[string]string{"without a signature": "", "with another's": sig} {
		if err := c.PutBundle(context.Background(), zipped, sig); err == nil || !strings.Contains(err.Error(), releasedOnly) {
			t.Errorf("%s: %v", what, err)
		}
	}
	if s.touchBundle(BundleHash(zipped)) {
		t.Fatal("it was kept")
	}
	s.AllowUnsigned = true
	if err := c.PutBundle(context.Background(), zipped, ""); err != nil {
		t.Fatal(err)
	}
	if about, err := c.About(context.Background()); err != nil || !about.Unsigned {
		t.Errorf("it says %+v, %v", about, err)
	}
}

func TestAnUploadIsHeldToWhatItSays(t *testing.T) {
	s, u := started(t)
	s.AllowUnsigned = true
	zipped, _ := made(t, clientFiles(t))
	put := func(hash string, body []byte, length int64) int {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, u.to(doorBundle+"/"+hash), bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.ContentLength = length
		if length < 0 {
			req.Body = http.NoBody
			req.Body = struct {
				*bytes.Reader
				nothing
			}{bytes.NewReader(body), nothing{}}
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			// The relay closed the connection rather than read on.
			return 0
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}
	n := int64(len(zipped))
	if got := put(strings.Repeat("0", 64), zipped, n); got != http.StatusBadRequest {
		t.Errorf("under another's hash: %d", got)
	}
	if got := put("nonsense", zipped, n); got != http.StatusBadRequest {
		t.Errorf("under no hash: %d", got)
	}
	if got := put(BundleHash(zipped), zipped, -1); got != http.StatusLengthRequired {
		t.Errorf("of no length: %d", got)
	}
	big := make([]byte, MaxBundle+1)
	if got := put(BundleHash(big), big, MaxBundle+1); got != http.StatusRequestEntityTooLarge && got != 0 {
		t.Errorf("too large: %d", got)
	}
	if got := put(BundleHash([]byte("x")), []byte("x"), 1); got != http.StatusUnprocessableEntity {
		t.Errorf("no zip: %d", got)
	}
	if got := put(BundleHash(zipped), zipped, n); got != http.StatusCreated {
		t.Errorf("a sound one: %d", got)
	}
}

// nothing is a Close that does nothing.
type nothing struct{}

func (nothing) Close() error { return nil }

func TestTheWebClientLongestUnusedMakesRoom(t *testing.T) {
	s := NewServer(NewSecret())
	of := func(i int) *Bundle {
		files := clientFiles(t)
		files["n.txt"] = fmt.Append(nil, i)
		zipped, _ := made(t, files)
		b, err := OpenBundle(zipped)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	// Each from an address of its own.
	from := func(i int) string { return fmt.Sprintf("192.0.2.%d", i) }
	var all []*Bundle
	for i := range mostBundles {
		b := of(i)
		all = append(all, b)
		if !s.keepBundle(b, from(i)) {
			t.Fatalf("no room for %d", i)
		}
	}
	// The first is in use, and the second asked for just now: the third
	// is the one.
	s.useLocked(&share{}, s.bundles[all[0].Hash])
	s.touchBundle(all[1].Hash)
	if !s.keepBundle(of(100), from(100)) {
		t.Fatal("no room for one more")
	}
	for i, want := range map[int]bool{0: true, 1: true, 2: false, 3: true} {
		if s.touchBundle(all[i].Hash) != want {
			t.Errorf("%d kept: %v", i, !want)
		}
	}
	// With all of them in use there is none to make room.
	for _, b := range s.bundles {
		s.useLocked(&share{}, b)
	}
	if s.keepBundle(of(101), from(101)) {
		t.Error("one in use made room")
	}
	// As much as may be kept is a limit too.
	s = NewServer(NewSecret())
	for i := range 3 {
		if !s.keepBundle(&Bundle{Hash: from(i), size: mostBundleBytesFrom}, from(i)) {
			t.Fatal("no room in an empty relay")
		}
	}
	if !s.keepBundle(of(1), from(3)) || s.bundleBytes > mostBundleBytes || s.touchBundle(from(0)) {
		t.Errorf("%d web clients, of %d bytes, are kept", len(s.bundles), s.bundleBytes)
	}
	if s.keepBundle(&Bundle{Hash: "more", size: mostBundleBytesFrom + 1}, from(4)) {
		t.Error("more than one address may have was kept")
	}
}

func TestOneAddressHasPartOfTheRoomForWebClients(t *testing.T) {
	s := NewServer(NewSecret())
	const mine, other = "192.0.2.1", "192.0.2.2"
	one := func(n int) *Bundle { return &Bundle{Hash: fmt.Sprint(n), size: 1} }
	if !s.keepBundle(one(-1), other) {
		t.Fatal("no room in an empty relay")
	}
	for i := range mostBundlesFrom {
		if !s.keepBundle(one(i), mine) {
			t.Fatalf("no room for %d", i)
		}
		s.useLocked(&share{}, s.bundles[fmt.Sprint(i)])
	}
	// All of its own are in use, and another's is not its to take.
	if s.keepBundle(one(100), mine) || !s.touchBundle("-1") {
		t.Error("one more was kept than one address may have")
	}
	if !s.keepBundle(one(101), other) {
		t.Error("another address has no room")
	}
	// One of its own that is no longer in use makes room for its next.
	s.bundles["2"].shares = 0
	if !s.keepBundle(one(102), mine) || s.touchBundle("2") || !s.touchBundle("-1") {
		t.Error("its own did not make room")
	}
	// One too large to keep takes no room from any.
	kept := len(s.bundles)
	if s.keepBundle(&Bundle{Hash: "large", size: mostBundleBytesFrom}, mine) || len(s.bundles) != kept {
		t.Error("what was not kept made room for itself")
	}
}

func TestAnUploadFromOneAddressLeavesRoomForOthers(t *testing.T) {
	s := NewServer(NewSecret())
	turn := func(key string) func() {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		return s.uploadFrom(ctx, key)
	}
	first := turn("a")
	if first == nil || turn("a") != nil {
		t.Fatal("one address uploads two at once")
	}
	for i := 1; i < mostUploads; i++ {
		if turn(fmt.Sprint(i)) == nil {
			t.Fatalf("another address has to wait: %d", i)
		}
	}
	if turn("more") != nil {
		t.Fatal("more are uploaded than may be")
	}
	// One that waits has its turn as another is done.
	got := make(chan func(), 1)
	go func() { got <- s.uploadFrom(context.Background(), "a") }()
	first()
	if (<-got) == nil {
		t.Fatal("the next had no turn")
	}
}
