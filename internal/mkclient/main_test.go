package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/marrasen/kakel/internal/clientbuild"
	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/webbundle/names"
)

// small is a client that does nothing, titled title.
func small(title string) map[string][]byte {
	return map[string][]byte{
		"index.html": []byte("<!doctype html><title>" + title + "</title>"), "wasm_exec.js": []byte("// runs it"),
		clientbuild.WasmName: []byte("\x00asm\x01\x00\x00\x00"),
	}
}

// made is small's bundle.
func made(t *testing.T, title string) *bundle {
	t.Helper()
	b, err := bundleOf(small(title))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// signer is a key, and what signs with it as gunimsign does.
func signer(t *testing.T) (ed25519.PublicKey, func([]byte) string) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return public, func(b []byte) string {
		return base64.StdEncoding.EncodeToString(ed25519.Sign(private, b)) + "\n"
	}
}

// The same client is the same bundle, and the manifest written is what a
// relay checks the signature of.
func TestABundleIsMadeToBeSigned(t *testing.T) {
	a, b := made(t, "kakel"), made(t, "kakel")
	if !bytes.Equal(a.zipped, b.zipped) {
		t.Error("the same client is two bundles")
	}
	key, sign := signer(t)
	if _, err := relay.CheckBundle(a.zipped, sign(a.manifest), key); err != nil {
		t.Errorf("its signature: %v", err)
	}
}

// A bundle written over another leaves nothing of its signature.
func TestABundleTakesTheOneBeforesPlace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "client")
	for _, title := range []string{"one", "two"} {
		b := made(t, title)
		if err := b.write(dir); err != nil {
			t.Fatal(err)
		}
		for name, want := range map[string][]byte{names.Zip: b.zipped, names.Manifest: b.manifest} {
			if got, err := os.ReadFile(filepath.Join(dir, name)); err != nil || !bytes.Equal(got, want) {
				t.Errorf("%s is %d bytes, %v", name, len(got), err)
			}
		}
		sig := filepath.Join(dir, names.Sig)
		if _, err := os.Stat(sig); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("a bundle just written has a signature: %v", err)
		}
		if err := os.WriteFile(sig, []byte("a signature"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// What is checked is the client in the programs themselves, wherever in
// them it and its signature are.
func TestTheClientInTheProgramsIsChecked(t *testing.T) {
	key, sign := signer(t)
	_, signOther := signer(t)
	one, two := made(t, "one"), made(t, "two")
	// with is a program with parts in it, and what looks like them: a
	// zip that is none, and base64 up against a signature.
	with := func(parts ...string) []byte {
		data := "\x7fELF PK\x03\x04 PK\x05\x06 " + sign([]byte("a SHA256SUMS")) + "AAAA"
		for _, p := range parts {
			data += p + "PK\x05\x06 padding"
		}
		return []byte(data)
	}
	sig := sign(one.manifest)
	cut := string(one.zipped[:len(one.zipped)-30]) + string(bytes.Repeat([]byte{0}, 30))
	for _, c := range []struct {
		name     string
		programs [][]byte
		ok       bool
	}{
		{"one client, signed", [][]byte{with(string(one.zipped), sig), with(sig, string(one.zipped))}, true},
		{"no client", [][]byte{with(), with()}, false},
		{"not signed", [][]byte{with(string(one.zipped))}, false},
		{"signed with another key", [][]byte{with(string(one.zipped), signOther(one.manifest))}, false},
		{"another client's signature", [][]byte{with(string(two.zipped), sig)}, false},
		{"one of the programs not signed", [][]byte{with(string(one.zipped), sig), with(string(one.zipped))}, false},
		{"a client in one of the programs", [][]byte{with(string(one.zipped), sig), with()}, false},
		{"a client in the last of the programs", [][]byte{with(), with(string(one.zipped), sig)}, false},
		{"two clients", [][]byte{with(string(one.zipped), sig), with(string(two.zipped), sign(two.manifest))}, false},
		{"two clients in one", [][]byte{with(string(one.zipped), sig, string(two.zipped), sign(two.manifest))}, false},
		{"a client cut short", [][]byte{with(cut, sig)}, false},
		{"no program", nil, false},
	} {
		// As a Windows release has them, a Linux one, and nothing.
		for _, as := range []string{".zip", ".tar.gz", ""} {
			dir := t.TempDir()
			var paths []string
			for i, data := range c.programs {
				path := filepath.Join(dir, string(rune('a'+i))+as)
				if err := os.WriteFile(path, archived(t, as, data), 0o600); err != nil {
					t.Fatal(err)
				}
				paths = append(paths, path)
			}
			if err := checkPrograms(paths, key); (err == nil) != c.ok {
				t.Errorf("%s, as %q: %v", c.name, as, err)
			}
		}
	}
	// And an archive with nothing in it.
	empty := filepath.Join(t.TempDir(), "empty.zip")
	var b bytes.Buffer
	if err := zip.NewWriter(&b).Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(empty, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if checkPrograms([]string{empty}, key) == nil {
		t.Error("an archive of no programs passes")
	}
}

// archived is a program in an archive of a kind.
func archived(t *testing.T, as string, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	var err error
	switch as {
	case "":
		return data
	case ".zip":
		w := zip.NewWriter(&b)
		f, cerr := w.Create("kakel.exe")
		if cerr != nil {
			t.Fatal(cerr)
		}
		_, err = f.Write(data)
		err = errors.Join(err, w.Close())
	case ".tar.gz":
		z := gzip.NewWriter(&b)
		w := tar.NewWriter(z)
		err = w.WriteHeader(&tar.Header{Name: "kakel", Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
		if err == nil {
			_, err = w.Write(data)
		}
		err = errors.Join(err, w.Close(), z.Close())
	}
	if err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
