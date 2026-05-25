// Package modules — module subprocess management (Stage 3.3 + 4.1).
//
// A Process owns one running module subprocess:
//
//   - stream mode: stdin/stdout are plain pipes; stderr is captured.
//   - tui mode: a PTY is allocated. The child's stdin/stdout/stderr all
//     point at the slave end so Bubble Tea sees a real terminal. The
//     host owns the master end (Process.PTY()) and reads frames from /
//     writes keys to it.
//
// fd 3 — JSON-RPC control channel — is used the same way in both modes.
//
// Lifecycle:
//
//   1. Spawn — fork+exec the binary.
//   2. Handshake — send `init`, wait for `module_info`.
//   3. Running — host invokes commands, module pushes notifications.
//   4. Shutdown — host sends `shutdown` notification, then waits up to
//      `graceful` for clean exit; SIGTERM, then SIGKILL after another
//      5 seconds if still alive.
package modules

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	hlog "github.com/sectersion/hackermode/internal/log"
)

// Process is a running module subprocess.
type Process struct {
	Manifest *Manifest

	cmd     *exec.Cmd
	conn    *Conn
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	rpcRead *os.File
	rpcSend *os.File

	// PTY-mode fields (nil for stream mode).
	ptyMaster *os.File
	ptySlave  *os.File

	mu      sync.Mutex
	state   State
	exitErr error
	done    chan struct{}
}

// ProcessOpts configure how a module is launched.
type ProcessOpts struct {
	// Dir is the directory containing the manifest. The binary is resolved
	// relative to this directory if not absolute.
	Dir string
	// Env is the environment for the child. If empty, os.Environ() is used.
	Env []string
	// PTY, when true, allocates a pseudo-terminal and connects the child's
	// stdin/stdout/stderr to the slave end. The host reads/writes the
	// master via Process.PTY(). Used for tui-mode modules.
	PTY bool
	// PTYCols and PTYRows are the initial terminal size. Both default to
	// 80x24 when zero.
	PTYCols uint16
	PTYRows uint16
}

// Spawn forks the module binary described by m. The returned Process is
// in StateSpawning until Handshake completes.
func Spawn(ctx context.Context, m *Manifest, opts ProcessOpts) (*Process, error) {
	if m == nil {
		return nil, errors.New("modules: manifest required")
	}
	binPath := m.Entry.Binary
	if !filepath.IsAbs(binPath) {
		binPath = filepath.Join(opts.Dir, binPath)
	}

	// fd 3 plumbing: two anonymous pipes, one each direction.
	rpcChildRead, rpcHostWrite, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("rpc pipe (child read): %w", err)
	}
	rpcHostRead, rpcChildWrite, err := os.Pipe()
	if err != nil {
		_ = rpcChildRead.Close()
		_ = rpcHostWrite.Close()
		return nil, fmt.Errorf("rpc pipe (host read): %w", err)
	}

	cmd := exec.CommandContext(ctx, binPath)
	cmd.Dir = opts.Dir
	if len(opts.Env) > 0 {
		cmd.Env = opts.Env
	} else {
		cmd.Env = os.Environ()
	}
	// ExtraFiles[0] becomes fd 3 in the child, [1] becomes fd 4.
	cmd.ExtraFiles = []*os.File{rpcChildRead, rpcChildWrite}

	if opts.PTY {
		return spawnPTY(ctx, m, cmd, opts, rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
	}
	return spawnStream(ctx, m, cmd, rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
}

// spawnStream is the stream-mode path: plain pipes for stdin/stdout, no PTY.
func spawnStream(ctx context.Context, m *Manifest, cmd *exec.Cmd,
	rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite *os.File) (*Process, error) {

	stdin, err := cmd.StdinPipe()
	if err != nil {
		closeAll(rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		closeAll(rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		closeAll(rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		closeAll(rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
		return nil, fmt.Errorf("spawn %s: %w", cmd.Path, err)
	}
	_ = rpcChildRead.Close()
	_ = rpcChildWrite.Close()

	p := &Process{
		Manifest: m,
		cmd:      cmd,
		conn:     NewConn(rpcHostRead, rpcHostWrite),
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		rpcRead:  rpcHostRead,
		rpcSend:  rpcHostWrite,
		state:    StateSpawning,
		done:     make(chan struct{}),
	}
	go p.consumeStderr()
	go p.runConn(ctx)
	go p.wait()
	return p, nil
}

// spawnPTY allocates a PTY, attaches it as the child's controlling terminal,
// and routes the child's stdin/stdout/stderr through the slave end.
func spawnPTY(ctx context.Context, m *Manifest, cmd *exec.Cmd, opts ProcessOpts,
	rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite *os.File) (*Process, error) {

	ptm, pts, err := pty.Open()
	if err != nil {
		closeAll(rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
		return nil, fmt.Errorf("pty open: %w", err)
	}

	cols := opts.PTYCols
	if cols == 0 {
		cols = 80
	}
	rows := opts.PTYRows
	if rows == 0 {
		rows = 24
	}
	_ = pty.Setsize(ptm, &pty.Winsize{Cols: cols, Rows: rows})

	cmd.Stdin = pts
	cmd.Stdout = pts
	cmd.Stderr = pts
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true

	if err := cmd.Start(); err != nil {
		_ = ptm.Close()
		_ = pts.Close()
		closeAll(rpcHostRead, rpcHostWrite, rpcChildRead, rpcChildWrite)
		return nil, fmt.Errorf("spawn %s: %w", cmd.Path, err)
	}
	_ = rpcChildRead.Close()
	_ = rpcChildWrite.Close()

	p := &Process{
		Manifest:  m,
		cmd:       cmd,
		conn:      NewConn(rpcHostRead, rpcHostWrite),
		stdin:     ptm,   // writes to master are visible as keystrokes to the child
		stdout:    ptm,   // reads from master are the child's terminal output
		rpcRead:   rpcHostRead,
		rpcSend:   rpcHostWrite,
		ptyMaster: ptm,
		ptySlave:  pts,
		state:     StateSpawning,
		done:      make(chan struct{}),
	}
	// stderr is multiplexed with stdout through the PTY; no separate
	// consumer goroutine is needed.
	go p.runConn(ctx)
	go p.wait()
	return p, nil
}

func closeAll(files ...*os.File) {
	for _, f := range files {
		if f != nil {
			_ = f.Close()
		}
	}
}

// Conn returns the JSON-RPC control connection.
func (p *Process) Conn() *Conn { return p.conn }

// Stdin / Stdout accessors. For stream-mode modules these are pipes; for
// TUI-mode modules they are the PTY master (write = keystrokes to child,
// read = terminal frames from child).
func (p *Process) Stdin() io.WriteCloser { return p.stdin }
func (p *Process) Stdout() io.ReadCloser { return p.stdout }

// PTY returns the PTY master file for TUI-mode modules. Returns nil for
// stream-mode modules.
func (p *Process) PTY() *os.File { return p.ptyMaster }

// IsPTY reports whether the process was launched with a PTY.
func (p *Process) IsPTY() bool { return p.ptyMaster != nil }

// ResizePTY updates the PTY's terminal dimensions. No-op for stream mode.
func (p *Process) ResizePTY(cols, rows uint16) error {
	if p.ptyMaster == nil {
		return nil
	}
	return pty.Setsize(p.ptyMaster, &pty.Winsize{Cols: cols, Rows: rows})
}

// State returns the current lifecycle state.
func (p *Process) State() State {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state
}

// Done returns a channel that closes when the process exits.
func (p *Process) Done() <-chan struct{} { return p.done }

// ExitErr returns the wait error after Done.
func (p *Process) ExitErr() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.exitErr
}

// InitParams is the host→module `init` payload.
type InitParams struct {
	TabID       string `json:"tab_id"`
	Mode        Mode   `json:"mode"`
	Theme       string `json:"theme"`
	HostVersion string `json:"host_version"`
	Width       int    `json:"size_w"`
	Height      int    `json:"size_h"`
}

// ModuleInfo is the module's reply to `init`.
type ModuleInfo struct {
	ModuleID    string        `json:"module_id"`
	Version     string        `json:"version"`
	Commands    []CommandSpec `json:"commands"`
	Keybinds    []KeybindSpec `json:"keybinds"`
	Capabilities []string     `json:"capabilities"`
}

// Handshake sends the `init` request and waits for the module's reply.
// Transitions state Spawning → Running on success, or Errored on failure.
func (p *Process) Handshake(ctx context.Context, params InitParams) (*ModuleInfo, error) {
	raw, err := p.conn.Call(ctx, "init", params)
	if err != nil {
		p.setState(StateErrored)
		return nil, fmt.Errorf("init: %w", err)
	}
	var info ModuleInfo
	if err := json.Unmarshal(raw, &info); err != nil {
		p.setState(StateErrored)
		return nil, fmt.Errorf("decode module_info: %w", err)
	}
	p.setState(StateRunning)
	return &info, nil
}

// Shutdown sends the `shutdown` notification, then waits up to graceful
// for the process to exit. If still alive, sends SIGTERM; after another
// 5 seconds, SIGKILL.
func (p *Process) Shutdown(ctx context.Context, graceful time.Duration) error {
	_ = p.conn.Notify("shutdown", nil)

	select {
	case <-p.done:
		return p.ExitErr()
	case <-time.After(graceful):
	case <-ctx.Done():
		return ctx.Err()
	}

	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.done:
		return p.ExitErr()
	case <-time.After(5 * time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}

	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	<-p.done
	return p.ExitErr()
}

func (p *Process) setState(s State) {
	p.mu.Lock()
	p.state = s
	p.mu.Unlock()
}

func (p *Process) consumeStderr() {
	buf := make([]byte, 4*1024)
	for {
		n, err := p.stderr.Read(buf)
		if n > 0 {
			hlog.With("module", p.Manifest.Module.ID).Warn("stderr", "data", string(buf[:n]))
		}
		if err != nil {
			return
		}
	}
}

func (p *Process) runConn(ctx context.Context) {
	if err := p.conn.Serve(ctx); err != nil && !errors.Is(err, io.EOF) {
		hlog.With("module", p.Manifest.Module.ID).Warn("rpc serve ended", "err", err.Error())
	}
}

func (p *Process) wait() {
	err := p.cmd.Wait()
	p.mu.Lock()
	if p.state != StateClosed {
		if err != nil {
			p.state = StateErrored
		} else {
			p.state = StateClosed
		}
	}
	p.exitErr = err
	p.mu.Unlock()
	close(p.done)
	_ = p.rpcRead.Close()
	_ = p.rpcSend.Close()
	if p.ptyMaster != nil {
		_ = p.ptyMaster.Close()
	}
	if p.ptySlave != nil {
		_ = p.ptySlave.Close()
	}
}
