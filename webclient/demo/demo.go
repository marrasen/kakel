// Package demo is a session with a made-up program at the other end: it
// shows what a terminal can draw, echoes what is typed, and floods the
// screen when asked to. The browser client draws it to be tried and
// tested with nothing to connect to.
package demo

import (
	"fmt"
	"io"
	"strings"
	"sync"
)

// Session is the made-up program. It implements session.Session.
type Session struct {
	// yield lets whoever draws the screen have the thread, between one
	// part of a burst and the next.
	yield func()

	mu sync.Mutex
	// out is what is yet to be read, and more tells a reader there is
	// some, or that the program has ended.
	out  []byte
	more *sync.Cond
	// flood is what is left of a burst, made as it is read, and line how
	// many of its lines have been.
	flood, line int
	// taken is closed once all there is to read has been read, and the
	// reader is back for more.
	taken chan struct{}
	// sent is everything that was typed.
	sent []byte
	// word is the command being typed.
	word       []byte
	cols, rows int
	ended      bool
}

// New starts the program, showing what it has to show. yield is called
// between the parts of a burst, as a network would hand them over, with
// the page free to draw in between: a browser has one thread for both.
func New(yield func()) *Session {
	s := &Session{yield: yield}
	s.more = sync.NewCond(&s.mu)
	s.out = []byte(Screen() + prompt)
	return s
}

// prompt is what the program asks for a command with.
const prompt = "\x1b[1;32mdemo\x1b[0m:\x1b[1;34m~\x1b[0m$ "

// Done is what a burst ends with.
const Done = "burst done"

// Screen is the picture of what a terminal draws, in colours the tests
// know. Its first rows stand at set places: see the tests.
func Screen() string {
	var b strings.Builder
	// The sixteen colours, two blank cells each.
	for i := range 16 {
		fmt.Fprintf(&b, "\x1b[48;5;%dm  ", i)
	}
	b.WriteString("\x1b[0m\r\n")
	// True colours: red, green, blue and a ramp.
	b.WriteString("\x1b[48;2;255;0;0m    \x1b[48;2;0;255;0m    \x1b[48;2;0;0;255m    ")
	for i := range 20 {
		fmt.Fprintf(&b, "\x1b[48;2;%d;%d;160m ", i*12, 255-i*12)
	}
	b.WriteString("\x1b[0m\r\n")
	b.WriteString("\x1b[1mbold\x1b[0m \x1b[3mitalic\x1b[0m \x1b[1;3mboth\x1b[0m \x1b[4munderline\x1b[0m \x1b[9mstruck\x1b[0m \x1b[2mdim\x1b[0m \x1b[7mreversed\x1b[0m \x1b[31mred\x1b[32m green\x1b[33m yellow\x1b[34m blue\x1b[35m magenta\x1b[36m cyan\x1b[0m\r\n")
	b.WriteString("┌──────────┬──────────┐ ╔════╦════╗ ╭────╮ ┏━━━┓\r\n")
	b.WriteString("│ box      │ drawing  │ ║    ║    ║ │    │ ┃   ┃\r\n")
	b.WriteString("├──────────┼──────────┤ ╠════╬════╣ ╰────╯ ┗━━━┛\r\n")
	b.WriteString("└──────────┴──────────┘ ╚════╩════╝ ░░▒▒▓▓██ ▁▂▃▄▅▆▇█ ▏▎▍▌▋▊▉█\r\n")
	b.WriteString("\x1b[44;37m main \x1b[34;42m\ue0b0\x1b[30;42m ok \x1b[32;49m\ue0b0\x1b[0m \x1b[36m\ue0b2\x1b[46;30m left \x1b[0m \ue0a0 \ue0b1 \ue0b3\r\n")
	b.WriteString("wide: 日本語 中文 한국어 | accents: e\u0301 n\u0303 å ä ö ß € | arrows: ← ↑ → ↓ ⇒ ✓ ✗ ★\r\n")
	b.WriteString("emoji: 😀 🚀 👍 ❤ 🇸🇪 👩‍💻 | greek: αβγδ ΩΨ | cyrillic: Привет | math: ∑ ∫ √ ∞ ≠\r\n")
	b.WriteString("programs: ⠋⠙⠹ ⣿⣶ ❯ ✔ ✖ ● ○ ◐ ▶ ⏺ ⏵ ⎿ ✻ ✳ ☐ ☒ ⚠ … • ➜ ╱╲╳ ▖▗▘▚\r\n\r\n")
	return b.String()
}

// Burst prints about n bytes as fast as they are read, ending with Done.
// The channel it returns is closed once the terminal has taken them all.
func (s *Session) Burst(n int) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flood, s.line = n, 0
	s.taken = make(chan struct{})
	s.more.Broadcast()
	return s.taken
}

// Print prints text.
func (s *Session) Print(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.out = append(s.out, text...)
	s.more.Broadcast()
}

// Reset clears the screen, the modes and what was typed, and shows what
// the program showed as it started.
func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flood, s.sent, s.word = 0, nil, nil
	s.out = []byte("\x1bc" + Screen() + prompt)
	s.more.Broadcast()
}

// Sent is everything typed so far.
func (s *Session) Sent() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]byte(nil), s.sent...)
}

// Size is the size the program was last given.
func (s *Session) Size() (cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

// floodChunk is how much of a burst one read gives, as much as a
// network hands over at a time.
const floodChunk = 32 << 10

// Read implements session.Session.
func (s *Session) Read(p []byte) (int, error) {
	s.mu.Lock()
	for len(s.out) == 0 && s.flood == 0 && !s.ended {
		if s.taken != nil {
			close(s.taken)
			s.taken = nil
		}
		s.more.Wait()
	}
	if len(s.out) == 0 && s.flood > 0 {
		s.out = s.chunk()
		s.mu.Unlock()
		s.yield()
		s.mu.Lock()
	}
	defer s.mu.Unlock()
	if len(s.out) > 0 {
		n := copy(p, s.out)
		s.out = s.out[n:]
		return n, nil
	}
	return 0, io.EOF
}

// chunk is the next part of a burst: lines as a build prints them, with
// a colour or two each.
func (s *Session) chunk() []byte {
	var b []byte
	for len(b) < floodChunk && s.flood > 0 {
		before := len(b)
		b = fmt.Appendf(b, "\x1b[3%dm[%6d]\x1b[0m compiling \x1b[1m%s\x1b[0m/%04d.go: the quick brown fox jumps over the lazy dog\r\n",
			1+s.line%6, s.line, []string{"grid", "input", "session", "relay"}[s.line%4], s.line*7%10000)
		s.line++
		s.flood = max(0, s.flood-(len(b)-before))
	}
	if s.flood == 0 {
		b = fmt.Appendf(b, "%s, %d lines\r\n%s", Done, s.line, prompt)
	}
	return b
}

// Write implements session.Session: it echoes what is typed, shows the
// keys that are no text as a shell does, and runs a line that names a
// command.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return 0, io.ErrClosedPipe
	}
	s.sent = append(s.sent, p...)
	for _, c := range p {
		switch {
		case c == '\r':
			s.out = append(s.out, "\r\n"...)
			s.run(string(s.word))
			s.word = s.word[:0]
		case c == 0x7f:
			if len(s.word) > 0 {
				s.word = s.word[:len(s.word)-1]
				s.out = append(s.out, "\b \b"...)
			}
		case c == 0x03:
			s.out = append(s.out, "^C\r\n"+prompt...)
			s.word = s.word[:0]
		case c < ' ':
			s.out = append(s.out, '^', c+'@')
		default:
			s.word = append(s.word, c)
			s.out = append(s.out, c)
		}
	}
	s.more.Broadcast()
	return len(p), nil
}

// run runs a command. It is called with mu held.
func (s *Session) run(line string) {
	var mb int
	switch {
	case line == "":
	case line == "demo":
		s.out = append(s.out, Screen()...)
	case line == "clear":
		s.out = append(s.out, "\x1b[2J\x1b[H"...)
	case line == "paste":
		// Bracketed paste, as a shell asks for.
		s.out = append(s.out, "\x1b[?2004h"...)
	case line == "mouse":
		s.out = append(s.out, "\x1b[?1000h\x1b[?1006h"...)
	case line == "burst":
		s.flood, s.line = 5<<20, 0
		return
	default:
		if n, _ := fmt.Sscanf(line, "burst %d", &mb); n == 1 && mb > 0 && mb <= 100 {
			s.flood, s.line = mb<<20, 0
			return
		}
		s.out = fmt.Appendf(s.out, "%s: not a command here. Try demo, clear, paste, mouse or burst\r\n", line)
	}
	s.out = append(s.out, prompt...)
}

// Resize implements session.Session.
func (s *Session) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
	return nil
}

// Close implements session.Session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ended = true
	s.more.Broadcast()
	return nil
}

// Wait implements session.Session.
func (s *Session) Wait() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for !s.ended {
		s.more.Wait()
	}
	return nil
}
