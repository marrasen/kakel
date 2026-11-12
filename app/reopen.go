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

// dialInPlace connects to machine for what shows how the connection
// goes itself, as a layout's waiting panes do: no pane of its log opens,
// and a failure's toast offers the log instead. A connection on its way
// already is waited on, with nothing more asked. then hears how it went.
func (a *app) dialInPlace(machine machines.ID, then func(error)) error {
	if a.machines.Get(machine).Dialing != nil {
		m := a.machines.At(machine)
		m.Waiters = append(m.Waiters, a.orAsking(false, func() error { return a.dialInPlace(machine, then) }, then))
		return nil
	}
	if target, window, ok := a.machines.Quick(machine); ok {
		if window {
			return a.reachWindow(ConnectWindow{Addr: target, ID: machine, InPlace: true}, false, then)
		}
		return a.connectThen(ConnectTo{Target: target, As: machine, InPlace: true}, then)
	}
	if _, saved := a.machines.Saved(machine); !saved {
		return errNotListed(a.machines.Name(machine))
	}
	return a.connectThen(ConnectTo{Server: machine, InPlace: true}, then)
}

// errNotListed refuses to connect to name, a server no longer on the
// list.
func errNotListed(name string) error {
	return errors.New(name + " is not in the server list any more, so there is nothing to connect to")
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
		return errNotListed(a.machines.Name(machine))
	}
	// A saved window too: connectThen connects to it as one.
	return a.connectThen(ConnectTo{Server: machine, Quiet: quiet}, then)
}
