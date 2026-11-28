//go:build unix

package guest

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A pipe in the folder is no file: opening it, to read or to write,
// fails at once, the channel goes on, and the share ends as it should.
func TestAPipeInTheFolderHoldsNothingUp(t *testing.T) {
	f := newFolders(t)
	if err := syscall.Mkfifo(filepath.Join(f.share, "pipe"), 0o644); err != nil {
		t.Skipf("no pipes here: %v", err)
	}
	r, s, c := filesOn(t, f.share, FilesChange)
	done := make(chan []error, 1)
	go func() {
		var errs []error
		_, err := readAll(c, "/pipe")
		errs = append(errs, err)
		errs = append(errs, upload(c, "/pipe", "x"))
		_, err = c.ReadDir("/pipe")
		errs = append(errs, err)
		done <- errs
	}()
	select {
	case errs := <-done:
		for i, err := range errs {
			if err == nil {
				t.Errorf("step %d opened the pipe", i)
			}
		}
	case <-time.After(10 * time.Second):
		t.Fatal("opening a pipe waits")
	}
	if infos, err := c.ReadDir("/"); err != nil || len(infos) == 0 {
		t.Errorf("after the pipe, listed %v, %v", infos, err)
	}
	_ = s.Close()
	r.until("the guest is away", func() bool { return !r.host.Status().Here })
}
