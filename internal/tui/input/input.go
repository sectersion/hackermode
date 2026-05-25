// Package input is the bottom prompt: a single-line text input with command
// history (up/down) and a stub completer. Modules will register completers
// later via the SDK.
package input

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

// Completer returns suggestions for the given prefix at cursor. Phase A uses
// a stub; Phase B replaces this with per-module completers.
type Completer func(text string, cursor int) []string

type Model struct {
	ti       textinput.Model
	history  []string
	histIdx  int // -1 means "current draft"
	draft    string
	complete Completer
	styles   theme.Styles
	theme    theme.Theme
	prompt   string
}

func New(th theme.Theme, styles theme.Styles) Model {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "type a command…"
	ti.CharLimit = 0
	ti.Focus()
	return Model{
		ti:       ti,
		histIdx:  -1,
		styles:   styles,
		theme:    th,
		prompt:   "❯ ",
		complete: stubCompleter,
	}
}

func (m *Model) SetStyles(s theme.Styles) { m.styles = s }
func (m *Model) SetTheme(t theme.Theme)   { m.theme = t }
func (m *Model) SetCompleter(c Completer) { m.complete = c }

func (m *Model) Focus()        { m.ti.Focus() }
func (m *Model) Blur()         { m.ti.Blur() }
func (m *Model) Focused() bool { return m.ti.Focused() }
func (m *Model) Value() string { return m.ti.Value() }
func (m *Model) Reset()        { m.ti.SetValue(""); m.histIdx = -1; m.draft = "" }

func (m *Model) SetWidth(w int) {
	w -= len(m.prompt) + 2
	if w < 1 {
		w = 1
	}
	m.ti.Width = w
}

// Submit pushes the current value into history and returns it.
func (m *Model) Submit() string {
	v := m.ti.Value()
	if v != "" && (len(m.history) == 0 || m.history[len(m.history)-1] != v) {
		m.history = append(m.history, v)
	}
	m.Reset()
	return v
}

// HistoryPrev moves to the previous entry (older).
func (m *Model) HistoryPrev() {
	if len(m.history) == 0 {
		return
	}
	if m.histIdx == -1 {
		m.draft = m.ti.Value()
		m.histIdx = len(m.history) - 1
	} else if m.histIdx > 0 {
		m.histIdx--
	}
	m.ti.SetValue(m.history[m.histIdx])
	m.ti.CursorEnd()
}

// HistoryNext moves toward the most recent / draft.
func (m *Model) HistoryNext() {
	if m.histIdx == -1 {
		return
	}
	m.histIdx++
	if m.histIdx >= len(m.history) {
		m.histIdx = -1
		m.ti.SetValue(m.draft)
	} else {
		m.ti.SetValue(m.history[m.histIdx])
	}
	m.ti.CursorEnd()
}

// Complete attempts a single-suggestion completion. If exactly one
// suggestion matches, replace the value with it. Multiple suggestions are
// surfaced via the returned slice (caller may render a popup).
func (m *Model) Complete() []string {
	val := m.ti.Value()
	pos := m.ti.Position()
	suggs := m.complete(val, pos)
	if len(suggs) == 1 {
		m.ti.SetValue(suggs[0])
		m.ti.CursorEnd()
		return nil
	}
	return suggs
}

// Update forwards messages to the embedded textinput.
func (m *Model) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	if m.histIdx != -1 && m.ti.Value() != m.history[m.histIdx] {
		// User edited a recalled history line — drop history mode.
		m.histIdx = -1
	}
	return cmd
}

func (m Model) View() string {
	// Gradient the prompt glyph over the input background so it blends in.
	base := lipgloss.NewStyle().
		Background(m.styles.Input.GetBackground()).
		Bold(true)
	prompt := m.theme.GradientStyle(m.prompt, base)
	return m.styles.Input.Render(prompt + m.ti.View())
}

// stubCompleter is the Phase A placeholder — recognises a couple of fake
// commands so the user can see the mechanism work.
func stubCompleter(text string, _ int) []string {
	candidates := []string{":help", ":quit", ":new", ":close", ":panel", ":theme"}
	out := []string{}
	for _, c := range candidates {
		if len(text) <= len(c) && c[:len(text)] == text {
			out = append(out, c)
		}
	}
	return out
}
