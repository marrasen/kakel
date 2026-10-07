package app

import (
	"errors"

	"github.com/marrasen/kakel/machines"
)

// dialAgain connects once more to a server whose connection has gone,
// by its ID: a saved one, or a quick connection's address, as the same
// quick connection. then hears how it went. A window is connected to
// again by the user, and a server removed from the list is not.
func (a *app) dialAgain(machine machines.ID, then func(error)) error {
	return a.dialAgainHow(machine, false, then)
}

// dialAgainHow is dialAgain, quiet for a file manager window.
func (a *app) dialAgainHow(machine machines.ID, quiet bool, then func(error)) error {
	if target, window, ok := a.machines.Quick(machine); ok {
		if window {
			return a.reachWindow(ConnectWindow{Addr: target, ID: machine}, quiet, then)
		}
		return a.connectThen(ConnectTo{Target: target, As: machine, Quiet: quiet}, then)
	}
	if _, saved := a.machines.Saved(machine); !saved {
		return errors.New(a.machines.Name(machine) + " is not in the server list any more, so there is nothing to connect to")
	}
	// A saved window too: connectThen connects to it as one.
	return a.connectThen(ConnectTo{Server: machine, Quiet: quiet}, then)
}
