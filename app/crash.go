package app

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/marrasen/kakel/conf"
)

// Crashes: what the runtime says as kakel dies, kept on disk. kakel
// logs to stderr, which a kakel started from the Start menu, the tray
// or with the computer has nowhere to send, and the Window Log dies with
// the process. A panic's stack would be lost with both.

// crashFile is the file in the private directory crashes are kept in.
const crashFile = "crash.log"

// mostCrashLog is how big the crash log may grow before it is started
// again.
const mostCrashLog = 1 << 20

// crashHeading starts the line each run of kakel writes as it starts,
// for a crash under it to say which kakel it was and when it started.
const crashHeading = "--- kakel "

// KeepCrashes has the runtime write a fatal panic or error into the
// crash log as well as to stderr, from any goroutine. Only the one
// kakel running should, as it starts: another would take its heading
// away.
func KeepCrashes() {
	dir, err := conf.Private()
	if err == nil {
		err = keepCrashesIn(filepath.Join(dir, crashFile), time.Now())
	}
	if err != nil {
		log.Printf("crashes will not be kept: %v", err)
	}
}

// keepCrashesIn points crash output at the log at path, below a heading
// for this run.
func keepCrashesIn(path string, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := forgetQuietRun(path); err != nil {
		return err
	}
	// Appending, so what the runtime writes goes at the end whatever
	// else has written to the file.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	// SetCrashOutput keeps a copy of its own.
	defer func() { _ = f.Close() }()
	if _, err := fmt.Fprintf(f, "%s%s, started %s, process %d\n", crashHeading, thisVersion(), now.Format(time.RFC3339), os.Getpid()); err != nil {
		return err
	}
	return debug.SetCrashOutput(f, debug.CrashOptions{})
}

// forgetQuietRun takes the last run's heading off the log at path when
// nothing came after it, so only the runs that crashed are left.
func forgetQuietRun(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, mostCrashLog+1))
	if err != nil {
		return err
	}
	if n := keptOfCrashLog(b); n < len(b) {
		return f.Truncate(int64(n))
	}
	return nil
}

// keptOfCrashLog is how much of a crash log to keep: all of it but a
// last heading with nothing after it, or none once it is too big.
func keptOfCrashLog(b []byte) int {
	if len(b) > mostCrashLog {
		return 0
	}
	// The start of the last heading, at the start of the log when it
	// is the only one.
	i := bytes.LastIndex(b, []byte("\n"+crashHeading)) + 1
	if !bytes.HasPrefix(b[i:], []byte(crashHeading)) {
		return len(b)
	}
	if end := bytes.IndexByte(b[i:], '\n'); end >= 0 && i+end+1 == len(b) {
		return i
	}
	return len(b)
}
