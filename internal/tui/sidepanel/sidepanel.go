// Package sidepanel renders the right-hand column: loaded modules, recent
// notifications, and module-contributed widgets. In Phase A it shows mock
// content so the layout is real; Phase B wires it to the module manager.
package sidepanel

import (
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

type Notification struct {
	Level   string // info | warn | error
	Title   string
	Body    string
	Time    time.Time
}

type ModuleStatus struct {
	ID    string
	State string // running | idle | error
}

// Widget is a module-contributed live mini-view rendered in the side
// panel. Phase B supports text widgets; richer kinds (kv, list, progress)
// arrive in Phase E.
type Widget struct {
	Owner string // module ID
	Key   string // stable id within owner (e.g. "unread")
	Title string // human-readable label
	Text  string // current text body
}

type Model struct {
	visible bool
	width   int
	height  int
	styles  theme.Styles
	theme   theme.Theme

	modules []ModuleStatus
	notifs  []Notification
	widgets []Widget
}

func New(th theme.Theme, styles theme.Styles, visible bool) Model {
	return Model{
		visible: visible,
		styles:  styles,
		theme:   th,
		modules: []ModuleStatus{}, // empty in Phase A
		notifs:  []Notification{},
	}
}

func (m *Model) SetStyles(s theme.Styles) { m.styles = s }
func (m *Model) SetTheme(t theme.Theme)   { m.theme = t }
func (m *Model) SetSize(w, h int)         { m.width = w; m.height = h }
func (m *Model) Toggle()                  { m.visible = !m.visible }
func (m Model) Visible() bool             { return m.visible }
func (m Model) Width() int                { return m.width }

// SetWidget upserts a widget keyed by (Owner, Key). Modules call this via
// the host's `panel` RPC; the host translates to a call here. New
// widgets are appended in registration order; updates keep position.
func (m *Model) SetWidget(w Widget) {
	for i, existing := range m.widgets {
		if existing.Owner == w.Owner && existing.Key == w.Key {
			m.widgets[i] = w
			return
		}
	}
	m.widgets = append(m.widgets, w)
}

// RemoveWidget drops a widget by (Owner, Key). Missing keys are a no-op.
func (m *Model) RemoveWidget(owner, key string) {
	out := m.widgets[:0]
	for _, w := range m.widgets {
		if w.Owner == owner && w.Key == key {
			continue
		}
		out = append(out, w)
	}
	m.widgets = out
}

// RemoveWidgetsOf drops every widget owned by the given module. Called
// when a module exits so stale tiles don't linger.
func (m *Model) RemoveWidgetsOf(owner string) {
	out := m.widgets[:0]
	for _, w := range m.widgets {
		if w.Owner == owner {
			continue
		}
		out = append(out, w)
	}
	m.widgets = out
}

func (m *Model) AddNotification(n Notification) {
	if n.Time.IsZero() {
		n.Time = time.Now()
	}
	m.notifs = append([]Notification{n}, m.notifs...)
	if len(m.notifs) > 20 {
		m.notifs = m.notifs[:20]
	}
}

func (m Model) View() string {
	if !m.visible || m.width <= 0 || m.height <= 0 {
		return ""
	}

	var b strings.Builder

	b.WriteString(m.theme.Gradient("modules"))
	b.WriteString("\n")
	if len(m.modules) == 0 {
		b.WriteString(m.styles.Muted.Render("none installed"))
		b.WriteString("\n")
	} else {
		for _, mod := range m.modules {
			b.WriteString(mod.ID)
			b.WriteString("  ")
			b.WriteString(m.styles.Muted.Render(mod.State))
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(m.theme.Gradient("notifications"))
	b.WriteString("\n")
	if len(m.notifs) == 0 {
		b.WriteString(m.styles.Muted.Render("no recent activity"))
		b.WriteString("\n")
	} else {
		for _, n := range m.notifs {
			ts := n.Time.Format("15:04")
			b.WriteString(m.styles.Muted.Render(ts))
			b.WriteString(" ")
			b.WriteString(n.Title)
			b.WriteString("\n")
			if n.Body != "" {
				b.WriteString(m.styles.Muted.Render("  " + n.Body))
				b.WriteString("\n")
			}
		}
	}

	if len(m.widgets) > 0 {
		b.WriteString("\n")
		b.WriteString(m.theme.Gradient("widgets"))
		b.WriteString("\n")
		for _, w := range m.widgets {
			b.WriteString(m.styles.Muted.Render(w.Title))
			b.WriteString("\n")
			b.WriteString(w.Text)
			b.WriteString("\n")
		}
	}

	return m.styles.Panel.
		Width(m.width).
		Height(m.height).
		MaxHeight(m.height).
		Render(lipgloss.NewStyle().MaxWidth(m.width - 3).Render(b.String()))
}
