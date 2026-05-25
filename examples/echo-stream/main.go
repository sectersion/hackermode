// Command echo-stream is a minimal hackermode stream-mode module.
// It reads lines from stdin and writes echoed responses to stdout.
// Build with: go build -o echo-stream ./examples/echo-stream
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"

	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/pkg/module"
)

func main() {
	err := module.RunStream(context.Background(),
		module.Info{
			ID:      "examples.echo-stream",
			Version: "0.1.0",
			Commands: []modules.CommandSpec{
				{
					ID:    "examples.echo-stream.greet",
					Title: "Echo: Greet",
					Hint:  "Send a greeting",
					Tags:  []string{"echo", "demo"},
				},
			},
		},
		run,
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "echo-stream:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, h module.Host, in io.Reader, out io.Writer) error {
	_, _ = fmt.Fprintln(out, "echo-stream ready — type something and press enter.")
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		_, _ = fmt.Fprintf(out, "echo: %s\n", line)
	}
	return sc.Err()
}
