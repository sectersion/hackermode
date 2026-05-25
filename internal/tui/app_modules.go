// Package tui — module-side hooks: command-registry projection, dynamic
// register/unregister RPC handlers, and host-side handlers for module
// notifications (notify, set_title, set_status, log, panel widgets).
//
// This file is the glue between a freshly-spawned modules.Process and the
// host's UI state. Everything here runs on Bubble Tea goroutines triggered
// by inbound RPC; mutations of Model.state are not safe here. Instead we
// route side-effects back to the event loop via runtime.Send.
package tui

import (
	"context"
	"encoding/json"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sectersion/hackermode/internal/commands"
	"github.com/sectersion/hackermode/internal/installer"
	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/internal/secrets"
	"github.com/sectersion/hackermode/internal/tui/dispatch"
	"github.com/sectersion/hackermode/internal/tui/ptyrender"
	"github.com/sectersion/hackermode/internal/tui/sidepanel"
	"github.com/sectersion/hackermode/internal/tui/streambridge"
	"github.com/sectersion/hackermode/internal/tui/tabs"
)

// dispatchActionID re-exports dispatch.ActionID conversion for readability.
func dispatchActionID(s string) dispatch.ActionID { return dispatch.ActionID(s) }

// registerModuleCommands projects the manifest-declared static commands
// from a module's handshake into the host command registry. Each entry
// is namespaced under the module ID (validation already enforces this);
// owner is the module ID so a future UnregisterOwner sweeps them out
// when the module exits.
func (m *Model) registerModuleCommands(moduleID string, specs []modules.CommandSpec) {
	for _, s := range specs {
		if err := m.cmds.Register(commands.Command{
			ID:       s.ID,
			Owner:    moduleID,
			Title:    s.Title,
			Hint:     s.Hint,
			Tags:     s.Tags,
			Keybind:  s.Keybind,
			When:     s.When,
			Launches: s.Launches,
		}); err != nil {
			hlog.With("module", moduleID).Warn("register manifest command failed",
				"id", s.ID, "err", err.Error())
		}
	}
}

// installModuleRPCHandlers attaches the host-side handlers a module
// expects on its fd-3 control channel. Each handler decodes the JSON
// params, performs the side-effect (often via runtime.Send for thread
// safety), and returns a result or RPC error.
func (m *Model) installModuleRPCHandlers(proc *modules.Process, moduleID string) {
	conn := proc.Conn()

	conn.Handle("notify", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Level string `json:"level"`
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		_ = json.Unmarshal(raw, &p)
		m.dispatchAsync(notifyMsg{level: p.Level, title: p.Title, body: p.Body})
		return nil, nil
	})

	conn.Handle("set_title", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			TabID string `json:"tab_id"`
			Text  string `json:"text"`
		}
		_ = json.Unmarshal(raw, &p)
		m.dispatchAsync(setTitleMsg{tabID: p.TabID, title: p.Text})
		return nil, nil
	})

	conn.Handle("set_status", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			TabID string `json:"tab_id"`
			Text  string `json:"text"`
		}
		_ = json.Unmarshal(raw, &p)
		m.dispatchAsync(setStatusMsg{tabID: p.TabID, status: p.Text})
		return nil, nil
	})

	conn.Handle("log", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p map[string]any
		_ = json.Unmarshal(raw, &p)
		lvl, _ := p["level"].(string)
		msg, _ := p["msg"].(string)
		// Promote module fields into the log line.
		kv := []any{"module", moduleID}
		for k, v := range p {
			if k == "level" || k == "msg" {
				continue
			}
			kv = append(kv, k, v)
		}
		switch strings.ToLower(lvl) {
		case "debug":
			hlog.Debug(msg, kv...)
		case "warn", "warning":
			hlog.Warn(msg, kv...)
		case "error", "err":
			hlog.Error(msg, kv...)
		default:
			hlog.Info(msg, kv...)
		}
		return nil, nil
	})

	conn.Handle("register_command", func(_ context.Context, raw json.RawMessage) (any, error) {
		var s modules.CommandSpec
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		return nil, m.cmds.Register(commands.Command{
			ID: s.ID, Owner: moduleID, Title: s.Title, Hint: s.Hint,
			Tags: s.Tags, Keybind: s.Keybind, When: s.When,
			Launches: s.Launches,
		})
	})
	conn.Handle("unregister_command", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(raw, &p)
		// Don't let modules unregister someone else's commands.
		if existing, ok := m.cmds.Get(p.ID); ok && existing.Owner != moduleID {
			return nil, nil
		}
		m.cmds.Unregister(p.ID)
		return nil, nil
	})

	conn.Handle("request_secret", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Key string `json:"key"`
		}
		_ = json.Unmarshal(raw, &p)
		v, err := secrets.Get(moduleID, p.Key)
		if err != nil {
			return nil, err
		}
		return map[string]string{"value": v}, nil
	})

	conn.Handle("panel", func(_ context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			Key   string `json:"key"`
			Title string `json:"title"`
			Text  string `json:"text"`
			Remove bool  `json:"remove"`
		}
		_ = json.Unmarshal(raw, &p)
		if p.Remove {
			m.dispatchAsync(panelRemoveMsg{owner: moduleID, key: p.Key})
		} else {
			m.dispatchAsync(panelSetMsg{w: sidepanel.Widget{
				Owner: moduleID, Key: p.Key, Title: p.Title, Text: p.Text,
			}})
		}
		return nil, nil
	})
}

// panelSetMsg / panelRemoveMsg / moduleExitMsg are inbound RPC-derived
// messages routed back to Update for thread-safe Model mutation.
type panelSetMsg struct{ w sidepanel.Widget }
type panelRemoveMsg struct{ owner, key string }
type moduleExitMsg struct{ moduleID string }

func (m *Model) applyPanelSet(msg panelSetMsg)         { m.panel.SetWidget(msg.w) }
func (m *Model) applyPanelRemove(msg panelRemoveMsg)   { m.panel.RemoveWidget(msg.owner, msg.key) }
func (m *Model) applyModuleExit(msg moduleExitMsg) {
	m.panel.RemoveWidgetsOf(msg.moduleID)
}

// invokeCommand routes a palette/slash command activation to the right
// place: host commands go through applyHostAction; module commands are
// looked up in the registry and dispatched to the owning running module
// via an `invoke` RPC. The result of the RPC (open_tab / notify / output)
// will arrive asynchronously through the module's RPC handlers — we just
// fire-and-forget here.
func (m Model) invokeCommand(id string) (Model, tea.Cmd) {
	if strings.HasPrefix(id, "host.") {
		return m.applyHostAction(dispatchActionID(id))
	}
	cmd, ok := m.cmds.Get(id)
	if !ok {
		hlog.Warn("invoke: unknown command", "id", id)
		return m, nil
	}
	// Find the running module that owns this command in the active tab.
	if t, ok := m.tabs.Active(); ok && t.Session != nil &&
		t.Session.Module() == cmd.Owner {
		if proc := m.activeProcess(); proc != nil {
			go func(p *modules.Process, cid string) {
				_, err := p.Conn().Call(context.Background(), "invoke",
					map[string]any{"command_id": cid})
				if err != nil {
					hlog.With("module", cmd.Owner).Warn("invoke failed",
						"id", cid, "err", err.Error())
				}
			}(proc, id)
			return m, nil
		}
	}
	// Lazy-spawn: find the installed module by owner ID and start it in
	// the active tab. If the active tab is already bound to a different
	// module we open a fresh tab so we don't displace the user's
	// in-flight session.
	return m.lazySpawnForCommand(cmd, id)
}

// lazySpawnForCommand spawns the module that owns the given command and
// then invokes the command on it. Used when the palette / slash command
// activates a module that isn't running in any tab yet.
func (m Model) lazySpawnForCommand(cmd commands.Command, commandID string) (Model, tea.Cmd) {
	installs, err := installer.Scan()
	if err != nil {
		hlog.With("module", cmd.Owner).Warn("scan failed", "err", err.Error())
		return m, nil
	}
	var inst *installer.Installed
	for i := range installs {
		if installs[i].Manifest.Module.ID == cmd.Owner {
			inst = &installs[i]
			break
		}
	}
	if inst == nil {
		if t, ok := m.tabs.Active(); ok {
			m.out.Appendln(t.ID, m.styles.Muted.Render(
				"("+cmd.Owner+" is not installed; run `hackermode install "+cmd.Owner+"`)"))
		}
		return m, nil
	}

	// If the active tab is bound to host, transition it. Otherwise spawn
	// a new tab so we don't replace whatever the user is doing.
	t, ok := m.tabs.Active()
	if !ok || !t.Session.IsHost() {
		nt := m.tabs.New(inst.Manifest.Module.Name)
		m.out.SetActive(nt.ID)
		t = nt
	}
	newM, cmdTea := m.devRunFromInstall(t.ID, inst)
	_ = commandID // command invocation after spawn lands in Phase D when we add prompt_arg
	return newM, cmdTea
}

// devRunFromInstall starts a module from an installer.Installed entry.
// Mirrors devRunModule but takes a pre-parsed manifest from disk rather
// than re-reading from a user-supplied directory.
func (m Model) devRunFromInstall(tabID string, inst *installer.Installed) (Model, tea.Cmd) {
	// Look up the tab so we can poke its session.
	var t tabs.Tab
	for i := 0; i < m.tabs.Count(); i++ {
		// tabs.Tab is value-returned; we just need any matching one.
		if cur, ok := m.tabs.Active(); ok && cur.ID == tabID {
			t = cur
			break
		}
	}
	if t.Session == nil {
		return m, nil
	}

	if !t.Session.IsHost() {
		m.out.Appendln(t.ID, m.styles.Muted.Render(
			"(tab already bound; can't lazy-spawn here)"))
		return m, nil
	}
	t.Session.Transition(inst.Manifest.Module.ID)
	t.Session.SetTitle(inst.Manifest.Module.Name)
	m.out.Appendln(t.ID, m.styles.Muted.Render("spawning "+inst.Manifest.Module.ID+" from "+inst.Dir+"..."))

	cols, rows := m.mainAreaSize()
	ctx := context.Background()

	proc, info, err := m.manager.Start(ctx, modules.StartOpts{
		TabID:    t.ID,
		Manifest: inst.Manifest,
		Dir:      inst.Dir,
		Init: modules.InitParams{
			TabID:       t.ID,
			Mode:        inst.Manifest.Entry.Mode,
			Theme:       m.theme.Name,
			HostVersion: "0.0.0-dev",
			Width:       cols,
			Height:      rows,
		},
		PTYCols: uint16(cols),
		PTYRows: uint16(rows),
	})
	if err != nil {
		t.Session.SetState(modules.StateErrored)
		t.Session.Transition(modules.HostModuleID)
		m.out.Appendln(t.ID, m.styles.Muted.Render("module start failed: "+err.Error()))
		return m, nil
	}
	t.Session.SetState(modules.StateRunning)
	t.Session.SetPermissions(info.Capabilities)
	m.registerModuleCommands(inst.Manifest.Module.ID, info.Commands)
	m.installModuleRPCHandlers(proc, inst.Manifest.Module.ID)

	go func(ownerID string) {
		<-proc.Done()
		m.cmds.UnregisterOwner(ownerID)
		m.dispatchAsync(moduleExitMsg{moduleID: ownerID})
	}(inst.Manifest.Module.ID)

	switch inst.Manifest.Entry.Mode {
	case modules.ModeTUI:
		r := ptyrender.New(cols, rows)
		m.renderers[t.ID] = r
		go r.Pump(proc.Stdout())
		return m, tickRedraw()
	default:
		if m.runtime != nil && m.runtime.Send != nil {
			streambridge.Start(t.ID, proc.Stdout(), m.runtime.Send)
		}
	}
	return m, nil
}

// dispatchAsync is the thread-safe path from an inbound RPC goroutine back
// to the Bubble Tea event loop. Falls back to logging if no runtime is
// attached (host tests, headless contexts).
func (m *Model) dispatchAsync(msg any) {
	if m.runtime == nil || m.runtime.Send == nil {
		hlog.Warn("rpc message dropped (no runtime)", "msg", msg)
		return
	}
	m.runtime.Send(msg)
}

// --- inbound message types (handled in app.go Update) -----------------

type notifyMsg struct{ level, title, body string }
type setTitleMsg struct{ tabID, title string }
type setStatusMsg struct{ tabID, status string }

// applyNotify pushes a notification into the side-panel.
func (m *Model) applyNotify(msg notifyMsg) {
	m.panel.AddNotification(sidepanel.Notification{
		Level: msg.level,
		Title: msg.title,
		Body:  msg.body,
	})
}

// applySetTitle finds the matching tab and updates its session title.
func (m *Model) applySetTitle(msg setTitleMsg) {
	for i := 0; i < m.tabs.Count(); i++ {
		// We can't see the tab list directly here; rely on Active()/
		// Next()/Prev() being side-effect free in the renderer copy.
		// For Phase B, only the active tab is reachable via this path;
		// when a module is loaded into the active tab, its msg.TabID
		// matches.
		if t, ok := m.tabs.Active(); ok && t.ID == msg.tabID && t.Session != nil {
			t.Session.SetTitle(msg.title)
			return
		}
		m.tabs.Next()
	}
}

// applySetStatus surfaces the status text as a transient statusline
// message (right-aligned).
func (m *Model) applySetStatus(msg setStatusMsg) {
	// In Phase B we treat tabID as advisory: there is no per-tab
	// statusline state. Apply the text to the global message slot.
	m.status.SetMessage(msg.status)
}
