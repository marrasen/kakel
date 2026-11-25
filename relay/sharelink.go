package relay

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// sharePath is the part of a share's URL before its id.
const sharePath = "/s/"

// indexName is the file of a share's page, as the page's own address
// may name it.
const indexName = "index.html"

// shareIDLen is how long a share's id is: 128 bits in base 32.
const shareIDLen = 26

// NewShareID makes an id for a share: 128 random bits, as a relay's
// secret is and with nothing of it.
func NewShareID() string { return NewSecret() }

// isShareID reports whether s is written as a share's id.
func isShareID(s string) bool {
	if len(s) != shareIDLen {
		return false
	}
	for i := range len(s) {
		if c := s[i]; (c < 'a' || c > 'z') && (c < '2' || c > '7') {
			return false
		}
	}
	return true
}

// shownID is a share's id cut short, for a log: enough to tell one from
// another, and no use to whoever reads it.
func shownID(id string) string {
	if len(id) > 6 {
		id = id[:6]
	}
	return id + "…"
}

// ShareLink is a window shared with a link: what its guests open.
type ShareLink struct {
	// page is the share's own page, with a slash at its end.
	page *url.URL
	// ID is the share's id on the relay.
	ID string
	// Secret is what follows the #: the guest's, which no browser sends
	// to the relay. It is whatever the host and the guest make of it.
	Secret string
}

// NewShareLink is the link to the share id on a relay, with secret after
// its #. It has nothing of the relay's own secret.
func NewShareLink(r URL, id, secret string) (ShareLink, error) {
	switch {
	case r.IsZero():
		return ShareLink{}, errors.New("no relay for the link")
	case !isShareID(id):
		return ShareLink{}, errors.New("not the id of a share")
	}
	u := *r.u
	u.Path = u.Path[:strings.LastIndex(u.Path, secretPath)] + sharePath + id + "/"
	return ShareLink{page: &u, ID: id, Secret: secret}, nil
}

// ParseShareLink reads a link as ShareLink.String writes it, or the
// address of a page of the share's, as a browser names the page it is
// on. One with nothing after the # is read too: its Secret is empty.
func ParseShareLink(s string) (ShareLink, error) {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ShareLink{}, errors.New("not the URL of a shared window")
	}
	at := strings.LastIndex(u.Path, sharePath)
	if at < 0 {
		return ShareLink{}, errors.New("not the URL of a shared window")
	}
	id, file, _ := strings.Cut(u.Path[at+len(sharePath):], "/")
	if !isShareID(id) || (file != "" && file != indexName) {
		return ShareLink{}, errors.New("not the URL of a shared window")
	}
	page := url.URL{Scheme: u.Scheme, Host: strings.ToLower(u.Host), Path: u.Path[:at] + sharePath + id + "/"}
	return ShareLink{page: &page, ID: id, Secret: u.Fragment}, nil
}

// IsZero reports a link never made.
func (l ShareLink) IsZero() bool { return l.page == nil }

// String is the link in full: what is sent to a guest.
func (l ShareLink) String() string {
	if l.page == nil {
		return ""
	}
	u := *l.page
	u.Fragment = l.Secret
	return u.String()
}

// Host is the relay's machine, and its port when one was given.
func (l ShareLink) Host() string {
	if l.page == nil {
		return ""
	}
	return l.page.Host
}

// Shown is what to call the link in a log: the relay's machine and the
// start of the id, and not the secret.
func (l ShareLink) Shown() string {
	return fmt.Sprintf("%s on %s", shownID(l.ID), l.Host())
}

// join is where the guest's stream comes in.
func (l ShareLink) join() string {
	u := *l.page
	u.Path += doorJoin
	return u.String()
}
