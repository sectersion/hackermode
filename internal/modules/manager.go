// Package modules — Manager: lifecycle + lookup for running module
// subprocesses (Stage 3.3).
//
// The Manager is the host-side authority over running modules. Multiple
// sessions can share a single Process (e.g. two tabs from the same
// module) or each session can have its own — the policy lives here, not
// in the tab UI.
//
// For Phase B the policy is the simpler one: **one Process per
// (module ID, tab ID) pair**, i.e. tabs do not share processes. This
// matches the "browser tab" mental model and avoids inter-tab state
// surprises. Phase E may revisit for memory-heavy modules that want to
// share a backend across tabs.
//
// The Manager does NOT spawn modules on its own. The host calls Start
// after a launcher / transition decides a module should run. Stage 5
// adds lazy spawning for palette-invoked commands.
package modules

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	hlog "github.com/sectersion/hackermode/internal/log"
)

// Manager tracks running module processes.
type Manager struct {
	mu        sync.RWMutex
	processes map[string]*Process // key = manager key (module + tab)
}

// NewManager returns an empty manager.
func NewManager() *Manager {
	return &Manager{processes: map[string]*Process{}}
}

// StartOpts feeds a launch.
type StartOpts struct {
	TabID    string
	Manifest *Manifest
	Init     InitParams
	Dir      string
	Env      []string

	// PTYCols / PTYRows are used for tui-mode modules. The manager
	// derives PTY=true from Manifest.Entry.Mode == ModeTUI; this is
	// only the initial size hint.
	PTYCols uint16
	PTYRows uint16
}

// Start spawns a module, performs handshake, and tracks the resulting
// Process under (module.id, tab id). Returns the started process and its
// ModuleInfo reply.
//
// The caller is expected to have updated the corresponding Session via
// Session.Transition before calling Start; this method does not touch
// the session directly.
func (mgr *Manager) Start(ctx context.Context, opts StartOpts) (*Process, *ModuleInfo, error) {
	if opts.Manifest == nil {
		return nil, nil, errors.New("manager: manifest required")
	}
	if opts.TabID == "" {
		return nil, nil, errors.New("manager: tab id required")
	}
	key := mgrKey(opts.Manifest.Module.ID, opts.TabID)

	mgr.mu.Lock()
	if existing, ok := mgr.processes[key]; ok {
		mgr.mu.Unlock()
		return existing, nil, fmt.Errorf("manager: already running: %s", key)
	}
	mgr.mu.Unlock()

	proc, err := Spawn(ctx, opts.Manifest, ProcessOpts{
		Dir:     opts.Dir,
		Env:     opts.Env,
		PTY:     opts.Manifest.Entry.Mode == ModeTUI,
		PTYCols: opts.PTYCols,
		PTYRows: opts.PTYRows,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("spawn: %w", err)
	}

	info, err := proc.Handshake(ctx, opts.Init)
	if err != nil {
		// Best-effort tear down.
		_ = proc.Shutdown(ctx, 500*time.Millisecond)
		return nil, nil, fmt.Errorf("handshake: %w", err)
	}

	mgr.mu.Lock()
	mgr.processes[key] = proc
	mgr.mu.Unlock()
	hlog.With("module", opts.Manifest.Module.ID, "tab", opts.TabID).
		Info("module started", "version", opts.Manifest.Module.Version)

	// Auto-clean on exit.
	go func() {
		<-proc.Done()
		mgr.mu.Lock()
		delete(mgr.processes, key)
		mgr.mu.Unlock()
		hlog.With("module", opts.Manifest.Module.ID, "tab", opts.TabID).
			Info("module exited", "err", errString(proc.ExitErr()))
		_ = info
	}()

	return proc, info, nil
}

// Stop shuts down a module attached to (moduleID, tabID). The grace period
// is the same as Process.Shutdown.
func (mgr *Manager) Stop(ctx context.Context, moduleID, tabID string, graceful time.Duration) error {
	mgr.mu.RLock()
	proc, ok := mgr.processes[mgrKey(moduleID, tabID)]
	mgr.mu.RUnlock()
	if !ok {
		return nil
	}
	return proc.Shutdown(ctx, graceful)
}

// Get returns the running process for (moduleID, tabID), or nil.
func (mgr *Manager) Get(moduleID, tabID string) *Process {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	return mgr.processes[mgrKey(moduleID, tabID)]
}

// All returns a snapshot of every tracked process.
func (mgr *Manager) All() []*Process {
	mgr.mu.RLock()
	defer mgr.mu.RUnlock()
	out := make([]*Process, 0, len(mgr.processes))
	for _, p := range mgr.processes {
		out = append(out, p)
	}
	return out
}

// ShutdownAll tells every tracked module to exit. Waits for all to be
// reaped or ctx to be cancelled.
func (mgr *Manager) ShutdownAll(ctx context.Context, graceful time.Duration) {
	for _, p := range mgr.All() {
		_ = p.Shutdown(ctx, graceful)
	}
}

func mgrKey(moduleID, tabID string) string { return moduleID + "\x00" + tabID }

func errString(e error) string {
	if e == nil {
		return ""
	}
	return e.Error()
}
