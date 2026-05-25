// Package module is the public SDK for hackermode modules.
//
// A module is a standalone Go binary that the host spawns and talks to
// over fd 3 (line-delimited JSON-RPC 2.0). Module authors don't touch
// fd 3 directly; they call RunStream or RunTUI and get a Host handle
// that exposes the host's RPC surface as Go methods.
//
// Minimal stream-mode module:
//
//	func main() {
//	    err := module.RunStream(context.Background(),
//	        module.Info{ID: "acme.echo", Version: "0.1.0"},
//	        func(ctx context.Context, h module.Host, in io.Reader, out io.Writer) error {
//	            sc := bufio.NewScanner(in)
//	            for sc.Scan() {
//	                fmt.Fprintf(out, "echo: %s\n", sc.Text())
//	            }
//	            return sc.Err()
//	        })
//	    if err != nil { os.Exit(1) }
//	}
package module

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/sectersion/hackermode/internal/modules"
)

// Info identifies a module to the host during the init handshake.
type Info struct {
	ID           string
	Version      string
	Capabilities []string
	Commands     []modules.CommandSpec
	Keybinds     []modules.KeybindSpec
}

// Host is the SDK-facing view of the host.
type Host interface {
	Notify(level, title, body string) error
	SetTitle(tabID, title string) error
	SetStatus(tabID, status string) error
	Log(level, msg string, kv ...any) error

	// SetPanelWidget pushes / updates a side-panel widget for this
	// module. Key is a stable identifier within the module. To remove,
	// use RemovePanelWidget.
	SetPanelWidget(key, title, text string) error

	// RemovePanelWidget removes a previously-set widget.
	RemovePanelWidget(key string) error

	// RegisterCommand registers a dynamic palette command at runtime.
	// Static manifest commands are auto-registered during init.
	RegisterCommand(spec modules.CommandSpec) error

	// UnregisterCommand removes a previously-registered command by ID.
	UnregisterCommand(id string) error

	// RequestSecret asks the host for a value stored in the OS keychain
	// under this module's namespace.
	RequestSecret(ctx context.Context, key string) (string, error)

	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Send(method string, params any) error
}

// StreamFunc is the user-supplied entry point for a stream-mode module.
type StreamFunc func(ctx context.Context, h Host, in io.Reader, out io.Writer) error

// RunStream wires fd 3, performs the init handshake, then invokes fn with
// stdin/stdout. Blocks until fn returns or the host closes the channel.
func RunStream(ctx context.Context, info Info, fn StreamFunc) error {
	conn, err := openConn()
	if err != nil {
		return err
	}
	h := &hostImpl{conn: conn}

	conn.Handle("init", func(_ context.Context, _ json.RawMessage) (any, error) {
		return modules.ModuleInfo{
			ModuleID:     info.ID,
			Version:      info.Version,
			Commands:     info.Commands,
			Keybinds:     info.Keybinds,
			Capabilities: info.Capabilities,
		}, nil
	})

	shutdown := make(chan struct{})
	conn.Handle("shutdown", func(_ context.Context, _ json.RawMessage) (any, error) {
		close(shutdown)
		return nil, nil
	})

	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	go func() { _ = conn.Serve(serveCtx) }()

	userDone := make(chan error, 1)
	go func() {
		userDone <- fn(serveCtx, h, os.Stdin, os.Stdout)
	}()

	select {
	case err := <-userDone:
		return err
	case <-shutdown:
		cancelServe()
		select {
		case err := <-userDone:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-ctx.Done():
		return ctx.Err()
	}
}

type hostImpl struct{ conn *modules.Conn }

func (h *hostImpl) Notify(level, title, body string) error {
	return h.conn.Notify("notify", map[string]string{"level": level, "title": title, "body": body})
}
func (h *hostImpl) SetTitle(tabID, title string) error {
	return h.conn.Notify("set_title", map[string]string{"tab_id": tabID, "text": title})
}
func (h *hostImpl) SetStatus(tabID, status string) error {
	return h.conn.Notify("set_status", map[string]string{"tab_id": tabID, "text": status})
}
func (h *hostImpl) Log(level, msg string, kv ...any) error {
	payload := map[string]any{"level": level, "msg": msg}
	for i := 0; i+1 < len(kv); i += 2 {
		k, ok := kv[i].(string)
		if !ok {
			continue
		}
		payload[k] = kv[i+1]
	}
	return h.conn.Notify("log", payload)
}
func (h *hostImpl) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	return h.conn.Call(ctx, method, params)
}
func (h *hostImpl) Send(method string, params any) error { return h.conn.Notify(method, params) }

func (h *hostImpl) SetPanelWidget(key, title, text string) error {
	return h.conn.Notify("panel", map[string]any{
		"key": key, "title": title, "text": text,
	})
}
func (h *hostImpl) RemovePanelWidget(key string) error {
	return h.conn.Notify("panel", map[string]any{"key": key, "remove": true})
}
func (h *hostImpl) RegisterCommand(spec modules.CommandSpec) error {
	return h.conn.Notify("register_command", spec)
}
func (h *hostImpl) UnregisterCommand(id string) error {
	return h.conn.Notify("unregister_command", map[string]string{"id": id})
}
func (h *hostImpl) RequestSecret(ctx context.Context, key string) (string, error) {
	raw, err := h.conn.Call(ctx, "request_secret", map[string]string{"key": key})
	if err != nil {
		return "", err
	}
	var p struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return "", err
	}
	return p.Value, nil
}

func openConn() (*modules.Conn, error) {
	r := os.NewFile(3, "hackermode-rpc-in")
	w := os.NewFile(4, "hackermode-rpc-out")
	if r == nil || w == nil {
		return nil, fmt.Errorf("module: rpc fds not present (fd 3 / fd 4) — not launched by hackermode")
	}
	return modules.NewConn(r, w), nil
}

// TUIFactory builds the user's bubbletea Model after the init handshake.
// Returning an error aborts the module.
type TUIFactory func(ctx context.Context, h Host) (tea.Model, error)

// RunTUI is the TUI-mode entry point. It wires fd 3, performs the init
// handshake, then runs the user-supplied bubbletea Model. Stdin/stdout
// are the PTY slave end provided by the host; the program sees a real
// terminal.
func RunTUI(ctx context.Context, info Info, factory TUIFactory) error {
	conn, err := openConn()
	if err != nil {
		return err
	}
	h := &hostImpl{conn: conn}

	conn.Handle("init", func(_ context.Context, _ json.RawMessage) (any, error) {
		return modules.ModuleInfo{
			ModuleID:     info.ID,
			Version:      info.Version,
			Commands:     info.Commands,
			Keybinds:     info.Keybinds,
			Capabilities: info.Capabilities,
		}, nil
	})

	shutdownC := make(chan struct{})
	conn.Handle("shutdown", func(_ context.Context, _ json.RawMessage) (any, error) {
		close(shutdownC)
		return nil, nil
	})

	serveCtx, cancelServe := context.WithCancel(ctx)
	defer cancelServe()
	go func() { _ = conn.Serve(serveCtx) }()

	model, err := factory(serveCtx, h)
	if err != nil {
		return err
	}

	// Bubble Tea consumes the entire terminal (PTY slave on our side);
	// we don't pass tea.WithInput/tea.WithOutput so it uses os.Stdin/Stdout
	// which are the PTY slave courtesy of the host's spawnPTY wiring.
	p := tea.NewProgram(model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	doneC := make(chan error, 1)
	go func() {
		_, runErr := p.Run()
		doneC <- runErr
	}()

	select {
	case err := <-doneC:
		return err
	case <-shutdownC:
		p.Quit()
		return <-doneC
	case <-ctx.Done():
		p.Quit()
		return ctx.Err()
	}
}
