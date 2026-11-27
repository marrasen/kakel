package remote

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/marrasen/kakel/internal/sshtest"
)

// checkedConfig connects with real known_hosts checking rather than the
// pinned key the other tests use. Everything about host key checking
// goes through here.
func checkedConfig(t *testing.T, s *sshtest.Server, knownHosts ...string) Config {
	t.Helper()
	host, port := s.Host()
	return Config{
		Host: host, Port: port, User: "tester",
		KnownHosts: knownHosts,
		Ask:        &testAsk{password: sshtest.Password},
		// A key file rather than the agent, so a test that turns the
		// dialogs off can still authenticate: with nobody to ask, a
		// password is not on offer either.
		NoAgent:    true,
		Identities: []string{sshtest.WriteKey(t)},
	}
}

// The host key must be checked. A terminal that silently trusts an
// unknown key can be man-in-the-middled without anyone noticing.
func TestDialRejectsTheWrongHostKey(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)

	cfg := testConfig(t, s)
	cfg.HostKeyCallback = ssh.FixedHostKey(other.HostKey())
	if _, err := Connect(t.Context(), cfg); err == nil {
		t.Fatal("Connect accepted a connection with the wrong host key")
	}
}

// With no known_hosts file and nobody to ask, the connection must fail
// rather than fall back to trusting anything.
func TestDialWithoutKnownHostsRefusesToConnect(t *testing.T) {
	s := sshtest.New(t)
	cfg := checkedConfig(t, s, "/nonexistent/known_hosts")
	cfg.Ask = nil

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect connected with no known_hosts to check against")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to mention known_hosts", err)
	}
}

func TestDialAcceptsAHostInKnownHosts(t *testing.T) {
	s := sshtest.New(t)
	cfg := checkedConfig(t, s, sshtest.WriteKnownHosts(t, s.KnownHostsLine()))
	cfg.Ask = nil

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect with a matching known_hosts entry: %v", err)
	}
	_ = c.Close()
}

// A host already recorded must not be asked about, or the dialog appears
// on every connection and stops being read.
func TestDialDoesNotAskAboutAKnownHost(t *testing.T) {
	s := sshtest.New(t)
	ask := newTestAsk()
	cfg := checkedConfig(t, s, sshtest.WriteKnownHosts(t, s.KnownHostsLine()))
	cfg.Ask = ask

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()
	if _, _, keys := ask.asked(); len(keys) != 0 {
		t.Fatalf("a known host was still asked about: %+v", keys)
	}
}

// An unknown host is shown to the user, and saying yes both connects and
// records the key.
func TestDialAsksAboutAnUnknownHostAndRecordsIt(t *testing.T) {
	s := sshtest.New(t)
	// Nested, so the test also covers creating the .ssh directory.
	kh := filepath.Join(t.TempDir(), "nested", "known_hosts")
	ask := newTestAsk()
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = ask

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect after the host key was accepted: %v", err)
	}
	_ = c.Close()

	_, _, keys := ask.asked()
	if len(keys) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(keys))
	}
	if got := keys[0].Fingerprint(); got != ssh.FingerprintSHA256(s.HostKey()) {
		t.Errorf("asked about %q, want the key the server presented", got)
	}

	b, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read the known_hosts that should have been written: %v", err)
	}
	if !strings.Contains(string(b), "ssh-ed25519") {
		t.Fatalf("known_hosts holds %q, want the host key", string(b))
	}

	// Recorded, so the next connection does not ask again.
	second := newTestAsk()
	cfg.Ask = second
	c2, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	_ = c2.Close()
	if _, _, keys := second.asked(); len(keys) != 0 {
		t.Fatalf("the recorded host was asked about again: %+v", keys)
	}
}

// Saying no means no. Nothing is recorded and nothing connects.
func TestDialRefusedHostKeyDoesNotConnectOrRecord(t *testing.T) {
	s := sshtest.New(t)
	kh := filepath.Join(t.TempDir(), "known_hosts")
	ask := newTestAsk()
	ask.trust = false
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = ask

	if _, err := Connect(t.Context(), cfg); err == nil {
		t.Fatal("Connect went ahead with a host key the user refused")
	}
	if _, err := os.Stat(kh); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("known_hosts was written for a refused key: %v", err)
	}
}

// A changed key with nobody to ask is refused, and the error names the risk.
func TestDialChangedHostKeyWithNobodyToAskIsRefused(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)

	kh := sshtest.WriteKnownHosts(t, s.LineFor(other.HostKey()))
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = nil

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect accepted a host whose key had changed")
	}
	if !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("error = %v, want it to say the key does not match", err)
	}
	if !strings.Contains(err.Error(), "man-in-the-middle") {
		t.Fatalf("error = %v, want it to name the risk", err)
	}
}

// A changed key is offered with the old key beside it, and a yes replaces the old entry.
func TestDialChangedHostKeyAcceptedReplacesTheOldOne(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)
	third := sshtest.New(t)
	ask := newTestAsk()

	kh := sshtest.WriteKnownHosts(t, "# kept", third.KnownHostsLine(), s.LineFor(other.HostKey()))
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = ask

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect after the new key was accepted: %v", err)
	}
	_ = c.Close()

	_, _, keys := ask.asked()
	if len(keys) != 1 {
		t.Fatalf("the user was asked %d times, want once", len(keys))
	}
	if got := keys[0].Fingerprint(); got != ssh.FingerprintSHA256(s.HostKey()) {
		t.Errorf("asked about %q, want the key the server presented", got)
	}
	if len(keys[0].Was) != 1 || ssh.FingerprintSHA256(keys[0].Was[0]) != ssh.FingerprintSHA256(other.HostKey()) {
		t.Errorf("Was = %v, want the key known_hosts recorded", keys[0].Was)
	}

	b, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	want := "# kept\n" + third.KnownHostsLine() + "\n" + s.KnownHostsLine() + "\n"
	if string(b) != want {
		t.Fatalf("known_hosts holds\n%s\nwant\n%s", b, want)
	}

	second := newTestAsk()
	cfg.Ask = second
	c2, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	_ = c2.Close()
	if _, _, keys := second.asked(); len(keys) != 0 {
		t.Fatalf("the replaced key was asked about again: %+v", keys)
	}
}

// Saying no to a changed key connects nothing and leaves known_hosts as it was.
func TestDialChangedHostKeyRefusedLeavesKnownHosts(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)
	ask := newTestAsk()
	ask.trust = false

	kh := sshtest.WriteKnownHosts(t, s.LineFor(other.HostKey()))
	before, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = ask

	if _, err := Connect(t.Context(), cfg); !errors.Is(err, ErrHostKeyRefused) {
		t.Fatalf("Connect = %v, want ErrHostKeyRefused", err)
	}
	after, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("known_hosts changed for a refused key:\n%s", after)
	}
}

// A server that no longer has a key of the recorded type is offered with the key it does have.
func TestDialChangedHostKeyOfAnotherTypeIsOffered(t *testing.T) {
	s := sshtest.New(t)
	ask := newTestAsk()

	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	old, err := ssh.NewPublicKey(&ec.PublicKey)
	if err != nil {
		t.Fatalf("public key: %v", err)
	}
	kh := sshtest.WriteKnownHosts(t, s.LineFor(old))
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = ask

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect after the new key was accepted: %v", err)
	}
	_ = c.Close()
	_, _, keys := ask.asked()
	if len(keys) != 1 || len(keys[0].Was) != 1 || keys[0].Was[0].Type() != old.Type() {
		t.Fatalf("asked %+v, want one question naming the recorded %s key", keys, old.Type())
	}
	b, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if string(b) != s.KnownHostsLine()+"\n" {
		t.Fatalf("known_hosts holds %q, want only the new key", b)
	}
}

// A line naming other hosts too keeps them, and a hashed name is recognised.
func TestDialChangedHostKeyKeepsOtherHostsOnTheLine(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)
	host, port := s.Host()
	addr := knownhosts.Normalize(net.JoinHostPort(host, strconv.Itoa(port)))
	shared := knownhosts.Line([]string{"elsewhere.example", addr}, other.HostKey())
	hashed := knownhosts.Line([]string{knownhosts.HashHostname(addr)}, other.HostKey())

	kh := sshtest.WriteKnownHosts(t, shared, hashed)
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = newTestAsk()

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()
	b, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	want := knownhosts.Line([]string{"elsewhere.example"}, other.HostKey()) + "\n" + s.KnownHostsLine() + "\n"
	if string(b) != want {
		t.Fatalf("known_hosts holds\n%s\nwant\n%s", b, want)
	}
}

// An entry that matches only through a wildcard is not edited, and the connection fails saying where it is.
func TestDialChangedHostKeyUnderAPatternIsLeftAlone(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)
	_, port := s.Host()
	line := "[*]:" + strconv.Itoa(port) + " " + string(bytes.TrimSpace(ssh.MarshalAuthorizedKey(other.HostKey())))
	kh := sshtest.WriteKnownHosts(t, line)
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = newTestAsk()

	_, err := Connect(t.Context(), cfg)
	if err == nil || !strings.Contains(err.Error(), "pattern") {
		t.Fatalf("Connect = %v, want it to say the entry is a pattern", err)
	}
	b, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if string(b) != line+"\n" {
		t.Fatalf("known_hosts changed: %q", b)
	}
}

// An unknown host and a changed key are different things and must say
// so. Reporting an unknown host as "key mismatch" trains the user to
// ignore the one message that matters.
func TestDialUnknownHostSaysSo(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)

	// A file with an entry for a different host.
	kh := sshtest.WriteKnownHosts(t, other.KnownHostsLine())
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = nil

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect accepted a host that is not in known_hosts")
	}
	if !strings.Contains(err.Error(), "not in known_hosts") {
		t.Fatalf("error = %v, want it to say the host is not in known_hosts", err)
	}
}

// OpenSSH skips a line it cannot parse. knownhosts.New rejects the whole
// file, so one truncated entry — or one written by a newer OpenSSH with
// a key type this version does not know — would otherwise disable SSH
// for every host.
func TestDialSkipsUnparseableKnownHostsLines(t *testing.T) {
	s := sshtest.New(t)

	kh := sshtest.WriteKnownHosts(t,
		"garbage-line-with-no-key",
		"somehost ssh-ed25519 not-base64!!",
		"somehost ssh-brandnew AAAA",
		"# a comment",
		s.KnownHostsLine(),
	)
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = nil

	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v; one bad line disabled the whole file", err)
	}
	_ = c.Close()
}

// A file that cannot be read is not a file with no entries. Treating it
// as empty would report a recorded host as unknown, or as one whose key
// had changed.
func TestDialUnreadableKnownHostsIsAnError(t *testing.T) {
	s := sshtest.New(t)
	// A directory opens and then fails to read, which is the closest a
	// test gets to an unreadable file on every platform.
	cfg := checkedConfig(t, s, t.TempDir())
	cfg.Ask = newTestAsk()

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect treated an unreadable known_hosts as an empty one")
	}
	if !strings.Contains(err.Error(), "known_hosts") {
		t.Fatalf("error = %v, want it to mention known_hosts", err)
	}
}

// A file of nothing but unreadable lines is not an empty file. Treating
// it as one would offer trust-on-first-use for every host the user has
// ever recorded.
func TestDialKnownHostsWithOnlyBadLinesIsAnError(t *testing.T) {
	s := sshtest.New(t)
	kh := sshtest.WriteKnownHosts(t, "garbage", "more garbage")
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = newTestAsk()

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect connected with no usable known_hosts entries")
	}
	if !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("error = %v, want it to say the lines could not be read", err)
	}
}

// A dropped line turns "this key changed, refuse" into "trust this key?"
// -- the one question that must never be asked by mistake. Drop the line
// that records a host and that host reads as unknown, so nothing is
// offered while any line is unaccounted for.
func TestDialWillNotOfferTrustWhenALineWasDropped(t *testing.T) {
	s := sshtest.New(t)
	ask := newTestAsk()
	kh := sshtest.WriteKnownHosts(t, "this line will not parse")
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = ask

	_, err := Connect(t.Context(), cfg)
	if err == nil {
		t.Fatal("Connect went ahead with known_hosts lines it could not read")
	}
	if _, _, keys := ask.asked(); len(keys) != 0 {
		t.Fatalf("the user was offered a host key while a line was unreadable: %+v", keys)
	}
	if !strings.Contains(err.Error(), "could not be read") {
		t.Fatalf("error = %v, want it to say a line could not be read", err)
	}
}

// A known_hosts with no trailing newline must not have the new entry
// glued onto its last one: that leaves two lines parsing as neither, so
// the host that was recorded reads as unknown next time.
func TestDialRecordsOnALineOfItsOwn(t *testing.T) {
	s := sshtest.New(t)
	other := sshtest.New(t)
	kh := filepath.Join(t.TempDir(), "known_hosts")
	// An entry for a different host, with no newline after it.
	if err := os.WriteFile(kh, []byte(other.KnownHostsLine()), 0o600); err != nil {
		t.Fatalf("write known_hosts: %v", err)
	}

	cfg := checkedConfig(t, s, kh)
	cfg.Ask = newTestAsk()
	c, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	_ = c.Close()

	b, err := os.ReadFile(kh)
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("known_hosts holds %d lines, want 2:\n%s", len(lines), b)
	}
	// Both must still parse, or the next connection asks again.
	second := newTestAsk()
	cfg.Ask = second
	c2, err := Connect(t.Context(), cfg)
	if err != nil {
		t.Fatalf("second Connect: %v", err)
	}
	_ = c2.Close()
	if _, _, keys := second.asked(); len(keys) != 0 {
		t.Fatalf("the recorded host was asked about again: %+v", keys)
	}
}

// A trust decision is not appended to whatever h.add happens to point at.
func TestDialWillNotRecordThroughSomethingThatIsNotAFile(t *testing.T) {
	s := sshtest.New(t)
	dir := t.TempDir()
	// A directory named where the file should be.
	kh := filepath.Join(dir, "known_hosts")
	if err := os.Mkdir(kh, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// The first path is where a new key would be recorded.
	cfg := checkedConfig(t, s, kh)
	cfg.Ask = newTestAsk()

	if _, err := Connect(t.Context(), cfg); err == nil {
		t.Fatal("Connect recorded a host key through a directory")
	}
}

// An entry written with SSH's own port in brackets names the host as plainly as one without it.
func TestWithoutHostMatchesABracketedDefaultPort(t *testing.T) {
	key := sshtest.New(t).HostKey()
	line := []byte(knownhosts.Line([]string{"other.example"}, key))
	line = append([]byte("[host.example]:22,"), line...)
	rest, ok := withoutHost(line, "host.example:22")
	if !ok {
		t.Fatal("withoutHost took [host.example]:22 for a pattern")
	}
	if want := knownhosts.Line([]string{"other.example"}, key); string(rest) != want {
		t.Fatalf("withoutHost = %q, want %q", rest, want)
	}
}

// A known_hosts file that changed after it was read is not replaced, so a line appended meanwhile is kept.
func TestReplaceFileKeepsALineAppendedMeanwhile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte("a\nb\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	read := []byte("a\n")
	if err := replaceFile(path, []byte("x\n"), 0o600, read); !errors.Is(err, errMovedUnder) {
		t.Fatalf("replaceFile = %v, want errMovedUnder", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(b) != "a\nb\n" {
		t.Fatalf("known_hosts holds %q, want it untouched", b)
	}
	left, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("the staged file was left behind: %v", left)
	}
}
