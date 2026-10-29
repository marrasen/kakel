package app

import (
	"net/url"
	"runtime"
	"strings"
	"time"
)

// The last crash, shown as kakel starts: what kakel said as it died, in
// a window of its own, to copy or to report on GitHub.

// reportCrash is the button that opens a GitHub issue about a crash.
const reportCrash = "Report on GitHub"

// newIssue is where a GitHub issue about kakel is written.
const newIssue = "https://github.com/marrasen/kakel/issues/new"

// mostIssueLink is how long the link to a new issue may be. GitHub turns
// away a much longer one, and some browsers cut it.
const mostIssueLink = 7000

// sayCrashed shows what the last run said as it crashed, once, as the
// one kakel starts.
func (a *app) sayCrashed() {
	crash := lastCrash
	lastCrash = ""
	if crash == "" {
		return
	}
	q := crashAsk(crash, crashPath)
	go func() { _, _ = a.ask(a.ctx, q) }()
}

// crashAsk is the question that shows a crash, the last run's part of
// the crash log at path: its trace, which kakel it was, and the
// buttons to copy it and to report it.
func crashAsk(crash, path string) Ask {
	heading, trace, _ := strings.Cut(crash, "\n")
	version, started := crashRun(heading)
	about := "kakel " + version
	if !started.IsZero() {
		about += ", started " + started.Format("15:04 on 2 January")
	}
	about += ", stopped saying this. All of it is in " + path + "."
	return Ask{
		Title: "kakel crashed", Text: tidyTrace(strings.TrimSpace(trace)), Preformatted: true,
		Small:   []string{about},
		Actions: []string{"Copy", reportCrash}, Copy: crash, Link: crashIssue(version, trace),
		Yes: "Close", Plain: true,
	}
}

// crashRun reads a run's heading: the version it was, and when it
// started, zero when the heading does not say.
func crashRun(heading string) (version string, started time.Time) {
	rest := strings.TrimPrefix(heading, crashHeading)
	version, rest, _ = strings.Cut(rest, ", started ")
	at, _, _ := strings.Cut(rest, ", process ")
	started, _ = time.Parse(time.RFC3339, at)
	return version, started
}

// crashIssue is the link to a new GitHub issue about a crash, written
// for the user to say what they were doing: the version, the system,
// and the trace, cut short to fit in a link.
func crashIssue(version, trace string) string {
	lines := strings.Split(tidyTrace(strings.TrimSpace(trace)), "\n")
	title := "Crash: " + strings.TrimPrefix(lines[0], "panic: ")
	link := func(lines []string, cut bool) string {
		body := "kakel " + version + " crashed, on " + runtime.GOOS + "/" + runtime.GOARCH + ".\n\n" +
			"**What I was doing:**\n\n\n\n" +
			"**What kakel said:**\n\n```\n" + strings.Join(lines, "\n") + "\n```\n"
		if cut {
			body += "\nCut short to fit. The rest is in kakel's crash.log.\n"
		}
		return newIssue + "?" + url.Values{"title": {title}, "body": {body}, "labels": {"bug"}}.Encode()
	}
	at := link(lines, false)
	for n := len(lines) - 1; len(at) > mostIssueLink && n > 1; n-- {
		at = link(lines[:n], true)
	}
	return at
}

// tidyTrace takes from a Go stack trace what says nothing to its
// reader: each call's arguments, as words in memory, and each line's
// offset into its function's code.
func tidyTrace(trace string) string {
	lines := strings.Split(trace, "\n")
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "\t"):
			// A file and line, then " +0x1a2".
			if at := strings.LastIndex(l, " +0x"); at >= 0 {
				lines[i] = l[:at]
			}
		case strings.HasSuffix(l, ")") && !strings.HasPrefix(l, "goroutine ") && !strings.HasPrefix(l, "panic: "):
			lines[i] = withoutArgs(l)
		}
	}
	return strings.Join(lines, "\n")
}

// withoutArgs is a call's line, as pkg.(*T).F(0x1, 0x2), with what is
// between its last parentheses taken out. A method's receiver is in
// parentheses too, as are a closure's, so the arguments are those after
// the last ")." or, with none, after the last "/".
func withoutArgs(l string) string {
	from := strings.LastIndex(l, ").")
	if from < 0 {
		from = strings.LastIndex(l, "/")
	}
	open := strings.IndexByte(l[from+1:], '(')
	if open < 0 {
		return l
	}
	return l[:from+1+open] + "(…)"
}
