// Package dispatch maps low-level input events (key presses, command
// activations) to named actions, and routes those actions to the registered
// handler. It is the single source of truth for "what does this key do?".
//
// Why this exists:
//
//   - app.Update used to be a hand-ordered `if key.Matches(...)` chain.
//     Each new keybind made the file longer and the ordering more fragile.
//   - Modules will contribute commands and bindings in Stage 5. Without a
//     central registry there is no way to detect conflicts, list everything
//     for the palette, or generate help.
//   - The same routing layer powers the command palette, the slash-command
//     bridge, and (later) inter-module RPC. They all reduce to "I have an
//     action ID, run it."
//
// Concepts:
//
//   - ActionID — a stable identifier like "host.tab.new" or "email.compose".
//   - Handler — a func(Context) Result invoked when the action fires.
//   - Binding — a key.Binding from bubbles/key that matches one or more
//     keystrokes; multiple bindings may map to the same action.
//
// The dispatcher does not know about commands as palette entries — that's
// a separate registry (internal/commands, Stage 2). Dispatch is the
// keymap-and-handler layer underneath.
package dispatch

import (
	"sort"
	"sync"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// ActionID is a stable identifier for a registered action.
type ActionID string

// Context is what a Handler receives. It is intentionally small; richer
// state (e.g. the active tab, model references) is captured by the closure
// the host uses to build the Handler.
type Context struct {
	// KeyMsg is the originating keypress, if the action was triggered via
	// a binding. Zero-value when invoked from the palette or programmatic
	// callers.
	KeyMsg tea.KeyMsg
	// Source describes how the action was triggered ("key", "palette",
	// "slash", "rpc"). Useful for logging and conditional behavior.
	Source string
}

// Result is what a Handler returns. Cmd is forwarded to bubbletea; Consumed
// signals whether the originating event should be considered handled (used
// to short-circuit further routing).
type Result struct {
	Cmd      tea.Cmd
	Consumed bool
}

// Handler runs an action. Receivers should be quick — long-running work
// belongs in the returned tea.Cmd.
type Handler func(Context) Result

// Dispatcher is the central registry. Safe to construct with the
// zero-value or via New().
type Dispatcher struct {
	mu       sync.RWMutex
	actions  map[ActionID]Handler
	bindings []binding // ordered: registration order; first match wins
}

type binding struct {
	action ActionID
	keys   key.Binding
}

// New returns an empty dispatcher.
func New() *Dispatcher {
	return &Dispatcher{
		actions: make(map[ActionID]Handler),
	}
}

// Register binds an action ID to its handler. Registering the same ID
// twice replaces the previous handler — the host re-registers after a
// config reload, and modules may update their handlers at runtime.
func (d *Dispatcher) Register(id ActionID, h Handler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.actions[id] = h
}

// Unregister removes an action and any bindings pointing at it.
func (d *Dispatcher) Unregister(id ActionID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.actions, id)
	d.bindings = filterBindings(d.bindings, id)
}

// Bind attaches a key binding to an action. Multiple bindings may target
// the same action; the action need not be registered yet.
func (d *Dispatcher) Bind(id ActionID, b key.Binding) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bindings = append(d.bindings, binding{action: id, keys: b})
}

// ClearBindings removes all key bindings (but keeps action handlers). Used
// when reloading the keymap from config.
func (d *Dispatcher) ClearBindings() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bindings = nil
}

// Invoke runs the action by ID. Returns the handler's Result; if the action
// is unknown, returns Result{} and Consumed=false.
func (d *Dispatcher) Invoke(id ActionID, ctx Context) Result {
	d.mu.RLock()
	h, ok := d.actions[id]
	d.mu.RUnlock()
	if !ok {
		return Result{}
	}
	if ctx.Source == "" {
		ctx.Source = "programmatic"
	}
	return h(ctx)
}

// HandleKey checks every registered binding in registration order; the
// first match's action is invoked. Returns the handler's Result and the
// matched action ID (empty if none). Multiple bindings for the same action
// are allowed; only the first matching one fires the handler (once).
func (d *Dispatcher) HandleKey(msg tea.KeyMsg) (Result, ActionID) {
	d.mu.RLock()
	binds := append([]binding(nil), d.bindings...) // snapshot
	d.mu.RUnlock()

	for _, b := range binds {
		if !key.Matches(msg, b.keys) {
			continue
		}
		res := d.Invoke(b.action, Context{KeyMsg: msg, Source: "key"})
		return res, b.action
	}
	return Result{}, ""
}

// Actions returns a sorted snapshot of every registered action ID. Useful
// for diagnostics and the upcoming command registry's "what's bound where"
// view.
func (d *Dispatcher) Actions() []ActionID {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]ActionID, 0, len(d.actions))
	for id := range d.actions {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// HasAction reports whether an action is registered.
func (d *Dispatcher) HasAction(id ActionID) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.actions[id]
	return ok
}

func filterBindings(in []binding, drop ActionID) []binding {
	out := in[:0]
	for _, b := range in {
		if b.action != drop {
			out = append(out, b)
		}
	}
	return out
}
