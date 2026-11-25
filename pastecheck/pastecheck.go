// Package pastecheck is the question asked before a paste of more than
// one line, or of much, reaches a terminal: an editor to look at it in,
// change it, or cancel it. kakel asks it, and so does the page a guest
// of a shared window is in.
package pastecheck

import (
	"fmt"
	"strings"

	"github.com/marrasen/gunim"
	"github.com/marrasen/gunim/icon"
	"github.com/marrasen/gunim/syntax"
	"github.com/marrasen/gunim/widget"

	"github.com/marrasen/kakel/words"
)

// Bytes is how large a paste on one line may be before it opens in the
// editor too: a page of text, as Windows Terminal counts it.
const Bytes = 5 << 10

// rows is how many lines the editor shows before it scrolls, and width
// how wide the dialog is.
const (
	rows  = 16
	width = 720
)

// Lines is how many lines s holds. A line break at the end starts no
// line of its own: a line copied whole carries one.
func Lines(s string) int {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return strings.Count(s, "\n") + strings.Count(s, "\r") - strings.Count(s, "\r\n") + 1
}

// Needed reports whether a paste is asked about before it reaches the
// terminal: more than one line, or more than Bytes.
func Needed(s string) bool { return len(s) > Bytes || Lines(s) > 1 }

// Ask is one paste to ask about.
type Ask struct {
	Text string
	// Highlight colours it as the shell it goes to reads it, and Marks
	// are its lines written for another. Both may be nil.
	Highlight syntax.Highlighter
	Marks     func(text string) []widget.CodeMark
	// Along is how many other panes it goes to, and Bracketed says the
	// program takes a paste as one, with no line break an Enter.
	Along     int
	Bracketed bool
	// OnPaste pastes text, as it is now, and OnClose hears that the
	// dialog closes, either way: what they return is sent.
	OnPaste func(text string)
	OnClose func(u *gunim.UI) gunim.Intent
}

// Dialog is the question, for the window to open. Enter pastes, from the
// editor too, and Shift+Enter starts a line.
func (a Ask) Dialog(u *gunim.UI) *widget.Dialog {
	// One kind of line break, as the editor shows them: the paste turns
	// each into Enter on its way all the same.
	s := strings.ReplaceAll(a.Text, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	text := widget.NewCodeEditor()
	text.Label = "Text to paste"
	text.Highlight = a.Highlight
	text.MaxRows = rows
	text.SetText(s, nil)
	d := widget.NewDialog(title(s))
	d.Icon = icon.ClipboardPaste
	d.Width = width
	marksOf := a.Marks
	if marksOf == nil {
		marksOf = func(string) []widget.CodeMark { return nil }
	}
	// Lines written for another shell are marked, as an editor marks
	// mistakes, and counted above the text.
	marks := marksOf(s)
	text.SetMarks(marks, u)
	label := widget.NewLabel(a.says(len(marks)))
	d.Body = widget.NewForm().Add("", label).Add("", text)
	d.SetButtons("Paste", "Cancel")
	closed := func(u *gunim.UI) gunim.Intent {
		if a.OnClose == nil {
			return nil
		}
		return a.OnClose(u)
	}
	d.OnAccept = func(u *gunim.UI) gunim.Intent {
		a.OnPaste(text.Text())
		return closed(u)
	}
	text.OnCommit = func(s string, u *gunim.UI) gunim.Intent {
		d.Close(u)
		a.OnPaste(s)
		return closed(u)
	}
	// The title counts the lines as they are edited, and the marks
	// follow the edits.
	text.OnChange = func(s string, u *gunim.UI) gunim.Intent {
		d.Title = title(s)
		marks := marksOf(s)
		text.SetMarks(marks, u)
		label.Text = a.says(len(marks))
		u.Invalidate()
		return nil
	}
	d.OnDismiss = closed
	return d
}

// title is the dialog's title: how many lines, or for one line, how
// large.
func title(s string) string {
	if n := Lines(s); n > 1 {
		return fmt.Sprintf("Paste %d Lines", n)
	}
	return "Paste " + words.Size(int64(len(s)))
}

// says is what stands above the text: how many lines look written for
// another shell, the other panes it reaches, that each line break is
// Enter where the program takes the paste as typed, and how to start a
// line.
func (a Ask) says(slips int) string {
	var says []string
	switch {
	case slips == 1:
		says = append(says, "1 line looks written for another shell.")
	case slips > 1:
		says = append(says, fmt.Sprintf("%d lines look written for another shell.", slips))
	}
	if a.Along > 0 {
		says = append(says, "It also goes to "+words.Count(a.Along, "other pane")+" typing along.")
	}
	if !a.Bracketed {
		says = append(says, "Each line break acts as Enter in this pane.")
	}
	says = append(says, "Shift+Enter starts a new line.")
	return strings.Join(says, " ")
}
