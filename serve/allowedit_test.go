package serve

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A pasted key is one line of a public key: not two, not a private key,
// not one with options or a certificate, and not nothing.
func TestParseKeyTakesOnePublicKey(t *testing.T) {
	_, line := aKey(t, "marcus@laptop")
	key, comment, err := ParseKey("\n  " + line + "  \n\n")
	if err != nil || comment != "marcus@laptop" || key == nil {
		t.Fatalf("ParseKey = %v, %q, %v", key, comment, err)
	}
	_, other := aKey(t, "")
	for text, want := range map[string]string{
		"":                  "no key",
		line + "\n" + other: "more than one",
		"-----BEGIN OPENSSH PRIVATE KEY-----\nabc": "private key",
		`from="10.0.0.1" ` + line:                  "does not honour",
		"ssh-ed25519 notbase64":                    "cannot be read",
	} {
		if _, _, err := ParseKey(text); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseKey(%q) = %v, want %q", text, err, want)
		}
	}
}

// Allow adds a key as one line, once, to a file only its owner reads,
// and Disallow takes it off again, leaving every other line as it was.
func TestAllowAndDisallow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conf", AuthFile)
	_, first := aKey(t, "first@one")
	_, second := aKey(t, "")
	name, err := Allow(path, first)
	if err != nil || name != "first@one" {
		t.Fatalf("Allow = %q, %v", name, err)
	}
	if _, err := Allow(path, first); err == nil || !strings.Contains(err.Error(), "already, as first@one") {
		t.Fatalf("allowed twice: %v", err)
	}
	// A comment of the user's own, with no newline at the end.
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, append(raw, []byte("# my laptop")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if name, err = Allow(path, second); err != nil || !strings.HasPrefix(name, "SHA256:") {
		t.Fatalf("Allow with no comment = %q, %v", name, err)
	}
	a, err := LoadAllowed(path)
	if err != nil || a.Len() != 2 {
		t.Fatalf("after adding, the file holds %d keys, %v", a.Len(), err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("the file's mode is %v", fi.Mode().Perm())
		}
	}

	keys := a.Keys()
	if keys[0].Name != "first@one" || keys[0].Type != "ssh-ed25519" {
		t.Fatalf("the keys are %+v", keys)
	}
	gone, err := Disallow(path, keys[0].Fingerprint)
	if err != nil || gone != "first@one" {
		t.Fatalf("Disallow = %q, %v", gone, err)
	}
	raw, _ = os.ReadFile(path)
	if want := "# my laptop\n" + strings.Fields(second)[0] + " " + strings.Fields(second)[1] + "\n"; string(raw) != want {
		t.Fatalf("after taking one off, the file reads %q, want %q", raw, want)
	}
	if _, err := Disallow(path, keys[0].Fingerprint); err == nil {
		t.Fatal("took off a key that was not there")
	}
}

// A file that would not serve is not added to or rewritten.
func TestABrokenFileIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), AuthFile)
	if err := os.WriteFile(path, []byte("ssh-ed25519 cut-in-ha\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, line := aKey(t, "x")
	if _, err := Allow(path, line); err == nil {
		t.Fatal("added a key to a file that cannot be read")
	}
	if raw, _ := os.ReadFile(path); string(raw) != "ssh-ed25519 cut-in-ha\n" {
		t.Fatalf("the file became %q", raw)
	}
}

// A server takes a new list of keys from the next connection, and says
// which clients a key no longer listed let in.
func TestSetAllowedChangesWhoGetsIn(t *testing.T) {
	mine, line := aKey(t, "marcus@laptop")
	other, otherLine := aKey(t, "other@desk")
	s := serving(t, line)
	if _, err := connect(t, s, other); err == nil {
		t.Fatal("an unlisted key got in")
	}
	both, err := ParseAllowed([]byte(line+"\n"+otherLine), "the test")
	if err != nil {
		t.Fatal(err)
	}
	s.SetAllowed(both)
	client, err := connect(t, s, other)
	if err != nil {
		t.Fatalf("a key added since could not connect: %v", err)
	}
	defer client.Close()
	waitFor(t, "the client to arrive", func() bool { return len(s.Clients()) == 1 })
	c := s.Clients()[0]
	if !c.AllowedBy(both) {
		t.Fatal("the client is not allowed by the list that let it in")
	}
	justMine, _ := ParseAllowed([]byte(line), "the test")
	if c.AllowedBy(justMine) {
		t.Fatal("the client is allowed by a list without its key")
	}
	if gone := s.SetAllowed(justMine); len(gone) != 1 || gone[0] != c {
		t.Fatalf("taking its key off hung up on %v", gone)
	}
	waitFor(t, "the client to be hung up on", func() bool { return len(s.Clients()) == 0 })
	// One that finished connecting with a key taken off since is not
	// added.
	if s.add(&Client{Name: "late", key: c.key}) {
		t.Fatal("a client whose key was taken off was added")
	}
	if _, err := connect(t, s, mine); err != nil {
		t.Fatalf("the key still listed could not connect: %v", err)
	}
}
