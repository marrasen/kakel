package guest

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"golang.org/x/crypto/ssh"
)

// What the two ends say to each other.
//
// Two kinds of channel, both opened by the guest, and nothing else: one
// control channel, and a channel for each pane it is shown. On both, each
// way, go frames: a byte that says what it is, the length of its body in
// four bytes, and the body. Every kind has a most, which is checked
// before anything is made to hold it.
const (
	chanControl = "control@kakel"

	// chanPane's payload is openPane.
	chanPane = "pane@kakel"

	// reqAlive is the request both ends send now and then, so that each
	// hears the other is there. The no it is answered with is enough.
	reqAlive = "alive@kakel"

	// reqTook, on a pane's channel, is the guest saying how much of what
	// came on it it has taken, a took. It wants no answer.
	reqTook = "took@kakel"
)

// took counts the bytes of frames, whole, that the guest has drawn since
// it last said.
type took struct{ Bytes uint32 }

// tookEvery is how much the guest takes before it says so.
const tookEvery = 16 << 10

// openPane names the pane a guest asks for, as the state gave it.
type openPane struct{ ID string }

// The frames on the control channel. From the host:
const (
	// frameJoin says the link is good and waits for the guest to join.
	frameJoin = 'J'

	// frameWaiting says the host is being asked.
	frameWaiting = 'W'

	// frameState is the window as it stands, a State, and says the guest
	// is in. Only the latest is sent.
	frameState = 'S'

	// frameEnded is the last thing sent, an ended.
	frameEnded = 'E'
)

// And from the guest:
const (
	// frameName joins, with a joining.
	frameName = 'N'

	// frameFocus says which pane the guest is in, by its ID.
	frameFocus = 'F'

	// frameLeave says the guest goes, and will not be back.
	frameLeave = 'L'
)

// The frames on a pane's channel. From the host:
const (
	// frameScreen is the columns and the rows of the pane, two bytes each,
	// and the whole screen at that size. Everything before it is past.
	frameScreen = 'S'

	// frameData is what the program wrote.
	frameData = 'D'

	// frameProgram is one byte: 0 once the program has ended, and 1 while
	// one runs. Four more count those started in another's place since the
	// channel was opened.
	frameProgram = 'P'
)

// frameTyped, from the guest, is what it typed, and all it can say there.
// It begins with the count of frameProgram as the guest had it, in four
// bytes: what was typed for a program that ended is not for the next.
const frameTyped = 'T'

// programLen and countLen are what a frameProgram is, and what a
// frameTyped begins with.
const (
	countLen   = 4
	programLen = 1 + countLen
)

// programFrame says whether the program runs, and which it is.
func programFrame(runs bool, count uint32) []byte {
	b := []byte{0}
	if runs {
		b[0] = 1
	}
	return frame(frameProgram, binary.BigEndian.AppendUint32(b, count))
}

// typedFrame is what was typed for program count.
func typedFrame(count uint32, text []byte) []byte {
	return frame(frameTyped, binary.BigEndian.AppendUint32(nil, count), text)
}

// splitTyped reads the body of a typedFrame. One too short says nothing.
func splitTyped(body []byte) (count uint32, text []byte) {
	if len(body) < countLen {
		return 0, nil
	}
	return binary.BigEndian.Uint32(body), body[countLen:]
}

// The most of each.
const (
	mostJoining = 2 << 10
	mostID      = 128
	mostTyped   = 64 << 10
	mostState   = 1 << 20
	mostEnded   = 1 << 10
	mostScreen  = 16 << 20
	mostData    = 32 << 10

	// mostPanes is how many panes a state lists, and mostCols and mostRows
	// the largest screen one has: more than the largest there are, and
	// what a page can hold.
	mostPanes          = 256
	mostCols, mostRows = 1000, 500

	// mostName, mostAbout and mostTitle count characters.
	mostName, mostAbout, mostTitle = 40, 100, 300
)

// headerLen is the length of what comes before a frame's body.
const headerLen = 5

// mosts is the most of each kind of frame a reader takes.
type mosts map[byte]int

var (
	controlToGuest = mosts{frameJoin: 0, frameWaiting: 0, frameState: mostState, frameEnded: mostEnded}
	controlToHost  = mosts{frameName: mostJoining, frameFocus: mostID, frameLeave: 0}
	paneToGuest    = mosts{frameScreen: mostScreen + 4, frameData: mostData, frameProgram: programLen}
	paneToHost     = mosts{frameTyped: countLen + mostTyped}
)

// errFrame is a far end that says something it has no business saying.
var errFrame = errors.New("the other end said something this kakel does not know")

// readHeader reads the start of a frame: its kind, and how long its body
// is.
func readHeader(r io.Reader, of mosts) (kind byte, n int, err error) {
	var head [headerLen]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return 0, 0, err
	}
	most, known := of[head[0]]
	size := binary.BigEndian.Uint32(head[1:])
	if !known || uint64(size) > uint64(most) {
		return 0, 0, fmt.Errorf("%w: %q of %d bytes", errFrame, head[0], size)
	}
	return head[0], int(size), nil
}

// readFrame reads a whole frame, its body into buf, which it grows as the
// body comes rather than by what the frame says it is.
func readFrame(r io.Reader, of mosts, buf []byte) (kind byte, body []byte, err error) {
	kind, n, err := readHeader(r, of)
	if err != nil {
		return 0, buf[:0], err
	}
	body = buf[:0]
	for len(body) < n {
		next := min(n-len(body), 64<<10)
		have := len(body)
		body = append(body, make([]byte, next)...)
		if _, err := io.ReadFull(r, body[have:]); err != nil {
			return 0, body[:0], err
		}
	}
	return kind, body, nil
}

// frame is a frame, whole.
func frame(kind byte, parts ...[]byte) []byte {
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	b := make([]byte, headerLen, headerLen+n)
	b[0] = kind
	binary.BigEndian.PutUint32(b[1:], uint32(n))
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// joining is what a guest joins with.
type joining struct {
	Name  string
	About string
	// Key is the public half of its own key.
	Key []byte
}

// ended says why a share, or this guest's part in it, is over.
type ended struct{ Reason Reason }

// Pane is one pane of the shared window, as its guest is told of it.
type Pane struct {
	// ID names it, for as long as it stands in the window.
	ID    string
	Title string
	Kind  Kind
	// Icon names the picture it has in the host's window.
	Icon string `json:",omitempty"`
	// Cols and Rows are a terminal's size.
	Cols, Rows int
	// Ended says its program has ended.
	Ended bool `json:",omitempty"`
}

// Kind is what a pane is.
type Kind string

const (
	// KindTerminal is a pane the guest may be shown.
	KindTerminal Kind = "terminal"
	// KindOther is one that is not shared, which stands in the layout and
	// says so.
	KindOther Kind = "other"
)

// State is the shared window as it stands.
type State struct {
	// HostName is who shares it, and Title what the window says it is.
	HostName, Title string

	// CanType says the guest may type. What it types while it may not is
	// dropped by the host, whatever the guest's own program thinks.
	CanType bool

	// Layout and Theme are the caller's own.
	Layout, Theme json.RawMessage `json:",omitempty"`

	Panes []Pane

	// HostIn is the pane the host is in.
	HostIn string `json:",omitempty"`
}

// encodeState is st as a frame.
func encodeState(st State) ([]byte, error) {
	body, err := json.Marshal(st)
	if err != nil {
		return nil, fmt.Errorf("the shared window's state: %w", err)
	}
	if len(body) > mostState {
		return nil, fmt.Errorf("the shared window's state is %d bytes, over the %d it may be", len(body), mostState)
	}
	return frame(frameState, body), nil
}

// least is st with nothing in it that can keep it from being sent: no
// layout and no theme, and no more of the rest than a guest keeps.
func (st State) least() State {
	few := State{
		HostName: plain(st.HostName, mostName), Title: plain(st.Title, mostName),
		CanType: st.CanType, Panes: []Pane{},
	}
	if len(st.HostIn) <= mostID {
		few.HostIn = st.HostIn
	}
	for _, p := range st.Panes {
		if len(few.Panes) == mostPanes {
			break
		}
		if p.ID != "" && len(p.ID) <= mostID {
			few.Panes = append(few.Panes, Pane{
				ID: p.ID, Title: plain(p.Title, mostName), Kind: p.Kind, Icon: plain(p.Icon, mostName),
				Cols: p.Cols, Rows: p.Rows, Ended: p.Ended,
			})
		}
	}
	return few
}

// decodeState reads a state, and keeps no more of it than it may hold.
func decodeState(body []byte) (State, error) {
	var st State
	if err := json.Unmarshal(body, &st); err != nil {
		return State{}, fmt.Errorf("%w: a state: %w", errFrame, err)
	}
	st.HostName, st.Title = plain(st.HostName, mostTitle), plain(st.Title, mostTitle)
	if len(st.HostIn) > mostID {
		st.HostIn = ""
	}
	panes := st.Panes[:0]
	for _, p := range st.Panes {
		if len(panes) == mostPanes || p.ID == "" || len(p.ID) > mostID {
			continue
		}
		p.Title, p.Icon = plain(p.Title, mostTitle), plain(p.Icon, mostName)
		if p.Kind != KindTerminal {
			p.Kind = KindOther
		}
		if !goodSize(p.Cols, p.Rows) {
			p.Cols, p.Rows = 0, 0
		}
		panes = append(panes, p)
	}
	st.Panes = panes
	return st, nil
}

// goodSize reports whether a screen of cols by rows is one to hold.
func goodSize(cols, rows int) bool {
	return cols > 0 && rows > 0 && cols <= mostCols && rows <= mostRows
}

// decodeJoining reads what a guest joins with, its words made plain.
func decodeJoining(body []byte) (joining, ssh.PublicKey, error) {
	var j joining
	if err := json.Unmarshal(body, &j); err != nil {
		return joining{}, nil, fmt.Errorf("%w: a join: %w", errFrame, err)
	}
	j.Name, j.About = plainName(j.Name), plain(j.About, mostAbout)
	if j.Name == "" {
		return joining{}, nil, fmt.Errorf("%w: a join with no name", errFrame)
	}
	key, err := ssh.ParsePublicKey(j.Key)
	if err != nil || key.Type() != ssh.KeyAlgoED25519 {
		return joining{}, nil, fmt.Errorf("%w: a join with no key", errFrame)
	}
	return j, key, nil
}

// Phase is how far a share has come.
type Phase uint8

const (
	// PhaseJoin is a link nobody has joined with yet.
	PhaseJoin Phase = iota
	// PhaseWaiting is a guest the host is being asked about.
	PhaseWaiting
	// PhaseIn is a guest the host let in, here or away.
	PhaseIn
	// PhaseOver is the end of it.
	PhaseOver
)

// Reason is why a share is over, or one connection to it.
type Reason string

const (
	// ReasonStopped is the host's Stop Sharing.
	ReasonStopped Reason = "stopped"
	// ReasonRefused is a host who said no.
	ReasonRefused Reason = "refused"
	// ReasonExpired is a link nobody joined with in time.
	ReasonExpired Reason = "expired"
	// ReasonUnanswered is a host who was asked and did not answer.
	ReasonUnanswered Reason = "unanswered"
	// ReasonAway is a guest who was gone too long.
	ReasonAway Reason = "away"
	// ReasonLeft is a guest who left.
	ReasonLeft Reason = "left"
	// ReasonWindowClosed is the window closed, and ReasonQuitting the
	// host's kakel.
	ReasonWindowClosed Reason = "window-closed"
	ReasonQuitting     Reason = "quitting"
	// ReasonUsed is a link somebody has joined with, told to whoever comes
	// with it next. The share goes on.
	ReasonUsed Reason = "used"
	// ReasonReplaced is a guest who opened the window somewhere else, told
	// to where it was. The share goes on.
	ReasonReplaced Reason = "replaced"
)

// EndedError says the host ended it, and why.
type EndedError struct{ Reason Reason }

func (e *EndedError) Error() string { return "the share is over: " + string(e.Reason) }

// Is makes errors.Is compare the reason.
func (e *EndedError) Is(target error) bool {
	other, ok := target.(*EndedError)
	return ok && other.Reason == e.Reason
}

// What Dial, Join and Session.Err say, besides an EndedError of another
// reason, for errors.Is.
var (
	ErrUsed     error = &EndedError{ReasonUsed}
	ErrExpired  error = &EndedError{ReasonExpired}
	ErrRefused  error = &EndedError{ReasonRefused}
	ErrReplaced error = &EndedError{ReasonReplaced}

	// ErrWrongHost is a far end that is not the one the link is for.
	ErrWrongHost = errors.New("the other end is not the kakel the link is for")

	// ErrDenied is a far end that knows neither the link nor the key.
	ErrDenied = errors.New("the link is not one that kakel gave")

	// ErrLost is a connection that failed, worth another try.
	ErrLost = errors.New("the connection was lost")

	// ErrNotShared is a pane the host does not show.
	ErrNotShared = errors.New("that pane is not shared")
)

// How long things are given.
const (
	// handshakeWindow is how long a caller has to prove its key, which
	// takes six turns of the line. joinWindow is how long after that it has
	// to join, or to open its control channel when it has joined before.
	// dialWindow is how long a guest waits for the host in all of it.
	handshakeWindow = 8 * time.Second
	joinWindow      = 30 * time.Second
	dialWindow      = 30 * time.Second

	// sayEndedIn bounds how long the last word is waited for.
	sayEndedIn = 500 * time.Millisecond
)

// marshalEnded is why as a frame.
func marshalEnded(why Reason) ([]byte, error) {
	body, err := json.Marshal(ended{why})
	if err != nil {
		return nil, err
	}
	return frame(frameEnded, body), nil
}
