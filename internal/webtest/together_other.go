//go:build !unix

package webtest

import "os/exec"

// together and endAll do nothing where no Chrome is run.
func together(*exec.Cmd) {}

func endAll(*exec.Cmd) {}
