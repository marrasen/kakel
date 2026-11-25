package view

import (
	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/syntax"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/app"
	"github.com/marrasen/kakel/pastecheck"
	"github.com/marrasen/kakel/shellsetup"
	"github.com/marrasen/kakel/wrongshell"
)

// checkPaste opens text pasted into t in an editor, to change or cancel
// before it reaches the pane, when it is more than one line or large.
// It reports whether it did; otherwise the paste goes straight in. The
// editor colours the text as the pane's shell reads it.
func (w *Window) checkPaste(t *term, s string, u *gunim.UI) bool {
	if !w.pasteCheck || !pastecheck.Needed(s) {
		return false
	}
	kind, running := w.shellKind(t.id), t.Shell().T.RunningAProgram()
	w.openDialog(pastecheck.Ask{
		Text:      s,
		Highlight: pasteColours(kind, running),
		Marks:     func(s string) []widget.CodeMark { return pasteSlips(s, kind, running) },
		Along:     len(t.AlongOthers()),
		Bracketed: t.Shell().T.Bracketed(),
		OnPaste:   t.Paste,
		OnClose:   widget.Sends(app.DialogClosed{}),
	}.Dialog(u), u)
	return true
}

// shellKind is the kind of shell pane id runs.
func (w *Window) shellKind(id string) shellsetup.Route {
	for _, p := range w.panes {
		if p.ID == id {
			return p.ShellKind
		}
	}
	return shellsetup.NoRoute
}

// pasteColours colours a paste as the shell it goes to reads it: bash's
// way, PowerShell's or the Command Prompt's. A paste goes in plain when
// a program the shell started is reading instead, or the shell is not
// known.
func pasteColours(kind shellsetup.Route, programRunning bool) syntax.Highlighter {
	if programRunning {
		return nil
	}
	switch kind {
	case shellsetup.Posix:
		return syntax.Bash
	case shellsetup.PowerShell:
		return syntax.PowerShell
	case shellsetup.Cmd:
		return syntax.Batch
	}
	return nil
}

// pasteSlips marks the lines of a paste written for another shell than
// kind, the shell it goes to, with how that shell writes them. A paste
// to a program the shell started, or to a shell not known, is not
// looked at.
func pasteSlips(s string, kind shellsetup.Route, programRunning bool) []widget.CodeMark {
	if programRunning {
		return nil
	}
	var marks []widget.CodeMark
	for _, slip := range wrongshell.Find(s, kind) {
		marks = append(marks, widget.CodeMark{Line: slip.Line, Col: slip.Col, End: slip.End, Message: slip.Message})
	}
	return marks
}
