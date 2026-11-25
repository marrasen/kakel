//go:build js && wasm

// Command guestpage is the page the relay's tests share a window with:
// it joins when it is told to, and says how that went.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall/js"
	"time"

	"github.com/marrasen/kakel/relay"
)

func main() {
	// kakelJoin(do) joins the share whose page this is, and promises what
	// became of it.
	js.Global().Set("kakelJoin", js.FuncOf(func(_ js.Value, args []js.Value) any {
		do := args[0].String()
		return js.Global().Get("Promise").New(js.FuncOf(func(_ js.Value, p []js.Value) any {
			go func() { p[0].Invoke(join(do)) }()
			return nil
		}))
	}))
	select {}
}

// join joins, and does with the stream what do says.
func join(do string) string {
	link, err := relay.ParseShareLink(js.Global().Get("location").Get("href").String())
	if err != nil {
		return err.Error()
	}
	ctx := context.Background()
	if do == "give up" {
		c, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
		defer cancel()
		ctx = c
	}
	c, err := relay.JoinShare(ctx, link, relay.Dialer{})
	var why relay.Refused
	switch {
	case errors.As(err, &why):
		return fmt.Sprintf("refused %d", int(why))
	case err != nil:
		return err.Error()
	}
	defer func() { _ = c.Close() }()
	switch do {
	case "echo":
		// More than one message's worth, both ways.
		sent := bytes.Repeat([]byte("kakel "), 50000)
		go func() { _, _ = c.Write(sent) }()
		got := make([]byte, len(sent))
		if _, err := io.ReadFull(c, got); err != nil {
			return err.Error()
		}
		return fmt.Sprintf("echoed %v for %s", bytes.Equal(got, sent), link.Secret)
	case "late":
		_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		_, err := c.Read(make([]byte, 1))
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			return fmt.Sprint("read ", err)
		}
		// And again, with time to.
		_ = c.SetReadDeadline(time.Time{})
		go func() { _, _ = c.Write([]byte("x")) }()
		_, err = c.Read(make([]byte, 1))
		return fmt.Sprint("late, then ", err)
	case "end":
		// Until the host closes, after all it sent.
		_, _ = c.Write([]byte("end"))
		got, err := io.ReadAll(c)
		return fmt.Sprintf("read %d, %v", len(got), err)
	case "close":
		_ = c.Close()
		_, err := c.Read(make([]byte, 1))
		return fmt.Sprint("closed, ", err)
	}
	return "joined"
}
