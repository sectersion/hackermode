// Package marketplace is the host's built-in marketplace UI — the "first
// tab" users see on a fresh install.
//
// It is intentionally a small Bubble-Tea-flavored Model rather than a
// real bubbletea program: the host owns the tab chrome, so we only need
// to render the main area. The host routes keys here whenever the active
// tab's session module equals the synthetic owner constant
// `host.marketplace`.
//
// Behavior:
//
//   - On open: fetch the registry index. Show a list (newest-first by
//     default). The right-side panel of the list shows the focused
//     module's description and version.
//   - `j`/`k` or arrows navigate.
//   - `/` opens a search field; everything filters as you type.
//   - `enter` invokes `host.install`, which runs the same install
//     flow as the CLI — the actual install logic lives in the
//     installer package, this is just UI.
//   - `i` is an alternative keybind for install (mirrors vim-style
//     bindings users may already have muscle memory for).
//
// We do NOT block on the registry call: a `tea.Cmd` fires the index
// fetch and returns the result asynchronously so the rest of the host
// stays responsive.
package marketplace

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sectersion/hackermode/internal/registry"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

// OwnerID is the synthetic module ID used for sessions bound to the
// marketplace UI. Mirrors modules.HostModuleID's namespacing convention.
const OwnerID = "host.marketplace"

// InstallRequestMsg is fired when the user picks a module to install.
// The host model handles this by calling the installer and surfacing
// progress.
type InstallRequestMsg struct {
	ID string
}

// IndexLoadedMsg lands when the async registry fetch returns. Exported
// so the host's Update can forward it back into the marketplace model.
type IndexLoadedMsg struct {
	Idx *registry.Index
	Err error
}

// Model is the marketplace UI.
type Model struct {
	open     bool
	width    int
	height   int
	theme    theme.Theme
	styles   theme.Styles

	idx      *registry.Index
	idxErr   error
	loading  bool
	cursor   int

	searching bool
	search    textinput.Model
}

// New returns a closed marketplace model.
func New(th theme.Theme, st theme.Styles) Model {
	ti := textinput.New()
	ti.Placeholder = "search modules"
	ti.Prompt = ""
	return Model{
		theme:  th,
		styles: st,
		search: ti,
	}
}

// SetSize lets the host resize the marketplace to its main-area dimensions.
func (m *Model) SetSize(w, h int) { m.width = w; m.height = h }

// Open reports whether the marketplace is currently rendering.
func (m Model) Open() bool { return m.open }

// Show opens the marketplace and fires the async index load. The returned
// tea.Cmd should be batched with the model into Update's return.
func (m *Model) Show(client registry.Client) tea.Cmd {
	m.open = true
	m.loading = true
	m.idx = nil
	m.idxErr = nil
	m.cursor = 0
	return loadIndexCmd(client)
}

// Hide closes the marketplace.
func (m *Model) Hide() { m.open = false; m.searching = false; m.search.Blur(); m.search.SetValue("") }

func loadIndexCmd(client registry.Client) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return IndexLoadedMsg{Err: errors.New("no registry configured")}
		}
		idx, err := client.Index()
		return IndexLoadedMsg{Idx: idx, Err: err}
	}
}

// Update consumes a tea.Msg. The second return is the message the host
// should react to (currently only InstallRequestMsg).
func (m *Model) Update(msg tea.Msg) (tea.Cmd, tea.Msg) {
	if !m.open {
		return nil, nil
	}
	switch v := msg.(type) {
	case IndexLoadedMsg:
		m.loading = false
		m.idx = v.Idx
		m.idxErr = v.Err
		return nil, nil

	case tea.KeyMsg:
		return m.handleKey(v)
	}
	return nil, nil
}

func (m *Model) handleKey(k tea.KeyMsg) (tea.Cmd, tea.Msg) {
	// Search input has focus — only collect input + esc.
	if m.searching {
		switch k.Type {
		case tea.KeyEsc:
			m.searching = false
			m.search.Blur()
			m.search.SetValue("")
		case tea.KeyEnter:
			m.searching = false
			m.search.Blur()
		default:
			var cmd tea.Cmd
			m.search, cmd = m.search.Update(k)
			m.cursor = 0
			return cmd, nil
		}
		return nil, nil
	}

	switch k.Type {
	case tea.KeyEsc:
		m.Hide()
		return nil, nil
	case tea.KeyUp:
		if m.cursor > 0 {
			m.cursor--
		}
		return nil, nil
	case tea.KeyDown:
		if m.cursor < len(m.filtered())-1 {
			m.cursor++
		}
		return nil, nil
	case tea.KeyEnter:
		if entry, ok := m.selected(); ok {
			return nil, InstallRequestMsg{ID: entry.ID}
		}
		return nil, nil
	case tea.KeyRunes:
		switch string(k.Runes) {
		case "j":
			if m.cursor < len(m.filtered())-1 {
				m.cursor++
			}
		case "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "/":
			m.searching = true
			m.search.Focus()
		case "i":
			if entry, ok := m.selected(); ok {
				return nil, InstallRequestMsg{ID: entry.ID}
			}
		}
	}
	return nil, nil
}

func (m Model) selected() (registry.IndexEntry, bool) {
	list := m.filtered()
	if m.cursor < 0 || m.cursor >= len(list) {
		return registry.IndexEntry{}, false
	}
	return list[m.cursor], true
}

// filtered returns the visible modules after applying the search query.
func (m Model) filtered() []registry.IndexEntry {
	if m.idx == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(m.search.Value()))
	if q == "" {
		return m.idx.Modules
	}
	out := make([]registry.IndexEntry, 0, len(m.idx.Modules))
	for _, e := range m.idx.Modules {
		hay := strings.ToLower(e.ID + " " + e.Name + " " + e.Description)
		if strings.Contains(hay, q) {
			out = append(out, e)
		}
	}
	return out
}

// View renders the marketplace into the host's main area.
func (m Model) View() string {
	if !m.open || m.width <= 0 || m.height <= 0 {
		return ""
	}
	header := m.theme.TitleRule("marketplace", m.width)

	var body string
	switch {
	case m.loading:
		body = m.styles.Muted.Render("  loading registry…")
	case m.idxErr != nil:
		body = m.styles.Muted.Render("  error: " + m.idxErr.Error())
	default:
		body = m.renderList()
	}

	footer := m.renderFooter()
	stacked := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		MaxHeight(m.height).
		Render(stacked)
}

func (m Model) renderList() string {
	list := m.filtered()
	if len(list) == 0 {
		return m.styles.Muted.Render("  no modules in registry")
	}
	rows := make([]string, 0, len(list))
	visibleH := m.height - 4 // header + footer + slack
	if visibleH < 1 {
		visibleH = 1
	}
	start := 0
	if m.cursor >= visibleH {
		start = m.cursor - visibleH + 1
	}
	end := start + visibleH
	if end > len(list) {
		end = len(list)
	}
	for i := start; i < end; i++ {
		rows = append(rows, m.renderRow(list[i], i == m.cursor))
	}
	return strings.Join(rows, "\n")
}

func (m Model) renderRow(e registry.IndexEntry, selected bool) string {
	idCol := e.ID
	verCol := e.Latest
	descCol := e.Description
	if selected {
		marker := m.theme.Gradient("▌ ")
		titleStyled := m.theme.Gradient(idCol)
		verStyled := lipgloss.NewStyle().Foreground(m.theme.Fg).Render(verCol)
		descStyled := lipgloss.NewStyle().Foreground(m.theme.FgMuted).Render(descCol)
		return fmt.Sprintf("%s%s %s — %s", marker, titleStyled, verStyled, descStyled)
	}
	return fmt.Sprintf("  %s %s — %s",
		lipgloss.NewStyle().Foreground(m.theme.Fg).Render(idCol),
		lipgloss.NewStyle().Foreground(m.theme.FgMuted).Render(verCol),
		lipgloss.NewStyle().Foreground(m.theme.FgSubtle).Render(descCol),
	)
}

func (m Model) renderFooter() string {
	if m.searching {
		prompt := m.theme.Gradient("/ ")
		return prompt + m.search.View()
	}
	keyStyle := lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true)
	muted := m.styles.Muted
	hints := []string{
		keyStyle.Render("↑/↓") + muted.Render(" navigate"),
		keyStyle.Render("enter") + muted.Render(" install"),
		keyStyle.Render("/") + muted.Render(" search"),
		keyStyle.Render("esc") + muted.Render(" close"),
	}
	return strings.Join(hints, "  ·  ")
}
