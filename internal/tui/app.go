// Package tui hosts the root Bubble Tea Model that owns the four-region
// layout: tablist (top), output + input (main), side panel (right),
// statusline (bottom). Phase A operates on mock tabs; module wiring comes
// in Phase B.
package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sectersion/hackermode/internal/commands"
	"github.com/sectersion/hackermode/internal/config"
	"github.com/sectersion/hackermode/internal/installer"
	hlog "github.com/sectersion/hackermode/internal/log"
	"github.com/sectersion/hackermode/internal/manifest"
	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/internal/paths"
	"github.com/sectersion/hackermode/internal/registry"
	"github.com/sectersion/hackermode/internal/tui/compose"
	"github.com/sectersion/hackermode/internal/tui/dispatch"
	"github.com/sectersion/hackermode/internal/tui/input"
	"github.com/sectersion/hackermode/internal/tui/keymap"
	"github.com/sectersion/hackermode/internal/tui/output"
	"github.com/sectersion/hackermode/internal/tui/marketplace"
	"github.com/sectersion/hackermode/internal/tui/palette"
	"github.com/sectersion/hackermode/internal/tui/ptyrender"
	"github.com/sectersion/hackermode/internal/tui/quitdialog"
	"github.com/sectersion/hackermode/internal/tui/sidepanel"
	"github.com/sectersion/hackermode/internal/tui/statusline"
	"github.com/sectersion/hackermode/internal/tui/streambridge"
	"github.com/sectersion/hackermode/internal/tui/tabs"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

// Layout sizes (rows / cols) reserved for chrome.
const (
	rowsTabBar       = 1
	rowsDivider      = 1
	rowsStatusBar    = 1
	rowsInput        = 1
	rowsInputDivider = 1
	minPanelChars    = 18
	minMainChars     = 30
	autoPanelHide    = 100 // term width below this hides the panel automatically
)

type focus int

const (
	focusInput focus = iota
	focusOutput
)

type Model struct {
	cfg    config.Config
	theme  theme.Theme
	styles theme.Styles
	keys   keymap.Map

	tabs       tabs.Model
	out        output.Model
	in         input.Model
	panel      sidepanel.Model
	status     statusline.Model
	palette    palette.Model
	dispatcher *dispatch.Dispatcher
	cmds       *commands.Registry
	manager    *modules.Manager
	runtime    *Runtime
	renderers  map[string]*ptyrender.Renderer // tabID → vt10x snapshot
	quit       quitdialog.Model
	market     marketplace.Model

	width, height int
	focus         focus
	ready         bool
}

// SetRuntime attaches a Runtime (typically tea.Program.Send capture) to
// the model. Must be called before background goroutines start needing
// to inject messages.
func (m *Model) SetRuntime(r *Runtime) { m.runtime = r }

// redrawMsg fires periodically while at least one tui-mode renderer is
// active. The handler simply triggers a re-render.
type redrawMsg struct{}

// tickRedraw schedules the next redraw frame (~25 fps).
func tickRedraw() tea.Cmd {
	return tea.Tick(40*time.Millisecond, func(time.Time) tea.Msg { return redrawMsg{} })
}

// New constructs the root model from config (and resolved keymap).
func New(cfg config.Config) Model {
	th := theme.Resolve(cfg.UI.Theme)
	st := theme.NewStyles(th)
	km := keymap.New(cfg.Keymap)

	m := Model{
		cfg:        cfg,
		theme:      th,
		styles:     st,
		keys:       km,
		tabs:       tabs.New(th, st),
		out:        output.New(st),
		in:         input.New(th, st),
		panel:      sidepanel.New(th, st, cfg.SidePanelEnabled()),
		status:     statusline.New(km, th, st),
		palette:    palette.New(th, st),
		dispatcher: dispatch.New(),
		cmds:       commands.NewRegistry(),
		manager:    modules.NewManager(),
		renderers:  map[string]*ptyrender.Renderer{},
		quit:       quitdialog.New(th, st),
		market:     marketplace.New(th, st),
		focus:      focusInput,
	}
	m.registerHostActions()
	m.registerHostCommands()
	m.discoverInstalledModules()
	if t, ok := m.tabs.Active(); ok {
		m.out.SetActive(t.ID)
	}
	hlog.Info("tui ready", "panel", cfg.SidePanelEnabled())
	return m
}

// discoverInstalledModules scans the on-disk module directory and projects
// each installed module's static commands into the command registry. This
// is what lights up the palette before any module is spawned — the bit
// that has been deferred since Phase B.
//
// Spawning itself happens lazily inside invokeCommand: the registry
// entry's Owner identifies the module, and we resolve the install path
// at invocation time.
func (m *Model) discoverInstalledModules() {
	installed, err := installer.Scan()
	if err != nil {
		hlog.Warn("module scan failed", "err", err.Error())
		return
	}
	for _, inst := range installed {
		m.registerModuleCommands(inst.Manifest.Module.ID, inst.Manifest.Commands)
		hlog.Info("module discovered",
			"id", inst.Manifest.Module.ID,
			"version", inst.Manifest.Module.Version,
			"commands", len(inst.Manifest.Commands))
	}
}

// registerHostActions wires every built-in action into the dispatcher and
// binds it to the configured key. Modules will call dispatcher.Register
// directly in Stage 5.
//
// We capture *Model into closures via a small pointer to the field set;
// the host's value-receiver Update returns the (possibly mutated) Model
// each turn, so handlers operate on whatever model owns this dispatcher.
//
// To keep things simple in the value-receiver world: handlers mutate via
// a sentinel returned in Result. Each Update call passes the current
// model pointer in via a temporary, dispatcher invokes the handler, then
// we return the model.
//
// For now the handlers are intentionally pure: they signal intent
// (action ID) via Result, and Update applies that intent to the model.
// This avoids closure-captures-pointer fragility.
func (m *Model) registerHostActions() {
	d := m.dispatcher

	// Actions are stateless markers: a handler just reports Consumed.
	// The real state mutation lives in Model.applyAction below.
	mark := func(id dispatch.ActionID) dispatch.Handler {
		return func(c dispatch.Context) dispatch.Result {
			_ = id
			return dispatch.Result{Consumed: true}
		}
	}

	for _, id := range hostActionIDs {
		d.Register(id, mark(id))
	}

	// Bind each known action to its configured key.
	for action, akey := range hostActionToKeymap {
		d.Bind(dispatch.ActionID(action), m.keys.Binding(akey))
	}
}

// hostActionIDs is the set of dispatcher actions the host owns.
var hostActionIDs = []dispatch.ActionID{
	"host.app.quit",
	"host.palette.show",
	"host.panel.toggle",
	"host.tab.new",
	"host.tab.close",
	"host.tab.next",
	"host.tab.prev",
	"host.focus.input",
	"host.help.show",
	"host.theme.show",
	"host.marketplace.show",
}

// hostActionToKeymap is the static mapping from dispatcher action ID to
// the keymap.* action name that produces its key binding. Modules will not
// touch this — they get their own bindings from their manifest.
var hostActionToKeymap = map[string]string{
	"host.app.quit":     keymap.Quit,
	"host.palette.show": keymap.CommandPalette,
	"host.panel.toggle": keymap.TogglePanel,
	"host.tab.new":      keymap.NewTab,
	"host.tab.close":    keymap.CloseTab,
	"host.tab.next":     keymap.NextTab,
	"host.tab.prev":     keymap.PrevTab,
	"host.focus.input":  keymap.FocusInput,
}

// registerHostCommands seeds the command registry with the built-in host
// entries. Stage 5 will add module-registered commands alongside these.
func (m *Model) registerHostCommands() {
	keyHint := func(action string) string {
		k, _ := m.keys.Help(action)
		return k
	}
	entries := []commands.Command{
		{ID: "host.tab.new", Owner: commands.HostOwner, Title: "New tab", Hint: keyHint(keymap.NewTab), Tags: []string{"tab", "open"}},
		{ID: "host.tab.close", Owner: commands.HostOwner, Title: "Close tab", Hint: keyHint(keymap.CloseTab), Tags: []string{"tab"}, When: "tab.count > 0"},
		{ID: "host.tab.next", Owner: commands.HostOwner, Title: "Next tab", Hint: keyHint(keymap.NextTab), Tags: []string{"tab"}, When: "tab.count > 1"},
		{ID: "host.tab.prev", Owner: commands.HostOwner, Title: "Previous tab", Hint: keyHint(keymap.PrevTab), Tags: []string{"tab"}, When: "tab.count > 1"},
		{ID: "host.panel.toggle", Owner: commands.HostOwner, Title: "Toggle side panel", Hint: keyHint(keymap.TogglePanel), Tags: []string{"panel", "sidebar"}},
		{ID: "host.theme.show", Owner: commands.HostOwner, Title: "Show theme name", Tags: []string{"theme", "color"}},
		{ID: "host.help.show", Owner: commands.HostOwner, Title: "Show help", Tags: []string{"help", "?"}},
		{ID: "host.marketplace.show", Owner: commands.HostOwner, Title: "Open marketplace", Tags: []string{"install", "browse", "market"}},
		{ID: "host.app.quit", Owner: commands.HostOwner, Title: "Quit hackermode", Hint: keyHint(keymap.Quit), Tags: []string{"exit", "close"}},
	}
	for _, c := range entries {
		if err := m.cmds.Register(c); err != nil {
			hlog.Error("host command register failed", "id", c.ID, "err", err.Error())
		}
	}
}

// paletteContext returns the variable map used by when-clause evaluation.
// Keeps the same set of keys mentioned in AGENTS.md so module manifests
// can rely on them.
func (m Model) paletteContext() commands.Context {
	module := commands.HostOwner
	if t, ok := m.tabs.Active(); ok && t.Session != nil {
		module = t.Session.Module()
	}
	panelVis := "false"
	if m.panel.Visible() {
		panelVis = "true"
	}
	return commands.Context{
		"tab.module":     module,
		"tab.count":      fmt.Sprintf("%d", m.tabs.Count()),
		"panel.visible":  panelVis,
		"network.online": "true", // host can't reliably detect; Phase E watcher will refine.
	}
}

// visiblePaletteCommands turns the registry's visible-for-context set
// into the palette.Command shape the overlay needs.
func (m Model) visiblePaletteCommands() []palette.Command {
	src := m.cmds.Visible(m.paletteContext())
	out := make([]palette.Command, 0, len(src))
	for _, c := range src {
		out = append(out, palette.Command{
			ID:    c.ID,
			Title: c.Title,
			Hint:  c.Hint,
			Tags:  c.Tags,
		})
	}
	return out
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.relayout()
		m.ready = true
		return m, nil

	case streambridge.ChunkMsg:
		m.out.Append(msg.TabID, string(msg.Data))
		return m, nil

	case streambridge.ClosedMsg:
		m.out.Appendln(msg.TabID, m.styles.Muted.Render("(module exited)"))
		return m, nil

	case redrawMsg:
		// Keep ticking as long as there's at least one tui-mode renderer.
		if len(m.renderers) > 0 {
			return m, tickRedraw()
		}
		return m, nil

	case AutoRunMsg:
		// Triggered by `hackermode dev run <dir>` after the program
		// starts. Route through the same code path as the slash command.
		return m.devRunModule(msg.Dir)

	case notifyMsg:
		m.applyNotify(msg)
		return m, nil

	case setTitleMsg:
		m.applySetTitle(msg)
		return m, nil

	case setStatusMsg:
		m.applySetStatus(msg)
		return m, nil

	case panelSetMsg:
		m.applyPanelSet(msg)
		return m, nil

	case panelRemoveMsg:
		m.applyPanelRemove(msg)
		return m, nil

	case moduleExitMsg:
		m.applyModuleExit(msg)
		return m, nil

	case marketplace.IndexLoadedMsg:
		// Forward async registry load into the marketplace model.
		cmd, _ := m.market.Update(msg)
		return m, cmd

	case tea.MouseMsg:
		if m.palette.Open() {
			return m, nil
		}
		return m.handleMouse(msg)

	case tea.KeyMsg:
		// Quit dialog takes precedence over everything else when open —
		// ctrl+c again confirms quit, anything else navigates the modal.
		if m.quit.Open() {
			res := m.quit.Update(msg)
			if res.Quit {
				return m, tea.Quit
			}
			return m, nil
		}

		// Intercept the quit binding before the dispatcher routes it.
		// First press opens the confirm dialog; the second press lands
		// in the branch above and quits.
		if k := m.keys.Binding(keymap.Quit); key.Matches(msg, k) {
			m.quit.SetSize(m.width, m.height)
			m.quit.Show()
			// Also close the palette if open so the modal isn't double-stacked.
			if m.palette.Open() {
				m.palette.Hide()
			}
			return m, nil
		}

		// Palette consumes input while open. (ctrl+c is handled above.)
		if m.palette.Open() {
			res, _, cmd := m.palette.Update(msg)
			if !res.Canceled && res.Command.ID != "" {
				newM, actCmd := m.invokeCommand(res.Command.ID)
				return newM, tea.Batch(cmd, actCmd)
			}
			return m, cmd
		}

		// Global keys: ask the dispatcher for a matching action.
		if _, id := m.dispatcher.HandleKey(msg); id != "" {
			return m.applyHostAction(id)
		}

		// Marketplace tab? Route the key into the marketplace model.
		if m.market.Open() && m.activeSessionIs(marketplace.OwnerID) {
			cmd, outMsg := m.market.Update(msg)
			if req, ok := outMsg.(marketplace.InstallRequestMsg); ok {
				newM, actCmd := m.installFromMarketplace(req.ID)
				return newM, tea.Batch(cmd, actCmd)
			}
			return m, cmd
		}

		// TUI-mode tab? Forward the keystroke to the PTY master and stop.
		if proc := m.activeProcess(); proc != nil && proc.IsPTY() {
			return m, forwardKeyToPTY(proc, msg)
		}

		// Focus-specific keys.
		switch m.focus {
		case focusInput:
			return m.handleInputKey(msg)
		case focusOutput:
			return m.handleOutputKey(msg)
		}
	}

	// Forward other messages to the input.
	cmds = append(cmds, m.in.Update(msg))
	return m, tea.Batch(cmds...)
}

func (m Model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k := m.keys.Binding(keymap.Submit); key.Matches(msg, k) {
		raw := m.in.Submit()
		text := strings.TrimSpace(raw)
		if text == "" {
			return m, nil
		}
		// If the active session is bound to a module, route the line
		// through to the module's stdin (no slash-command interpretation).
		// Slash commands still work *only* when the active tab is a host
		// session.
		if t, ok := m.tabs.Active(); ok && t.Session != nil && !t.Session.IsHost() {
			if proc := m.manager.Get(t.Session.Module(), t.ID); proc != nil {
				if _, err := proc.Stdin().Write([]byte(raw + "\n")); err != nil {
					m.out.Appendln(t.ID, m.styles.Muted.Render("(write to module failed: "+err.Error()+")"))
				}
				return m, nil
			}
		}
		return m.dispatchCommand(text)
	}
	if k := m.keys.Binding(keymap.Complete); key.Matches(msg, k) {
		if suggs := m.in.Complete(); len(suggs) > 1 {
			t, _ := m.tabs.Active()
			m.out.Appendln(t.ID, m.styles.Muted.Render("suggestions: "+strings.Join(suggs, "  ")))
		}
		return m, nil
	}
	if k := m.keys.Binding(keymap.HistoryPrev); key.Matches(msg, k) {
		m.in.HistoryPrev()
		return m, nil
	}
	if k := m.keys.Binding(keymap.HistoryNext); key.Matches(msg, k) {
		m.in.HistoryNext()
		return m, nil
	}
	if k := m.keys.Binding(keymap.ScrollUp); key.Matches(msg, k) {
		m.out.ScrollUp(3)
		return m, nil
	}
	if k := m.keys.Binding(keymap.ScrollDown); key.Matches(msg, k) {
		m.out.ScrollDown(3)
		return m, nil
	}

	cmd := m.in.Update(msg)
	return m, cmd
}

func (m Model) handleOutputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k := m.keys.Binding(keymap.ScrollUp); key.Matches(msg, k) {
		m.out.ScrollUp(5)
	}
	if k := m.keys.Binding(keymap.ScrollDown); key.Matches(msg, k) {
		m.out.ScrollDown(5)
	}
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Tab strip clicks (top row).
	if msg.Y == 0 && msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
		if i := m.tabs.HitTest(msg.X); i >= 0 {
			m.tabs.SetActive(i)
			if t, ok := m.tabs.Active(); ok {
				m.out.SetActive(t.ID)
			}
			return m, nil
		}
	}
	// Wheel in main area scrolls output.
	if msg.Button == tea.MouseButtonWheelUp {
		m.out.ScrollUp(3)
	}
	if msg.Button == tea.MouseButtonWheelDown {
		m.out.ScrollDown(3)
	}
	return m, nil
}

// dispatchCommand routes a typed host command from the input box to an
// action ID. Anything else is echoed. The leading `:` colon prefix is
// optional — `help` and `:help` are equivalent.
func (m Model) dispatchCommand(text string) (Model, tea.Cmd) {
	t, ok := m.tabs.Active()
	if !ok {
		return m, nil
	}
	m.out.Appendln(t.ID, m.theme.Gradient("❯")+" "+text)

	// Accept either `cmd` or `:cmd`. Strip the optional colon and route.
	cmd := strings.TrimPrefix(text, ":")

	switch {
	case cmd == "help":
		return m.applyHostAction("host.help.show")
	case cmd == "quit":
		return m.applyHostAction("host.app.quit")
	case cmd == "new":
		return m.applyHostAction("host.tab.new")
	case cmd == "close":
		return m.applyHostAction("host.tab.close")
	case cmd == "panel":
		return m.applyHostAction("host.panel.toggle")
	case cmd == "theme":
		return m.applyHostAction("host.theme.show")
	case strings.HasPrefix(cmd, "dev run "):
		dir := strings.TrimSpace(strings.TrimPrefix(cmd, "dev run "))
		return m.devRunModule(dir)
	default:
		m.out.Appendln(t.ID, m.styles.Muted.Render("(unknown command — try `help`)"))
		return m, nil
	}
}

// devRunModule loads a manifest from dir and spawns its module, binding
// the active tab's session to it. This is the developer escape hatch
// while the marketplace doesn't exist yet — module authors point
// :dev run at their source directory.
func (m Model) devRunModule(dir string) (Model, tea.Cmd) {
	t, ok := m.tabs.Active()
	if !ok {
		return m, nil
	}
	manifest, err := modules.ParseDir(dir)
	if err != nil {
		m.out.Appendln(t.ID, m.styles.Muted.Render("dev run: "+err.Error()))
		return m, nil
	}
	if errs := manifest.Validate(); len(errs) > 0 {
		m.out.Appendln(t.ID, m.styles.Muted.Render("manifest invalid:"))
		for _, e := range errs {
			m.out.Appendln(t.ID, m.styles.Muted.Render("  - "+e.Error()))
		}
		return m, nil
	}

	if !t.Session.IsHost() {
		m.out.Appendln(t.ID, m.styles.Muted.Render("(this tab is already bound to "+t.Session.Module()+"; open a new tab first)"))
		return m, nil
	}
	t.Session.Transition(manifest.Module.ID)
	t.Session.SetTitle(manifest.Module.Name)

	m.out.Appendln(t.ID, m.styles.Muted.Render("spawning "+manifest.Module.ID+" from "+dir+"..."))

	// Spawn synchronously. We use a long-lived background context here —
	// using a WithTimeout + defer cancel() would kill the module the
	// instant devRunModule returns (CommandContext propagates ctx death
	// to the child). The handshake itself blocks inside Start and has
	// its own internal timeout via the RPC layer.
	ctx := context.Background()

	// For tui-mode modules, compute the PTY size from the host's main area.
	cols, rows := m.mainAreaSize()

	proc, info, err := m.manager.Start(ctx, modules.StartOpts{
		TabID:    t.ID,
		Manifest: manifest,
		Dir:      dir,
		Init: modules.InitParams{
			TabID:       t.ID,
			Mode:        manifest.Entry.Mode,
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

	// Register any manifest-declared static commands the module just
	// reported during handshake. ModuleInfo carries them verbatim so
	// they end up in the palette immediately (visible across tabs by
	// default — when-clauses on the manifest entries narrow visibility).
	m.registerModuleCommands(manifest.Module.ID, info.Commands)

	// Install host-side RPC handlers on this process's control channel.
	m.installModuleRPCHandlers(proc, manifest.Module.ID)

	// On module exit, drop its registered commands + widgets.
	go func(ownerID string) {
		<-proc.Done()
		m.cmds.UnregisterOwner(ownerID)
		m.dispatchAsync(moduleExitMsg{moduleID: ownerID})
	}(manifest.Module.ID)

	switch manifest.Entry.Mode {
	case modules.ModeTUI:
		// Run a vt10x emulator over the PTY master; the host samples it
		// for each render frame. Drive a periodic redraw via tea.Tick.
		r := ptyrender.New(cols, rows)
		m.renderers[t.ID] = r
		go r.Pump(proc.Stdout())
		return m, tickRedraw()
	default:
		// Stream mode: pump bytes into the per-tab output buffer.
		if m.runtime != nil && m.runtime.Send != nil {
			streambridge.Start(t.ID, proc.Stdout(), m.runtime.Send)
		} else {
			hlog.Warn("module spawned but no runtime sender — output suppressed", "module", manifest.Module.ID)
		}
	}
	return m, nil
}

// activeSessionIs reports whether the focused tab's session is bound to
// the given module ID. Used to dispatch built-in views like the
// marketplace.
func (m Model) activeSessionIs(moduleID string) bool {
	t, ok := m.tabs.Active()
	if !ok || t.Session == nil {
		return false
	}
	return t.Session.Module() == moduleID
}

// activeRenderer returns the TUI-mode renderer for the focused tab, or
// nil if none.
func (m Model) activeRenderer() *ptyrender.Renderer {
	t, ok := m.tabs.Active()
	if !ok {
		return nil
	}
	return m.renderers[t.ID]
}

// forwardKeyToPTY writes the bytes for a tea.KeyMsg to the module's PTY
// master. Returns a tea.Cmd (always nil) for ergonomic Update returns.
//
// We translate a small set of common keys to their canonical byte
// sequences. Anything not explicitly mapped falls back to the rune list.
// More exhaustive translation can come later.
func forwardKeyToPTY(proc *modules.Process, msg tea.KeyMsg) tea.Cmd {
	var data []byte
	switch msg.Type {
	case tea.KeyRunes:
		data = []byte(string(msg.Runes))
	case tea.KeyEnter:
		data = []byte{'\r'}
	case tea.KeySpace:
		data = []byte{' '}
	case tea.KeyTab:
		data = []byte{'\t'}
	case tea.KeyBackspace:
		data = []byte{0x7f}
	case tea.KeyEsc:
		data = []byte{0x1b}
	case tea.KeyUp:
		data = []byte("\x1b[A")
	case tea.KeyDown:
		data = []byte("\x1b[B")
	case tea.KeyRight:
		data = []byte("\x1b[C")
	case tea.KeyLeft:
		data = []byte("\x1b[D")
	case tea.KeyHome:
		data = []byte("\x1b[H")
	case tea.KeyEnd:
		data = []byte("\x1b[F")
	case tea.KeyDelete:
		data = []byte("\x1b[3~")
	case tea.KeyPgUp:
		data = []byte("\x1b[5~")
	case tea.KeyPgDown:
		data = []byte("\x1b[6~")
	default:
		// Best-effort fallback — use the string representation.
		s := msg.String()
		if len(s) == 1 {
			data = []byte(s)
		}
	}
	if len(data) > 0 {
		_, _ = proc.Stdin().Write(data)
	}
	return nil
}

// activeProcess returns the running module process for the focused tab,
// or nil if the tab is host-bound.
func (m Model) activeProcess() *modules.Process {
	t, ok := m.tabs.Active()
	if !ok || t.Session == nil || t.Session.IsHost() {
		return nil
	}
	return m.manager.Get(t.Session.Module(), t.ID)
}

// mainAreaSize returns the (cols, rows) of the host's main area, with
// safe fallbacks if the window size hasn't been reported yet.
func (m Model) mainAreaSize() (int, int) {
	cols := m.width
	if m.panel.Visible() && m.panel.Width() > 0 {
		cols -= m.panel.Width()
	}
	if cols < 10 {
		cols = 80
	}
	rows := m.height - rowsTabBar - rowsDivider - rowsInputDivider - rowsInput - rowsStatusBar
	if rows < 3 {
		rows = 24
	}
	return cols, rows
}

// applyHostAction performs a registered host action and returns the
// updated model + optional tea.Cmd. Both the dispatcher's key path and
// the palette result handler funnel through here, so behavior is
// consistent.
func (m Model) applyHostAction(id dispatch.ActionID) (Model, tea.Cmd) {
	switch id {
	case "host.app.quit":
		return m, tea.Quit

	case "host.palette.show":
		m.palette.SetCommands(m.visiblePaletteCommands())
		m.palette.SetSize(m.width, m.height)
		m.palette.Show()
		return m, nil

	case "host.panel.toggle":
		m.panel.Toggle()
		m.relayout()
		return m, nil

	case "host.tab.new":
		nt := m.tabs.New(fmt.Sprintf("new %d", m.tabs.Count()+1))
		m.out.SetActive(nt.ID)
		return m, nil

	case "host.tab.close":
		old, _ := m.tabs.Active()
		if m.tabs.Close() {
			m.out.Forget(old.ID)
			// Tear down any module + renderer attached to the closed tab.
			if old.Session != nil && !old.Session.IsHost() {
				_ = m.manager.Stop(context.Background(), old.Session.Module(), old.ID, 500*time.Millisecond)
			}
			if r, ok := m.renderers[old.ID]; ok {
				r.Close()
				delete(m.renderers, old.ID)
			}
			if t, ok := m.tabs.Active(); ok {
				m.out.SetActive(t.ID)
			}
		}
		return m, nil

	case "host.tab.next":
		m.tabs.Next()
		if t, ok := m.tabs.Active(); ok {
			m.out.SetActive(t.ID)
		}
		return m, nil

	case "host.tab.prev":
		m.tabs.Prev()
		if t, ok := m.tabs.Active(); ok {
			m.out.SetActive(t.ID)
		}
		return m, nil

	case "host.focus.input":
		m.focus = focusInput
		m.in.Focus()
		return m, nil

	case "host.theme.show":
		if t, ok := m.tabs.Active(); ok {
			m.out.Appendln(t.ID, "theme: "+m.theme.Gradient(m.theme.Name))
		}
		return m, nil

	case "host.help.show":
		t, ok := m.tabs.Active()
		if !ok {
			return m, nil
		}
		g := m.theme.Gradient
		m.out.Appendln(t.ID, "host commands:")
		m.out.Appendln(t.ID, "  "+g("help")+"    show this")
		m.out.Appendln(t.ID, "  "+g("quit")+"    exit")
		m.out.Appendln(t.ID, "  "+g("new")+"     new tab")
		m.out.Appendln(t.ID, "  "+g("close")+"   close current tab")
		m.out.Appendln(t.ID, "  "+g("panel")+"   toggle side panel")
		m.out.Appendln(t.ID, "  "+g("theme")+"   show theme name")
		m.out.Appendln(t.ID, "  "+g("dev run <dir>")+" run a module from a directory")
		m.out.Appendln(t.ID, m.styles.Muted.Render("(ctrl+p opens the command palette)"))
		return m, nil

	case "host.marketplace.show":
		return m.openMarketplace()
	}

	hlog.Warn("unknown host action", "id", string(id))
	return m, nil
}

// installFromMarketplace runs the installer for the given module ID
// against the active manifest. If no manifest exists in any scope (the
// most common state for a fresh install), we synthesize a minimal one
// in the user's config dir so the install has somewhere to write.
//
// Progress goes into the active tab's output buffer; once the call
// returns the marketplace shows a one-line confirmation in the panel
// via sidepanel.AddNotification.
func (m Model) installFromMarketplace(id string) (Model, tea.Cmd) {
	t, ok := m.tabs.Active()
	if !ok {
		return m, nil
	}
	mani, _, err := manifest.Load(manifest.LocateOpts{Profile: m.cfg.Hackermode.DefaultProfile})
	if err == manifest.ErrNoManifest {
		// Create a system manifest as the implicit default.
		sysPath := filepath.Join(paths.ConfigDir(), manifest.FileName)
		_ = os.MkdirAll(filepath.Dir(sysPath), 0o755)
		_ = os.WriteFile(sysPath, []byte(`[hackermode]
version = "0.1"

[modules]
`), 0o644)
		mani, err = manifest.ParseFile(sysPath)
	}
	if err != nil {
		m.applyNotify(notifyMsg{level: "error", title: "install failed", body: err.Error()})
		return m, nil
	}
	// Add the dep (defaulting to >=0.0.0) and re-parse.
	if err := addDependencyForInstall(mani, id); err != nil {
		m.applyNotify(notifyMsg{level: "error", title: "edit manifest failed", body: err.Error()})
		return m, nil
	}
	reg := m.openRegistry()
	if reg == nil {
		m.applyNotify(notifyMsg{level: "error", title: "no registry configured"})
		return m, nil
	}
	out := &installSink{model: &m, tabID: t.ID}
	_, err = installer.Install(mani, reg, installer.Opts{Out: out})
	if err != nil {
		m.applyNotify(notifyMsg{level: "error", title: "install " + id, body: err.Error()})
		return m, nil
	}
	m.applyNotify(notifyMsg{level: "info", title: "installed", body: id})
	// Refresh the command registry so the new module's static commands
	// show up in the palette immediately.
	m.discoverInstalledModules()
	return m, nil
}

// installSink relays installer progress lines into the marketplace tab's
// output buffer so the user sees what's happening.
type installSink struct {
	model *Model
	tabID string
}

func (s *installSink) Write(p []byte) (int, error) {
	s.model.out.Append(s.tabID, string(p))
	return len(p), nil
}

// addDependencyForInstall is a stripped-down manifest editor reused by
// the marketplace install path. We re-implement the line-edit here
// rather than depending on cmd/install.go (which is its own package and
// would create a circular import).
func addDependencyForInstall(m *manifest.Manifest, id string) error {
	if m.Path == "" {
		return fmt.Errorf("manifest has no on-disk path")
	}
	data, err := os.ReadFile(m.Path)
	if err != nil {
		return err
	}
	if hasModuleLine(string(data), id) {
		// Already declared — nothing to do.
		return nil
	}
	entry := fmt.Sprintf(`"%s" = ">=0.0.0"`, id)
	updated := injectUnderModulesHeader(string(data), entry)
	if err := os.WriteFile(m.Path, []byte(updated), 0o644); err != nil {
		return err
	}
	fresh, err := manifest.ParseFile(m.Path)
	if err != nil {
		return err
	}
	*m = *fresh
	return nil
}

func hasModuleLine(src, id string) bool {
	needle := `"` + id + `"`
	for _, line := range strings.Split(src, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, needle) && strings.Contains(trim, "=") {
			return true
		}
	}
	return false
}

func injectUnderModulesHeader(src, entry string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "[modules]" {
			lines = append(lines[:i+1], append([]string{entry}, lines[i+1:]...)...)
			return strings.Join(lines, "\n")
		}
	}
	return strings.TrimRight(src, "\n") + "\n\n[modules]\n" + entry + "\n"
}

// openMarketplace transitions (or creates) a tab bound to the synthetic
// host.marketplace owner, sizes the marketplace model to the tab's main
// area, and kicks off the registry index load.
func (m Model) openMarketplace() (Model, tea.Cmd) {
	t, ok := m.tabs.Active()
	if !ok || !t.Session.IsHost() {
		nt := m.tabs.New("marketplace")
		m.out.SetActive(nt.ID)
		t = nt
	}
	t.Session.Transition(marketplace.OwnerID)
	t.Session.SetTitle("marketplace")

	cols, rows := m.mainAreaSize()
	m.market.SetSize(cols, rows+rowsInputDivider+rowsInput)
	regClient := m.openRegistry()
	cmd := m.market.Show(regClient)
	// Tick redraws so the marketplace re-renders smoothly on async load.
	return m, tea.Batch(cmd, tickRedraw())
}

// openRegistry resolves the configured registry from local config. The
// marketplace tolerates a nil client (it just shows an error in the
// loading state).
func (m Model) openRegistry() registry.Client {
	url := m.cfg.Marketplace.Registry
	if url == "" {
		return nil
	}
	if strings.HasPrefix(url, "file://") {
		return registry.NewFS(strings.TrimPrefix(url, "file://"))
	}
	if strings.HasPrefix(url, "/") {
		return registry.NewFS(url)
	}
	return nil
}

func (m *Model) relayout() {
	if m.width <= 0 || m.height <= 0 {
		return
	}

	// Vertical budget: tabbar · divider · main · divider · input · statusbar
	mainH := m.height - rowsTabBar - rowsDivider - rowsInputDivider - rowsInput - rowsStatusBar
	if mainH < 1 {
		mainH = 1
	}
	outputH := mainH
	if outputH < 1 {
		outputH = 1
	}

	panelW := 0
	if m.panel.Visible() && m.width >= autoPanelHide {
		panelW = (m.width * m.cfg.UI.SidePanelWidth) / 100
		if panelW < minPanelChars {
			panelW = minPanelChars
		}
		if m.width-panelW < minMainChars {
			panelW = 0
		}
	}
	mainW := m.width - panelW

	// Panel sits beside output + input + the input divider.
	panelH := outputH + rowsInputDivider + rowsInput

	m.tabs.SetWidth(m.width)
	m.out.SetSize(mainW, outputH)
	m.in.SetWidth(mainW)
	m.panel.SetSize(panelW, panelH)
	m.status.SetWidth(m.width)
	m.palette.SetSize(m.width, m.height)
	m.quit.SetSize(m.width, m.height)

	// Propagate the new size to every TUI-mode renderer. The PTY for the
	// owning process is resized at the same time so the module's
	// SIGWINCH wakes up Bubble Tea.
	tuiRows := outputH + rowsInputDivider + rowsInput
	for tabID, r := range m.renderers {
		r.Resize(mainW, tuiRows)
		// Resolve the module owning the tab via the active-tab API: we
		// don't have direct tabs-by-id lookup, so for resize purposes we
		// walk the manager. The manager keys on (module,tabID) so we just
		// iterate everything and resize matching processes.
		for _, proc := range m.manager.All() {
			// We can't see the manager's tab→key mapping, so resize
			// every process to the same dimensions — they all share the
			// host's main area, so that's correct.
			_ = proc.ResizePTY(uint16(mainW), uint16(tuiRows))
			_ = tabID
		}
	}
}

func (m Model) View() string {
	if !m.ready {
		return ""
	}

	// Top tablist divider spans full width.
	topDivider := m.theme.Divider(m.width)

	// Main-area inner divider sits between output and input on the main side
	// only (so the side panel's border continues uninterrupted).
	mainW := m.width
	if m.panel.Visible() && m.panel.Width() > 0 {
		mainW = m.width - m.panel.Width()
	}
	inputDivider := m.theme.Divider(mainW)

	var mainStack string
	switch {
	case m.activeSessionIs(marketplace.OwnerID):
		// Marketplace tab — host built-in UI takes the main area.
		mainStack = m.market.View()
	case m.activeRenderer() != nil:
		// TUI-mode module: vt10x snapshot owns the area. We omit the
		// input box — the module owns keyboard input itself.
		mainStack = m.activeRenderer().Snapshot()
	default:
		mainStack = lipgloss.JoinVertical(lipgloss.Left,
			m.out.View(),
			inputDivider,
			m.in.View(),
		)
	}
	panel := m.panel.View()

	var body string
	if panel != "" {
		body = lipgloss.JoinHorizontal(lipgloss.Top, mainStack, panel)
	} else {
		body = mainStack
	}

	statusFocus := statusline.FocusInput
	if m.focus == focusOutput {
		statusFocus = statusline.FocusOutput
	}
	statusModel := m.status
	statusModel.SetFocus(statusFocus)
	status := statusModel.View()

	base := lipgloss.JoinVertical(lipgloss.Left,
		m.tabs.View(),
		topDivider,
		body,
		status,
	)

	// Compose overlays bottom-up: palette first, then quit dialog (so the
	// dialog wins focus precedence visually as well as in the event
	// dispatch above).
	out := base
	if m.palette.Open() {
		out = compose.Overlay(out, m.palette.View())
	}
	if m.quit.Open() {
		out = compose.Overlay(out, m.quit.View())
	}
	return out
}
