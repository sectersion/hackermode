// Package cmd — `hackermode run <dir>`: headless module invocation.
//
// Headless mode bypasses the TUI host entirely. The module's stdin,
// stdout, and stderr are wired to the caller's. fd 3 is still allocated
// (it's the only way the SDK knows the module is being managed by
// hackermode), but the only RPC sent is the init handshake; afterwards
// the module is on its own.
//
// Stream-mode modules become regular CLI tools in shell pipelines:
//
//	echo hi | hackermode run examples/echo-stream
//
// TUI-mode modules attach to the caller's terminal directly — they run
// like any other Bubble Tea program would.
//
// Per the design: every invocation requires a manifest. There is no
// "raw binary" headless path. A directory is the input; the binary
// inside is looked up from the manifest.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/internal/paths"
)

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <dir>",
		Short: "Run a module headless (no TUI). The current terminal becomes the module's I/O.",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			manifest, err := modules.ParseDir(dir)
			if err != nil {
				return fmt.Errorf("manifest: %w", err)
			}
			if vErrs := manifest.Validate(); len(vErrs) > 0 {
				msg := "manifest invalid:"
				for _, e := range vErrs {
					msg += "\n  - " + e.Error()
				}
				return errors.New(msg)
			}
			extra := args[1:]
			return runHeadless(cmd.Context(), dir, manifest, extra)
		},
	}
}

// runHeadless spawns the module binary, forwarding the caller's
// stdin/stdout/stderr directly. fd 3/4 carry a minimal RPC pair just so
// the SDK's openConn() succeeds — we send `init` and discard the reply.
//
// We deliberately do NOT use modules.Spawn here: the host-side Process
// type captures stdout for the TUI scrollback, which we don't want.
// Instead this function reimplements the spawn dance with passthrough
// stdio.
func runHeadless(ctx context.Context, dir string, m *modules.Manifest, extra []string) error {
	if err := paths.EnsureAll(); err != nil {
		return fmt.Errorf("init paths: %w", err)
	}
	if err := hlog.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "warning: log init:", err)
	}
	defer hlog.Close()
	hlog.Info("hackermode run", "module", m.Module.ID, "dir", dir)

	bin := m.Entry.Binary
	if !filepath.IsAbs(bin) {
		bin = filepath.Join(dir, bin)
	}

	// Build fd 3/4 pipes the SDK expects.
	rpcChildRead, rpcHostWrite, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("rpc pipe: %w", err)
	}
	rpcHostRead, rpcChildWrite, err := os.Pipe()
	if err != nil {
		_ = rpcChildRead.Close()
		_ = rpcHostWrite.Close()
		return fmt.Errorf("rpc pipe: %w", err)
	}

	cmd := exec.CommandContext(ctx, bin, extra...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{rpcChildRead, rpcChildWrite}

	if err := cmd.Start(); err != nil {
		closeFiles(rpcChildRead, rpcChildWrite, rpcHostRead, rpcHostWrite)
		return fmt.Errorf("spawn %s: %w", bin, err)
	}
	_ = rpcChildRead.Close()
	_ = rpcChildWrite.Close()

	// Drive the RPC handshake just enough to satisfy the SDK.
	conn := modules.NewConn(rpcHostRead, rpcHostWrite)
	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	go func() { _ = conn.Serve(serveCtx) }()

	if _, err := conn.Call(serveCtx, "init", modules.InitParams{
		TabID:       "headless",
		Mode:        m.Entry.Mode,
		HostVersion: "0.0.0-dev",
	}); err != nil {
		// init failed: try to clean up and report.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_ = cmd.Wait()
		closeFiles(rpcHostRead, rpcHostWrite)
		return fmt.Errorf("handshake: %w", err)
	}

	waitErr := cmd.Wait()
	cancelServe()
	closeFiles(rpcHostRead, rpcHostWrite)

	if exit, ok := exitCode(waitErr); ok {
		os.Exit(exit)
	}
	return waitErr
}

func closeFiles(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}

func exitCode(err error) (int, bool) {
	if err == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 1, false
}

// io is referenced indirectly to keep this file self-contained when
// extending headless I/O later (e.g. wrapping stdout for logging).
var _ io.Writer = os.Stdout
