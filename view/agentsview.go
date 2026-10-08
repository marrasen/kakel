package view

import (
	"slices"
	"strings"

	"github.com/marrasen/kakel/agenthost"
	"github.com/marrasen/kakel/app"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/agent"
	"github.com/marrasen/kakel/settings"
)

// The window's side of sharing panes with an agent.

// shareDialog shows the share: its code, the terminal panes with a box
// for each that is in it, and the agent program the prompt is written
// for. Ticking a box shares that pane at once. With no share yet it
// starts one with the focused pane.
func (w *Window) shareDialog(st app.Share, u *gunim.UI) {
	if st.Code == "" {
		if w.kindOf(w.focused) != app.KindTerminal {
			w.toasts.Show(widget.Toast{Title: "Share a terminal pane", Body: "Click into the terminal to share, then choose Share with an Agent."}, u)
			return
		}
		u.Send(w, app.SharePane{Pane: w.focused})
		w.sharing = true
		return
	}
	shared := map[string]bool{}
	mays := map[string]settings.AgentMay{}
	for _, p := range st.Panes {
		shared[p.Pane] = true
		mays[p.Pane] = settings.AgentMay(p.May)
	}
	host := widget.NewDropdown(widget.Labels(agenthost.Names()...))
	host.Label = "Agent"
	host.SetSelected(max(0, slices.Index(agenthost.Names(), st.Host)), nil)
	hostName := func() string { return agenthost.Names()[max(0, min(host.Selected(), len(agenthost.All)-1))] }
	code := widget.NewLabel(st.Code)
	// Picked out and copied as it is, for an agent set up by hand.
	code.Selectable = true
	form := widget.NewForm().
		Add("", widget.NewLabel("An agent with this code can read and type in the ticked panes, and reaches nothing else. Copy Prompt puts the code on the clipboard with how to use it.")).
		Add("Code", code).
		Add("Agent", host)
	for _, p := range w.panes {
		if p.Kind != app.KindTerminal {
			continue
		}
		label := p.Title
		if p.Machine != "" {
			label += " on " + w.nameOf(p.Machine)
		}
		if shared[p.ID] {
			label += mayWords(mays[p.ID])
		}
		box := widget.NewCheckbox(label)
		box.SetChecked(shared[p.ID], nil)
		id := p.ID
		box.OnChange = func(on bool, u *gunim.UI) gunim.Intent {
			if on {
				u.Send(w, app.SharePane{Pane: id})
			} else {
				u.Send(w, app.UnsharePane{Pane: id})
			}
			return nil
		}
		form.Add("", box)
	}
	d := widget.NewDialog("Agent Share")
	d.Body = form
	d.SetButtons("Done", "")
	d.AddAction("Copy Prompt", func(u *gunim.UI) gunim.Intent { u.Send(w, app.CopyAgentPrompt{Host: hostName()}); return nil })
	d.AddAction("Copy Code", func(u *gunim.UI) gunim.Intent {
		u.SetClipboard(st.Code)
		w.toasts.Show(widget.Toast{Title: "Code copied", Kind: widget.ToastSuccess}, u)
		return nil
	})
	d.AddAction("Setup…", func(u *gunim.UI) gunim.Intent { w.setupDialog(agenthost.Named(hostName()), u); return nil })
	d.AddAction("Write Skill", func(u *gunim.UI) gunim.Intent { u.Send(w, app.WriteSkill{Host: hostName()}); return nil })
	d.AddButton("Stop Sharing", func(u *gunim.UI) gunim.Intent { return app.StopSharing{} })
	d.OnAccept, d.OnDismiss = widget.Sends(app.DialogClosed{}), widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// permissionsDialog shows what the agent may do in the focused pane
// beyond reading and typing. Each box takes effect as it is ticked.
func (w *Window) permissionsDialog(st app.Share, u *gunim.UI) {
	i := slices.IndexFunc(st.Panes, func(p app.SharedPane) bool { return p.Pane == w.focused })
	if i < 0 {
		w.toasts.Show(widget.Toast{Title: "This pane is not shared", Body: "Share it with an agent first."}, u)
		return
	}
	may := st.Panes[i].May
	id := w.focused
	form := widget.NewForm().Add("", widget.NewLabel("The agent can read this pane and type into it. Changes apply at once."))
	for _, b := range []struct {
		label string
		on    *bool
	}{
		{agent.BoxReadOnly, &may.ReadOnly},
		{agent.BoxReadBack, &may.ReadBack},
		{agent.BoxOpenMore, &may.OpenMore},
		{agent.BoxRestart, &may.Restart},
	} {
		box := widget.NewCheckbox(b.label)
		box.SetChecked(*b.on, nil)
		on := b.on
		box.OnChange = func(v bool, u *gunim.UI) gunim.Intent {
			*on = v
			u.Send(w, app.SetAgentMay{Pane: id, May: settings.AgentMay(may)})
			return nil
		}
		form.Add("", box)
	}
	d := widget.NewDialog("Agent Permissions")
	d.Body = form
	d.SetButtons("Done", "")
	d.AddButton("Take Back", func(u *gunim.UI) gunim.Intent { return app.UnsharePane{Pane: id} })
	d.OnAccept, d.OnDismiss = widget.Sends(app.DialogClosed{}), widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// setupDialog shows how to add kakel's MCP server to an agent
// program, and copies it.
func (w *Window) setupDialog(host agenthost.Host, u *gunim.UI) {
	what := "Run this, as one command line, then start " + host.Called + " again. It only writes the config."
	copyTitle := "Copy Command"
	if host.Cmd == "" {
		where := host.ConfigAt
		if where == "" {
			where = "its MCP config"
		}
		what = "Put this in " + where + ", beside any servers already there. Then start " + host.Called + " again."
		copyTitle = "Copy Config"
	}
	line := host.SetupToCopy(agenthost.ExePath())
	d := widget.NewDialog("Set Up " + host.Called)
	d.Body = widget.NewForm().Add("", widget.NewLabel(what)).Add("", widget.NewLabel(line))
	d.SetButtons("Close", "")
	d.AddAction(copyTitle, func(u *gunim.UI) gunim.Intent { u.Send(w, app.CopyAgentSetup{Host: host.Name}); return nil })
	d.OnAccept, d.OnDismiss = widget.Sends(app.DialogClosed{}), widget.Sends(app.DialogClosed{})
	w.openDialog(d, u)
}

// mayWords is what a shared pane lets the agent do beyond reading and
// typing, or nothing.
func mayWords(may settings.AgentMay) string {
	var adds []string
	if may.ReadOnly {
		adds = append(adds, "read only")
	}
	if may.Restart {
		adds = append(adds, "restart")
	}
	if may.OpenMore {
		adds = append(adds, "open panes")
	}
	if may.ReadBack {
		adds = append(adds, "read above a clear")
	}
	if len(adds) == 0 {
		return ""
	}
	return " (" + strings.Join(adds, ", ") + ")"
}
