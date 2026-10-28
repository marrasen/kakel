package view

import (
	"slices"
	"strconv"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/relay"
	"github.com/marrasen/kakel/remote"
	"github.com/marrasen/kakel/words"
)

// serverFields are a saved server's settings as a machine's pane shows
// them: its name, type, address, relay code, port, user, the server it
// is reached through, its key, and whether its agent is forwarded.
type serverFields struct {
	form                              *widget.Form
	name, addr, port, user, key, code *widget.TextField
	kind, via                         *widget.Dropdown
	forward                           *widget.Checkbox
	// under is the name it is saved under, "" for a new one.
	under string
	// host is the server as the fields say, or why it cannot be saved.
	host func() (remote.Host, string)
	// changed, when set, runs as any field changes.
	changed func(*gunim.UI)
}

// typed reports whether anything has been typed into a new server's
// fields.
func (f *serverFields) typed() bool {
	for _, t := range []*widget.TextField{f.name, f.addr, f.port, f.user, f.key, f.code} {
		if strings.TrimSpace(t.Text()) != "" {
			return true
		}
	}
	return false
}

// newServerFields are the fields for a new server, or old to change.
func newServerFields(w *Window, old *remote.Host) *serverFields {
	f := &serverFields{}
	name, addr, port, user, key := widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField(), widget.NewTextField()
	addr.Placeholder, port.Placeholder = "host name or address", "22"
	// The code of a window served through a relay, whose address is
	// then the relay's URL.
	code := widget.NewTextField()
	code.Placeholder = "XXXX-XXXX, for a window through a relay"
	user.Placeholder, key.Placeholder = "your name here", "the usual keys in ~/.ssh"
	// Through lists the other saved servers, to reach this one through:
	// a server, or a kakel window or its relay. A server can go through
	// a kakel window too, which dials it from its machine; a window
	// cannot, as it is reached over a stream of an SSH connection.
	through := []string{"Directly"}
	ids := []string{""}
	windows := map[string]bool{}
	for _, h := range w.saved {
		if old == nil || h.ID != old.ID {
			label := h.Name
			if h.Window {
				label += " (kakel window)"
				windows[h.ID] = true
			}
			through = append(through, label)
			ids = append(ids, h.ID)
		}
	}
	via := widget.NewDropdown(widget.Labels(through...))
	via.Label = "Through"
	kind := widget.NewDropdown(widget.Labels("Server", remote.WindowKind))
	kind.Label = "Type"
	f.name, f.addr, f.port, f.user, f.key, f.code, f.kind, f.via = name, addr, port, user, key, code, kind, via
	changed := func(u *gunim.UI) {
		if f.changed != nil {
			f.changed(u)
		}
	}
	// The key files kept, so one is a pick away rather than a path to
	// remember.
	var kept *widget.Dropdown
	if len(w.keyFiles) > 0 {
		kept = widget.NewDropdown(widget.Labels(append([]string{"Choose a saved key"}, w.keyFiles...)...))
		kept.Label = "Saved keys"
		files := w.keyFiles
		kept.OnChange = func(i int, u *gunim.UI) gunim.Intent {
			if i > 0 {
				key.SetText(files[i-1], nil)
				changed(u)
			}
			return nil
		}
	}
	forward := widget.NewCheckbox("Forward this machine's SSH agent to it")
	f.forward = forward
	if old != nil {
		f.under = old.Name
		name.SetText(old.Name, nil)
		addr.SetText(old.Address, nil)
		if old.Port != 0 {
			port.SetText(strconv.Itoa(old.Port), nil)
		}
		user.SetText(old.User, nil)
		if len(old.Identities) > 0 {
			key.SetText(old.Identities[0], nil)
		}
		if old.Window {
			kind.SetSelected(1, nil)
		}
		code.SetText(old.Code, nil)
		forward.SetChecked(old.ForwardAgent, nil)
		for i, id := range ids {
			if id != "" && id == old.Via {
				via.SetSelected(i, nil)
			}
		}
	}
	f.host = func() (remote.Host, string) {
		h := remote.Host{Name: strings.TrimSpace(name.Text()), Address: strings.TrimSpace(addr.Text()), User: strings.TrimSpace(user.Text())}
		if old != nil {
			h = *old
			h.Name, h.Address, h.User = strings.TrimSpace(name.Text()), strings.TrimSpace(addr.Text()), strings.TrimSpace(user.Text())
			h.Port, h.Identities = 0, nil
		}
		if p := strings.TrimSpace(port.Text()); p != "" {
			n, err := strconv.Atoi(p)
			if err != nil || n <= 0 || n > 65535 {
				return h, "The port is a number from 1 to 65535."
			}
			h.Port = n
		}
		// The key typed first, and the keys after it the entry had,
		// which the form does not show and so keeps.
		var rest []string
		if old != nil && len(old.Identities) > 1 {
			rest = old.Identities[1:]
		}
		if k := strings.TrimSpace(key.Text()); k != "" {
			h.Identities = []string{k}
		}
		for _, r := range rest {
			if !slices.Contains(h.Identities, r) {
				h.Identities = append(h.Identities, r)
			}
		}
		h.Via = ids[max(0, min(via.Selected(), len(ids)-1))]
		h.Window = kind.Selected() == 1
		h.Code = ""
		if c := strings.TrimSpace(code.Text()); h.Window && c != "" {
			parsed, err := relay.ParseCode(c)
			if err != nil {
				return h, words.UpperFirst(err.Error()) + "."
			}
			h.Code, h.Port = parsed, 0
			if u, err := relay.ParseURL(h.Address); err == nil {
				h.Address = u.String()
			}
		}
		if old != nil {
			// Folders saved before favourites stay until they are moved.
			h.Folders = old.Folders
		}
		// A window keeps the agent tick it had, unused, so switching it
		// back to a server brings it back. Its route it takes: a window,
		// or its relay, can be reached through a saved server.
		h.ForwardAgent = forward.Checked()
		if h.Window && windows[h.Via] {
			return h, "A kakel window is reached through a server, not through another window."
		}
		if err := h.Validate(); err != nil {
			return h, words.UpperFirst(err.Error()) + "."
		}
		if why := w.savingClashes(h, old); why != "" {
			return h, why
		}
		return h, ""
	}
	// A window has no account and no session to carry an agent over:
	// those are greyed out while the type says window, rather than
	// taken and dropped.
	applies := func() {
		window := kind.Selected() == 1
		via.Disabled = len(ids) <= 1
		forward.Disabled = window
		code.Disabled = !window
		addr.Placeholder = "host name or address"
		if window {
			addr.Placeholder = "host name or address, or a relay's URL"
		}
	}
	applies()
	kind.OnChange = func(_ int, u *gunim.UI) gunim.Intent { applies(); changed(u); return nil }
	via.OnChange = func(_ int, u *gunim.UI) gunim.Intent { changed(u); return nil }
	forward.OnChange = func(_ bool, u *gunim.UI) gunim.Intent { changed(u); return nil }
	for _, t := range []*widget.TextField{name, port, user, key, code} {
		t.OnChange = func(_ string, u *gunim.UI) gunim.Intent { changed(u); return nil }
	}
	// A link pasted whole, the relay and the code, is split into the two.
	addr.OnChange = func(text string, u *gunim.UI) gunim.Intent {
		if l, err := relay.ParseLink(text); err == nil {
			addr.SetText(l.Relay.String(), u)
			code.SetText(l.Code, u)
			kind.SetSelected(1, u)
			applies()
		}
		changed(u)
		return nil
	}
	f.form = widget.NewForm().Add("Name", name).Add("Type", kind).Add("Address", addr).Add("Code", code).Add("Port", port).Add("User", user).
		Add("Through", via).Add("Key file", key)
	if kept != nil {
		f.form.Add("Saved keys", kept)
	}
	f.form.Add("", forward)
	return f
}
