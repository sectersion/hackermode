// Package palette renders a centered command palette overlay. In Phase A it
// exposes host-level actions (new tab, toggle panel, etc.). Modules will
// register their own commands here in Phase B.
//
// The palette is a self-contained component: caller pushes a list of
// commands, opens it, forwards key events while Open(), and receives a
// chosen Command back via Selected() / OnEnter.
package palette

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sectersion/hackermode/internal/tui/theme"
)

// Command is something the palette can run.
type Command struct {
	ID    string   // stable identifier the host switches on
	Title string   // displayed primary text
	Hint  string   // secondary text (keybind, category)
	Tags  []string // additional fuzzy-match tokens (kept lowercase)
}

// Result reports whichever command was activated.
type Result struct {
	Command  Command
	Canceled bool
}

type Model struct {
	open    bool
	input   textinput.Model
	all     []Command
	filtered []Command
	cursor  int

	width, height int

	theme  theme.Theme
	styles theme.Styles
}

func New(th theme.Theme, st theme.Styles) Model {
	ti := textinput.New()
	ti.Placeholder = "type to search commands…"
	ti.Prompt = ""
	ti.CharLimit = 0
	return Model{
		theme:  th,
		styles: st,
		input:  ti,
	}
}

func (m *Model) SetCommands(cmds []Command) {
	m.all = cmds
	m.filter()
}

func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }
func (m *Model) SetTheme(t theme.Theme)   { m.theme = t }
func (m *Model) SetStyles(s theme.Styles) { m.styles = s }

func (m Model) Open() bool { return m.open }

// Show resets and opens the palette.
func (m *Model) Show() {
	m.open = true
	m.input.SetValue("")
	m.input.Focus()
	m.cursor = 0
	m.filter()
}

// Hide closes the palette without selecting anything.
func (m *Model) Hide() {
	m.open = false
	m.input.Blur()
}

// Update handles keys while the palette is open. Returns a Result with
// Canceled=true on escape, a Command on enter, or zero value otherwise.
// The second return is whether the palette consumed the event.
func (m *Model) Update(msg tea.Msg) (Result, bool, tea.Cmd) {
	if !m.open {
		return Result{}, false, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEsc, tea.KeyCtrlC:
			m.Hide()
			return Result{Canceled: true}, true, nil
		case tea.KeyEnter:
			if m.cursor >= 0 && m.cursor < len(m.filtered) {
				chosen := m.filtered[m.cursor]
				m.Hide()
				return Result{Command: chosen}, true, nil
			}
			return Result{Canceled: true}, true, nil
		case tea.KeyUp, tea.KeyCtrlP:
			if m.cursor > 0 {
				m.cursor--
			}
			return Result{}, true, nil
		case tea.KeyDown, tea.KeyCtrlN:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return Result{}, true, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.filter()
	return Result{}, true, cmd
}

func (m *Model) filter() {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		m.filtered = append([]Command(nil), m.all...)
	} else {
		m.filtered = m.filtered[:0]
		for _, c := range m.all {
			if match(c, q) {
				m.filtered = append(m.filtered, c)
			}
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = len(m.filtered) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// match returns true if every token in q appears as a substring in the
// command's title / hint / tags (case-insensitive). Simple but predictable.
func match(c Command, q string) bool {
	haystack := strings.ToLower(c.Title + " " + c.Hint + " " + strings.Join(c.Tags, " "))
	for _, tok := range strings.Fields(q) {
		if !strings.Contains(haystack, tok) {
			return false
		}
	}
	return true
}

// View renders the palette centered in the parent's space. Returns the
// laid-out overlay string; the caller composites it onto the main view
// using lipgloss.Place.
func (m Model) View() string {
	if !m.open {
		return ""
	}

	w := m.width * 6 / 10
	if w < 40 {
		w = 40
	}
	if w > m.width-4 && m.width > 6 {
		w = m.width - 4
	}

	innerW := w - 4 // border (2) + padding (2)
	if innerW < 10 {
		innerW = 10
	}

	header := m.theme.TitleRule("command palette", innerW)
	prompt := m.theme.Gradient("❯") + " " + m.input.View()

	maxRows := m.height - 8
	if maxRows < 4 {
		maxRows = 4
	}
	if maxRows > 12 {
		maxRows = 12
	}

	rows := []string{}
	if len(m.filtered) == 0 {
		rows = append(rows, m.styles.Muted.Render("no matches"))
	} else {
		start := 0
		if m.cursor >= maxRows {
			start = m.cursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := start; i < end; i++ {
			rows = append(rows, m.renderRow(m.filtered[i], i == m.cursor, innerW))
		}
	}

	body := strings.Join(rows, "\n")

	box := lipgloss.NewStyle().
		Background(m.theme.BgPanel).
		Foreground(m.theme.Fg).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BorderHi).
		Padding(1, 1).
		Width(w)

	content := strings.Join([]string{header, "", prompt, "", body}, "\n")
	rendered := box.Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		rendered,
	)
}

func (m Model) renderRow(c Command, selected bool, width int) string {
	title := c.Title
	hint := c.Hint

	if selected {
		// Gradient the selected title; keep the row visually anchored with a leading marker.
		marker := m.theme.Gradient("▌")
		left := marker + " " + m.theme.Gradient(title)
		if hint == "" {
			return left
		}
		rightStyle := lipgloss.NewStyle().Foreground(m.theme.FgMuted)
		return alignRow(left, rightStyle.Render(hint), width)
	}

	left := "  " + lipgloss.NewStyle().Foreground(m.theme.Fg).Render(title)
	if hint == "" {
		return left
	}
	right := lipgloss.NewStyle().Foreground(m.theme.FgSubtle).Render(hint)
	return alignRow(left, right, width)
}

func alignRow(left, right string, width int) string {
	lw := lipgloss.Width(left)
	rw := lipgloss.Width(right)
	gap := width - lw - rw
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}
