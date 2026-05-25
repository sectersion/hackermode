// Command clock-widget pushes a live side-panel widget with the current
// time, then echoes anything on stdin. Demonstrates panel widgets +
// asynchronous updates from a stream-mode module.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/sectersion/hackermode/pkg/module"
)

func main() {
	err := module.RunStream(context.Background(),
		module.Info{ID: "examples.clock-widget", Version: "0.1.0"},
		run,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "clock-widget:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, h module.Host, in io.Reader, out io.Writer) error {
	// Tick a widget every second.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				_ = h.SetPanelWidget("clock", "now", now.Format("15:04:05"))
			}
		}
	}()

	_, _ = fmt.Fprintln(out, "clock-widget — watch the side panel.")
	sc := bufio.NewScanner(in)
	for sc.Scan() {
		line := sc.Text()
		_, _ = fmt.Fprintf(out, "you said: %s\n", line)
	}
	_ = h.RemovePanelWidget("clock")
	return sc.Err()
}
