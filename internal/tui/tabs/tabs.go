// Package tabs renders the top tab strip and owns tab order / active
// state. Each tab embeds a *modules.Session — the binding to whatever
// is currently driving the tab (host, or a module after a transition).
//
// In Phase A tabs were inert {ID, Title} structs. Stage 2 of Phase B
// elevates them to "a tab is a session-bearing UI element": creating a
// tab creates a host session; module commands transition that session
// in place; launcher commands ask for a brand-new tab + session.
package tabs

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/sectersion/hackermode/internal/modules"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

// Tab is a tab strip entry. Title is derived from the underlying session;
// see Tab.Title.
type Tab struct {
	ID      string
	Session *modules.Session
}

// Title returns the current tab title (delegates to the session).
func (t Tab) Title() string {
	if t.Session == nil {
		return ""
	}
	return t.Session.Title()
}

type Model struct {
	tabs   []Tab
	active int
	width  int
	styles theme.Styles
	theme  theme.Theme
	nextID int
}

func New(th theme.Theme, styles theme.Styles) Model {
	m := Model{styles: styles, theme: th}
	m.New("new")
	return m
}

func (m *Model) SetStyles(s theme.Styles) { m.styles = s }
func (m *Model) SetTheme(t theme.Theme)   { m.theme = t }
func (m *Model) SetWidth(w int)           { m.width = w }

// New appends a new tab whose session is a fresh host session with the
// given title. Returns the created Tab.
func (m *Model) New(title string) Tab {
	m.nextID++
	id := fmt.Sprintf("tab-%d", m.nextID)
	t := Tab{
		ID:      id,
		Session: modules.NewHostSession(id, title),
	}
	m.tabs = append(m.tabs, t)
	m.active = len(m.tabs) - 1
	return t
}

// Close removes the active tab. Returns true if a tab was closed.
// The closed tab's session is moved to StateClosed.
func (m *Model) Close() bool {
	if len(m.tabs) == 0 {
		return false
	}
	closed := m.tabs[m.active]
	if closed.Session != nil {
		closed.Session.Close()
	}
	m.tabs = append(m.tabs[:m.active], m.tabs[m.active+1:]...)
	if m.active >= len(m.tabs) && m.active > 0 {
		m.active--
	}
	return true
}

func (m *Model) Next() {
	if len(m.tabs) == 0 {
		return
	}
	m.active = (m.active + 1) % len(m.tabs)
}

func (m *Model) Prev() {
	if len(m.tabs) == 0 {
		return
	}
	m.active = (m.active - 1 + len(m.tabs)) % len(m.tabs)
}

func (m *Model) SetActive(i int) {
	if i >= 0 && i < len(m.tabs) {
		m.active = i
	}
}

func (m Model) Active() (Tab, bool) {
	if len(m.tabs) == 0 {
		return Tab{}, false
	}
	return m.tabs[m.active], true
}

func (m Model) Count() int     { return len(m.tabs) }
func (m Model) ActiveIdx() int { return m.active }

// HitTest returns the tab index at column x in the rendered bar, or -1 if none.
func (m Model) HitTest(x int) int {
	col := 0
	for i, t := range m.tabs {
		label := fmt.Sprintf(" %s ", t.Title())
		w := lipgloss.Width(label)
		if x >= col && x < col+w {
			return i
		}
		col += w
	}
	return -1
}

func (m Model) View() string {
	if len(m.tabs) == 0 {
		return m.styles.TabBar.Width(m.width).Render(" " + m.theme.Gradient("no tabs — ctrl+t to open") + " ")
	}
	out := ""
	for i, t := range m.tabs {
		title := t.Title()
		if i == m.active {
			active := m.styles.TabActive
			grad := m.theme.GradientStyle(title, lipgloss.NewStyle().
				Background(active.GetBackground()).
				Bold(true))
			pad := lipgloss.NewStyle().Background(active.GetBackground()).Render(" ")
			out += pad + grad + pad
		} else {
			label := fmt.Sprintf(" %s ", title)
			out += m.styles.Tab.Render(label)
		}
	}
	if m.width > 0 {
		out = lipgloss.NewStyle().
			Width(m.width).
			MaxWidth(m.width).
			Inline(true).
			Render(out)
	}
	return out
}
