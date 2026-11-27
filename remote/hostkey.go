package remote

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // known_hosts hashes host names with HMAC-SHA1
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// maxKnownHostsLine caps one line of known_hosts. Certificate entries
// can be long, so the cap is generous; a line over it is a read failure
// rather than a line to skip, because carrying on would drop every entry
// after it.
const maxKnownHostsLine = 1024 * 1024

// errUnknownHost is what the checker reports for a host with no entry,
// when there is nobody to ask about it.
var errUnknownHost = errors.New("the host is not in known_hosts")

// hostKeys checks a server's key against known_hosts and can add one the
// user confirms.
type hostKeys struct {
	// check is the known_hosts verification. A host with no entry comes
	// back as a knownhosts.KeyError with nothing wanted.
	check ssh.HostKeyCallback

	// add is the file a newly trusted key is appended to: the first path
	// given, or ~/.ssh/known_hosts.
	add string

	// files are the paths that were read, and dropped is how many of
	// their lines would not parse.
	files   []string
	dropped int

	// lines are the lines check was built from, in order, so a line number check reports maps back to its file.
	lines []knownLine

	// retried says the caller dials again asking for a key type known_hosts holds, so a changed key of another type
	// waits for that attempt before it is offered.
	retried bool

	mu      sync.Mutex
	trusted []trustedKey
}

// trustedKey is a host key the user accepted during this connection.
type trustedKey struct {
	host string
	key  []byte
}

// knownLine is a line of known_hosts that parsed, with the file and line number it came from.
type knownLine struct {
	text   []byte
	path   string
	num    int
	marker string
}

// callback returns the check to hand to x/crypto.
//
// An unknown host is offered to the user, and only an explicit yes adds
// it. A key that does not match the one on record is offered too, with
// the old key beside it, and a yes replaces the old entry.
func (h *hostKeys) callback(ctx context.Context, ask Ask) ssh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		if h.accepted(hostname, key) {
			return nil
		}
		err := h.check(hostname, remote, key)
		if err == nil {
			return nil
		}
		var ke *knownhosts.KeyError
		if !errors.As(err, &ke) {
			return err
		}
		if len(ke.Want) > 0 {
			return h.changed(ctx, ask, hostname, key, ke)
		}
		if ask == nil {
			return fmt.Errorf("%w; connect once with ssh to record its host key",
				errUnknownHost)
		}
		// A host with no entry might have one that could not be read. A
		// dropped line turns "this key changed, refuse" into "trust this
		// key?", which is the one question that must never be asked by
		// mistake, so nothing is offered while any line is unaccounted
		// for.
		if h.dropped > 0 {
			return fmt.Errorf("%w, and %d line(s) of %v could not be read, "+
				"so it may be recorded there after all; repair known_hosts first",
				errUnknownHost, h.dropped, h.files)
		}

		ok, askErr := ask.TrustHostKey(ctx, HostKey{Addr: hostname, Key: key})
		if askErr != nil {
			return askErr
		}
		if !ok {
			return keyRefused(hostname)
		}
		if err := h.record(hostname, key); err != nil {
			return err
		}
		h.accept(hostname, key)
		return nil
	}
}

// changedKey is a changed host key of a type known_hosts holds nothing for, which dial offers once asking for a
// recorded type has failed too.
type changedKey struct {
	*knownhosts.KeyError
	offer func() error
}

func (c *changedKey) Unwrap() error { return c.KeyError }

// changed handles a key that does not match the one known_hosts records for the host.
func (h *hostKeys) changed(ctx context.Context, ask Ask, hostname string, key ssh.PublicKey,
	ke *knownhosts.KeyError) error {

	was := h.recorded(ke.Want)
	if ask == nil || len(was) == 0 {
		return ke
	}
	offer := func() error { return h.offerChanged(ctx, ask, hostname, key, was) }
	if h.retried && !holdsType(was, key.Type()) {
		return &changedKey{KeyError: ke, offer: offer}
	}
	return offer()
}

// offerChanged asks whether to accept a changed host key, and on a yes replaces what known_hosts records for the host.
func (h *hostKeys) offerChanged(ctx context.Context, ask Ask, hostname string, key ssh.PublicKey,
	was []knownLine) error {

	old := make([]ssh.PublicKey, 0, len(was))
	for _, l := range was {
		_, _, k, _, _, err := ssh.ParseKnownHosts(l.text)
		if err != nil {
			return fmt.Errorf("remote: read known_hosts %s line %d: %w", l.path, l.num, err)
		}
		old = append(old, k)
	}
	ok, err := ask.TrustHostKey(ctx, HostKey{Addr: hostname, Key: key, Was: old})
	if err != nil {
		return err
	}
	if !ok {
		return keyRefused(hostname)
	}
	// Refuse a pattern entry before anything is written, and record the new key before the old one goes
	for _, l := range was {
		if _, ok := withoutHost(l.text, hostname); !ok {
			return fmt.Errorf("remote: replace the host key in %s: %w", l.path, underPattern(l.num, hostname))
		}
	}
	if err := h.record(hostname, key); err != nil {
		return err
	}
	if err := h.forget(hostname, was); err != nil {
		return err
	}
	h.accept(hostname, key)
	return nil
}

// underPattern says line num matches hostname only through a pattern, which is left for the user to edit.
func underPattern(num int, hostname string) error {
	return fmt.Errorf("line %d matches %s through a pattern; remove the old key there by hand", num, hostname)
}

// recorded returns the plain key lines behind the entries check reported, leaving out certificate authorities.
func (h *hostKeys) recorded(want []knownhosts.KnownKey) []knownLine {
	var out []knownLine
	for _, w := range want {
		if w.Line < 1 || w.Line > len(h.lines) {
			continue
		}
		if l := h.lines[w.Line-1]; l.marker == "" {
			out = append(out, l)
		}
	}
	return out
}

// holdsType reports whether any of the lines records a key of type t.
func holdsType(lines []knownLine, t string) bool {
	for _, l := range lines {
		if _, _, k, _, _, err := ssh.ParseKnownHosts(l.text); err == nil && k.Type() == t {
			return true
		}
	}
	return false
}

// accept remembers a key the user accepted, so later key exchanges and attempts on the same connection pass.
func (h *hostKeys) accept(hostname string, key ssh.PublicKey) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.trusted = append(h.trusted, trustedKey{host: hostname, key: key.Marshal()})
}

// accepted reports whether the user accepted key for hostname during this connection.
func (h *hostKeys) accepted(hostname string, key ssh.PublicKey) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	b := key.Marshal()
	for _, t := range h.trusted {
		if t.host == hostname && bytes.Equal(t.key, b) {
			return true
		}
	}
	return false
}

// forget removes hostname from the known_hosts lines in was, and drops a line that names no other host.
func (h *hostKeys) forget(hostname string, was []knownLine) error {
	byFile := map[string][]knownLine{}
	var order []string
	for _, l := range was {
		if _, ok := byFile[l.path]; !ok {
			order = append(order, l.path)
		}
		byFile[l.path] = append(byFile[l.path], l)
	}
	for _, path := range order {
		if err := forgetIn(path, hostname, byFile[path]); err != nil {
			return fmt.Errorf("remote: replace the host key in %s: %w", path, err)
		}
	}
	return nil
}

// knownHostsWrite serialises this process's writes to known_hosts files.
var knownHostsWrite sync.Mutex

// errMovedUnder says a known_hosts file changed between reading it and replacing it.
var errMovedUnder = errors.New("known_hosts changed while it was being rewritten")

// forgetIn rewrites one known_hosts file without hostname on the given lines, starting over when the file changes
// underneath it.
func forgetIn(path, hostname string, lines []knownLine) error {
	knownHostsWrite.Lock()
	defer knownHostsWrite.Unlock()
	for range 3 {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("not a regular file")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out, err := withoutLines(b, hostname, lines)
		if err != nil {
			return err
		}
		if err := replaceFile(path, out, info.Mode().Perm(), b); !errors.Is(err, errMovedUnder) {
			return err
		}
	}
	return fmt.Errorf("%w; try again", errMovedUnder)
}

// withoutLines returns the contents b of a known_hosts file without hostname on the given lines.
func withoutLines(b []byte, hostname string, lines []knownLine) ([]byte, error) {
	segs := bytes.Split(b, []byte("\n"))
	drop := map[int]bool{}
	for _, l := range lines {
		i := l.num - 1
		if i >= len(segs) || !bytes.Equal(bytes.TrimSpace(segs[i]), l.text) {
			return nil, fmt.Errorf("line %d changed while connecting; try again", l.num)
		}
		rest, ok := withoutHost(l.text, hostname)
		if !ok {
			return nil, underPattern(l.num, hostname)
		}
		if rest == nil {
			drop[i] = true
			continue
		}
		if bytes.HasSuffix(segs[i], []byte("\r")) {
			rest = append(rest, '\r')
		}
		segs[i] = rest
	}
	var out bytes.Buffer
	for i, seg := range segs {
		if drop[i] {
			continue
		}
		out.Write(seg)
		if i < len(segs)-1 {
			out.WriteByte('\n')
		}
	}
	return out.Bytes(), nil
}

// withoutHost returns a known_hosts line with hostname taken out of its host list, nil when no host is left, and false
// when no entry names hostname exactly.
func withoutHost(line []byte, hostname string) ([]byte, bool) {
	text := string(line)
	cut := strings.IndexAny(text, " \t")
	if cut < 0 {
		return nil, false
	}
	want := knownhosts.Normalize(hostname)
	var kept []string
	found := false
	for _, p := range strings.Split(text[:cut], ",") {
		if namesHost(p, want) {
			found = true
			continue
		}
		kept = append(kept, p)
	}
	if !found {
		return nil, false
	}
	if len(kept) == 0 {
		return nil, true
	}
	return []byte(strings.Join(kept, ",") + text[cut:]), true
}

// namesHost reports whether one known_hosts host entry, plain or hashed, is exactly the normalized host want.
func namesHost(entry, want string) bool {
	if !strings.HasPrefix(entry, "|") {
		return strings.EqualFold(knownhosts.Normalize(entry), want)
	}
	parts := strings.Split(entry, "|")
	if len(parts) != 4 || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	hash, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(want))
	return hmac.Equal(mac.Sum(nil), hash)
}

// replaceFile writes data to path through a file beside it, so a failed write leaves the old file whole, and returns
// errMovedUnder instead when path no longer holds was.
func replaceFile(path string, data []byte, perm fs.FileMode, was []byte) (err error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".known_hosts-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if rmErr := os.Remove(tmp.Name()); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
				err = errors.Join(err, rmErr)
			}
		}
	}()
	if _, err = tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	now, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(now, was) {
		err = errMovedUnder
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// record appends a newly trusted key to known_hosts.
//
// A failure to write fails the connection. Connecting anyway would mean
// answering the same question on every connection, and a user who is
// asked the same question often enough stops reading it.
func (h *hostKeys) record(hostname string, key ssh.PublicKey) error {
	if h.add == "" {
		return errors.New("remote: nowhere to record the host key")
	}
	knownHostsWrite.Lock()
	defer knownHostsWrite.Unlock()
	if err := os.MkdirAll(filepath.Dir(h.add), 0o700); err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	// Only ever a plain file. Appending a trust decision to a symlink or
	// a device is not something to do by accident.
	if info, err := os.Lstat(h.add); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("remote: record the host key: %s is not a regular file", h.add)
	}
	// A file that does not end in a newline would otherwise have this
	// entry glued onto its last one, leaving two lines that parse as
	// neither — and a host that was recorded reading as unknown.
	unterminated, err := lacksFinalNewline(h.add)
	if err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	f, err := os.OpenFile(h.add, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	line := knownhosts.Line([]string{hostname}, key) + "\n"
	if unterminated {
		line = "\n" + line
	}
	if _, err := f.WriteString(line); err != nil {
		_ = f.Close()
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("remote: record the host key: %w", err)
	}
	return nil
}

// lacksFinalNewline reports whether a file is there, has something in it
// and does not end in a newline.
func lacksFinalNewline(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Size() == 0 {
		return false, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	// Opened to read, so a close that fails has lost nothing.
	defer func() { _ = f.Close() }()
	var last [1]byte
	if _, err := f.ReadAt(last[:], info.Size()-1); err != nil {
		return false, err
	}
	return last[0] != '\n', nil
}

// loadKnownHosts reads the known_hosts files and returns the check built
// from them.
//
// A file that is not there is not an error: every host is simply unknown
// until one is recorded. A file that exists and cannot be read is an
// error, because a host that is in it would otherwise read as unknown --
// or, worse, as a host whose key had changed.
// saying is told what could not be cleared up afterwards, and may be
// nil.
func loadKnownHosts(paths []string, saying func(string)) (*hostKeys, error) {
	if len(paths) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("remote: no home directory for known_hosts: %w", err)
		}
		paths = []string{filepath.Join(home, ".ssh", "known_hosts")}
	}

	// Keep only lines this library can parse. OpenSSH skips a bad line;
	// knownhosts.New rejects the whole file, so one truncated entry — or
	// one written by a newer OpenSSH with a key type this version does
	// not know — would otherwise disable SSH for every host.
	good, dropped, err := usableKnownHostLines(paths)
	if err != nil {
		return nil, err
	}
	check, err := checkerFor(good, paths, saying)
	if err != nil {
		return nil, err
	}
	return &hostKeys{check: check, add: paths[0], files: paths, dropped: dropped, lines: good}, nil
}

// checkerFor builds the verification from the lines that parsed. With no
// lines at all, every host is unknown.
func checkerFor(lines []knownLine, paths []string, saying func(string)) (cb ssh.HostKeyCallback, err error) {
	if len(lines) == 0 {
		return func(hostname string, _ net.Addr, _ ssh.PublicKey) error {
			return &knownhosts.KeyError{}
		}, nil
	}

	tmp, err := os.CreateTemp("", "kakel-known-hosts-*")
	if err != nil {
		return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
	}
	// The staged copy holds the user's host keys, so one left behind in
	// the system temp directory is said out loud -- but not failed over:
	// the lines were already read, so the check is sound and only the
	// copy leaks.
	defer func() {
		if rmErr := os.Remove(tmp.Name()); rmErr != nil && saying != nil {
			saying("could not clear up " + tmp.Name() + ": " + rmErr.Error())
		}
	}()
	for _, l := range lines {
		if _, err := tmp.Write(append(append([]byte(nil), l.text...), '\n')); err != nil {
			_ = tmp.Close()
			return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
		}
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("remote: stage known_hosts: %w", err)
	}

	cb, err = knownhosts.New(tmp.Name())
	if err != nil {
		// The staged copy is about to be removed, so the path in the
		// wrapped error names a file the user cannot go and look at.
		return nil, fmt.Errorf("remote: read known_hosts %v: %w", paths, err)
	}
	return cb, nil
}

// usableKnownHostLines returns the lines of the given files that parse.
//
// A file that is not there is skipped, because the default list names
// one path and a machine that has never used ssh does not have it. Any
// other failure to read one stops the lot: a truncated list would say a
// host is unknown, or say its key had changed, when the truth is that a
// file could not be read.
func usableKnownHostLines(paths []string) (lines []knownLine, dropped int, err error) {
	for _, p := range paths {
		f, err := os.Open(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, 0, fmt.Errorf("remote: read known_hosts: %w", err)
		}
		good, bad, err := parseKnownHosts(f, p)
		// A close that fails after a read is the one hint that the file
		// was truncated underneath it, and a short list of known hosts
		// says a host is unknown when it is not.
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return nil, 0, fmt.Errorf("remote: read known_hosts %s: %w", p, err)
		}
		lines = append(lines, good...)
		dropped += bad
	}
	return lines, dropped, nil
}

// parseKnownHosts reads one known_hosts file, keeping the lines that
// parse, counting the ones that do not, and reporting a failure to read
// it.
func parseKnownHosts(f *os.File, path string) (lines []knownLine, dropped int, err error) {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxKnownHostsLine)
	num := 0
	for sc.Scan() {
		num++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		marker, _, _, _, _, err := ssh.ParseKnownHosts(line)
		if err != nil {
			dropped++
			continue
		}
		lines = append(lines, knownLine{text: append([]byte(nil), line...), path: path, num: num, marker: marker})
	}
	if err := sc.Err(); err != nil {
		return nil, 0, err
	}
	return lines, dropped, nil
}

// hostKeyCheck builds the check a connection uses: the known_hosts
// files given, and a question for a host that is not in any of them.
//
// One kakel reaching another takes its own files, because a window
// is a machine to be checked like any other but does not belong in the
// user's ~/.ssh/known_hosts. The rule is the same either way and must
// not be written twice: an unknown host or a changed key is offered to
// the user, and only an explicit yes records it.
func hostKeyCheck(ctx context.Context, paths []string, ask Ask) (ssh.HostKeyCallback, error) {
	if len(paths) == 0 {
		return nil, errors.New("remote: no known hosts file to check against")
	}
	keys, err := loadKnownHosts(paths, nil)
	if err != nil {
		return nil, err
	}
	return keys.callback(ctx, ask), nil
}

// ErrHostKeyRefused is what a connection fails with when the user said
// no to trusting a machine's host key.
var ErrHostKeyRefused = errors.New("the host key was not accepted")

// keyRefused says the host key of a machine was not accepted, and is
// ErrHostKeyRefused.
type keyRefused string

func (k keyRefused) Error() string        { return "the host key for " + string(k) + " was not accepted" }
func (k keyRefused) Is(target error) bool { return target == ErrHostKeyRefused }
