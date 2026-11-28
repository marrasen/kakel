package guest

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// What the two ends say to each other.
//
// Three kinds of channel, all opened by the guest, and nothing else: one
// control channel, a channel for each pane it is shown, and channels to
// the share's folder, which carry SFTP. On the first two, each
// way, go frames: a byte that says what it is, the length of its body in
// four bytes, and the body. Every kind has a most, which is checked
// before anything is made to hold it.
const (
	chanControl = "control@kakel"

	// chanPane's payload is openPane.
	chanPane = "pane@kakel"

	// chanFiles has no payload, and carries SFTP: see files.go.
	chanFiles = "files@kakel"

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

	// frameFocus says which pane the guest is in, by its ID: one it
	// picked, so it follows the host no more.
	frameFocus = 'F'

	// frameFollow says the guest follows the host: it is in whichever pane
	// the host is in, until a frameFocus says otherwise. Each says all
	// there is of where the guest is, so only the latest is sent.
	frameFollow = 'H'

	// frameLeave says the guest goes, and will not be back.
	frameLeave = 'L'

	// frameShown names the panes on the guest's page, a line each: those
	// whose size the guest may set. Only the latest is sent.
	frameShown = 'V'
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

// From the guest:
const (
	// frameTyped is what it typed. It begins with the count of
	// frameProgram as the guest had it, in four bytes: what was typed for
	// a program that ended is not for the next.
	frameTyped = 'T'

	// frameFit is the columns and the rows, two bytes each, that the pane
	// has room for on the guest's page, for the host to size the program
	// to while the share has the guest set the size. Both zero give the
	// host's size back. Only the latest counts.
	frameFit = 'Z'
)

// fitLen is how long a frameFit's body is.
const fitLen = 4

// fitFrame asks for the pane to be c.
func fitFrame(c Cells) []byte {
	var b [fitLen]byte
	binary.BigEndian.PutUint16(b[:], uint16(c.Cols))
	binary.BigEndian.PutUint16(b[2:], uint16(c.Rows))
	return frame(frameFit, b[:])
}

// readFit reads the body of a fitFrame: a size to hold, or none, and
// whether it is either.
func readFit(body []byte) (Cells, bool) {
	if len(body) != fitLen {
		return Cells{}, false
	}
	c := Cells{Cols: int(binary.BigEndian.Uint16(body)), Rows: int(binary.BigEndian.Uint16(body[2:]))}
	return c, c == Cells{} || goodSize(c.Cols, c.Rows)
}

// Cells is the size of a terminal, in columns and rows.
type Cells struct{ Cols, Rows int }

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

	// mostFolder is the longest path of a Files pane, in bytes.
	mostFolder = 4096

	// mostRoom is the widest and tallest the host's panes are said to be,
	// as a window across several screens is, and mostCell a terminal's
	// cell, as the largest text makes it: in the host's logical pixels.
	mostRoom, mostCell = 32 << 10, 512
)

// headerLen is the length of what comes before a frame's body.
const headerLen = 5

// mosts is the most of each kind of frame a reader takes.
type mosts map[byte]int

var (
	controlToGuest = mosts{frameJoin: 0, frameWaiting: 0, frameState: mostState, frameEnded: mostEnded}
	controlToHost  = mosts{frameName: mostJoining, frameFocus: mostID, frameFollow: 0, frameLeave: 0, frameShown: mostPanes * (mostID + 1)}
	paneToGuest    = mosts{frameScreen: mostScreen + 4, frameData: mostData, frameProgram: programLen}
	paneToHost     = mosts{frameTyped: countLen + mostTyped, frameFit: fitLen}
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
	// HostSized says the terminal has a size the guest's page does not
	// set, though the state says GuestSizes: one another kakel watching it
	// gives it, or that of screens from elsewhere.
	HostSized bool `json:",omitempty"`
	// Folder is where a Files pane is, inside the share's folder: its
	// path from the folder's top, with slashes, as "/Reports/2026", and
	// "" where it is elsewhere, or no files are shared. Never a path of
	// the host's own.
	Folder string `json:",omitempty"`
}

// Kind is what a pane is.
type Kind string

const (
	// KindTerminal is a pane the guest may be shown.
	KindTerminal Kind = "terminal"
	// KindOther is one that is not shared, which stands in the layout and
	// says so.
	KindOther Kind = "other"
	// KindFiles is a Files pane, which shows the share's folder where the
	// guest may have files, and is not shared where it may not.
	KindFiles Kind = "files"
)

// State is the shared window as it stands.
type State struct {
	// HostName is who shares it. The guest's page is called after the
	// pane the guest is in.
	HostName string

	// CanType says the guest may type. What it types while it may not is
	// dropped by the host, whatever the guest's own program thinks.
	CanType bool

	// Layout and Theme are the caller's own.
	Layout, Theme json.RawMessage `json:",omitempty"`

	Panes []Pane

	// HostIn is the pane the host is in.
	HostIn string `json:",omitempty"`

	// Files is what the guest may do with the share's folder, and Folder
	// what the folder is called: its own name, and not the path to it,
	// which is the host's. No folder shares no files.
	Files  FilesMay `json:",omitempty"`
	Folder string   `json:",omitempty"`
	// Folders counts the folders the host has shared, so a guest knows
	// one from another of the same name.
	Folders uint32 `json:",omitempty"`

	// Room is how large the host's panes are laid out, their bars with
	// them, and Cell how large a terminal's cell is there, both in the
	// host's logical pixels: for a guest to lay the panes out as the host
	// has them, every terminal's text the one size. Each is zero where
	// the host has not said.
	Room Size `json:",omitzero"`
	Cell Size `json:",omitzero"`

	// GuestSizes says the guest's page sets the size of the terminals it
	// shows, rather than the host's window: the page lays its panes out
	// at its own size, and says what fits each, which the host sizes the
	// program to. Sizes the guest says while it does not are dropped by
	// the host.
	GuestSizes bool `json:",omitempty"`
}

// Size is a width and a height, in the host's logical pixels.
type Size struct{ W, H float32 }

// within is s where it is a size that is no larger than most either way,
// and zero where it is not.
func (s Size) within(most float32) Size {
	if !(s.W > 0 && s.W <= most && s.H > 0 && s.H <= most) {
		return Size{}
	}
	return s
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
		HostName: plain(st.HostName, mostName),
		CanType:  st.CanType, GuestSizes: st.GuestSizes, Panes: []Pane{},
		Room: st.Room.within(mostRoom), Cell: st.Cell.within(mostCell),
	}
	few.Files, few.Folder = filesOf(st.Files, plain(st.Folder, mostName))
	few.Folders = st.Folders
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
				Cols: p.Cols, Rows: p.Rows, Ended: p.Ended, HostSized: p.HostSized,
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
	st.HostName = plain(st.HostName, mostTitle)
	st.Files, st.Folder = filesOf(st.Files, plain(st.Folder, mostTitle))
	if len(st.HostIn) > mostID {
		st.HostIn = ""
	}
	st.Room, st.Cell = st.Room.within(mostRoom), st.Cell.within(mostCell)
	panes := st.Panes[:0]
	for _, p := range st.Panes {
		if len(panes) == mostPanes || p.ID == "" || len(p.ID) > mostID {
			continue
		}
		p.Title, p.Icon = plain(p.Title, mostTitle), plain(p.Icon, mostName)
		if p.Kind != KindTerminal && p.Kind != KindFiles {
			p.Kind = KindOther
		}
		p.Folder = FolderPath(p.Folder)
		if p.Kind != KindFiles || st.Files == FilesNone {
			p.Folder = ""
		}
		if !goodSize(p.Cols, p.Rows) {
			p.Cols, p.Rows = 0, 0
		}
		panes = append(panes, p)
	}
	st.Panes = panes
	return st, nil
}

// FolderPath is p where it is a path from the top of the share's folder a
// Files pane may be told of: whole, clean, with slashes, no longer than
// mostFolder, and with nothing in its names that is not plain. Any other
// is "", the top.
func FolderPath(p string) string {
	if p == "" || len(p) > mostFolder || p[0] != '/' || path.Clean(p) != p || p == "/" {
		return ""
	}
	for name := range strings.SplitSeq(p[1:], "/") {
		if name == "" || name == "." || name == ".." || plain(name, mostFolder) != name || strings.Contains(name, `\`) {
			return ""
		}
	}
	return p
}

// filesOf is what a guest may do with a folder called folder: nothing
// with none, and nothing it does not know.
func filesOf(may FilesMay, folder string) (FilesMay, string) {
	if folder == "" || may > FilesChange {
		return FilesNone, folder
	}
	return may, folder
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
