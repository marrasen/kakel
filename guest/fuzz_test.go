package guest

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"
	"unicode/utf8"
)

// Nothing a far end sends makes a reader hold more than the kind of frame
// may be, or panic.
func FuzzFrames(f *testing.F) {
	f.Add(frame(frameState, []byte(`{"Title":"x"}`)))
	f.Add(frame(frameTyped, []byte("hello")))
	f.Add([]byte{frameTyped, 0xff, 0xff, 0xff, 0xff, 1})
	f.Add(append(frame(frameScreen, []byte("\x00\x50\x00\x18\x1b[H")), frame(frameData, []byte("x"))...))
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, of := range []mosts{controlToGuest, controlToHost, paneToGuest, paneToHost} {
			r := bytes.NewReader(b)
			for {
				kind, body, err := readFrame(r, of, nil)
				if err != nil {
					break
				}
				most, known := of[kind]
				if !known || len(body) > most {
					t.Fatalf("a frame %q of %d bytes was taken", kind, len(body))
				}
				if again := frame(kind, body); !bytes.Contains(b, again) {
					t.Fatalf("%q is not what was sent", again)
				}
			}
		}
	})
}

func FuzzState(f *testing.F) {
	f.Add([]byte(`{"HostName":"anna","Panes":[{"ID":"a","Kind":"terminal","Cols":80,"Rows":24}],"Layout":{"a":1}}`))
	f.Add([]byte(`{"Panes":[{"ID":"a","Cols":-1,"Rows":99999999999}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		st, err := decodeState(b)
		if err != nil {
			return
		}
		if len(st.Panes) > mostPanes || utf8.RuneCountInString(st.Title) > mostTitle {
			t.Fatalf("a state of %d panes", len(st.Panes))
		}
		for _, p := range st.Panes {
			if p.ID == "" || len(p.ID) > mostID || p.Cols > mostCols || p.Rows > mostRows || p.Cols < 0 {
				t.Fatalf("a pane %+v", p)
			}
		}
		if _, err := json.Marshal(st); err != nil {
			t.Fatalf("it cannot be written: %v", err)
		}
	})
}

func FuzzJoin(f *testing.F) {
	f.Add([]byte(`{"Name":"Guest","About":"a browser","Key":"AAAA"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		j, key, err := decodeJoining(b)
		if err == nil && (key == nil || j.Name == "" || utf8.RuneCountInString(j.Name) > mostName || utf8.RuneCountInString(j.About) > mostAbout) {
			t.Fatalf("a join %+v", j)
		}
	})
}

func FuzzPlain(f *testing.F) {
	f.Add("a\u202eb\x1b[31m", 3)
	f.Fuzz(func(t *testing.T, s string, most int) {
		most = min(max(most, 1), 300)
		got := plain(s, most)
		if utf8.RuneCountInString(got) > most || !utf8.ValidString(got) || plain(got, most) != got {
			t.Fatalf("%q is made %q", s, got)
		}
	})
}

func FuzzSecret(f *testing.F) {
	f.Add("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	f.Fuzz(func(t *testing.T, s string) {
		if got, err := ParseSecret(s); err == nil && got.String() != s {
			t.Fatalf("%q reads as %q", s, got)
		}
	})
}

// A header that promises much and sends little takes little room.
func TestAFrameIsHeldOnlyAsItComes(t *testing.T) {
	b := []byte{frameScreen, 0, 0xff, 0xff, 0xff}
	if _, body, err := readFrame(bytes.NewReader(b), paneToGuest, nil); err != io.EOF || cap(body) > 128<<10 {
		t.Errorf("%v, with room for %d", err, cap(body))
	}
}
