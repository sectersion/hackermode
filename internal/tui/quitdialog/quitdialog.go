// Package quitdialog is the "are you sure you want to quit?" confirm modal,
// modeled on Crush's quit dialog. Behavior:
//
//   - First ctrl+c opens the dialog. The "Nope" button is pre-selected so
//     a stray Enter does not exit.
//   - ←/→ or Tab switches between Yep / Nope. Enter / Space confirms the
//     current selection.
//   - `y` (Yes) or a second ctrl+c quits immediately.
//   - `n` or Esc cancels (closes the dialog without quitting).
//
// The dialog renders as a centered framed box, composited over the main
// view by the host using internal/tui/compose.
package quitdialog

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sectersion/hackermode/internal/tui/theme"
)

// Result reports what the dialog produced. Exactly one of Quit / Cancel
// is true when Open returns to false after Update.
type Result struct {
	Quit   bool
	Cancel bool
}

// Model is the dialog state.
type Model struct {
	open   bool
	noSel  bool // true == "Nope" selected (default)
	width  int
	height int
	theme  theme.Theme
	styles theme.Styles
}

// New returns a closed dialog. Open via Show.
func New(th theme.Theme, st theme.Styles) Model {
	return Model{theme: th, styles: st, noSel: true}
}

// SetSize updates the dimensions of the host canvas. The dialog is always
// centered, so this is what governs placement.
func (m *Model) SetSize(w, h int) { m.width, m.height = w, h }

// SetTheme and SetStyles let callers refresh styling without rebuilding.
func (m *Model) SetTheme(t theme.Theme)   { m.theme = t }
func (m *Model) SetStyles(s theme.Styles) { m.styles = s }

// Open reports whether the dialog is visible.
func (m Model) Open() bool { return m.open }

// Show resets the dialog to "Nope" and opens it.
func (m *Model) Show() {
	m.open = true
	m.noSel = true
}

// Hide closes the dialog.
func (m *Model) Hide() { m.open = false }

// Update consumes a tea.Msg while the dialog is open. The Result reports
// terminal outcomes (Quit / Cancel); ongoing arrow-key navigation
// returns the zero value.
func (m *Model) Update(msg tea.Msg) Result {
	if !m.open {
		return Result{}
	}
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return Result{}
	}
	switch key.Type {
	case tea.KeyCtrlC:
		// Second ctrl+c — quit immediately, no questions.
		m.Hide()
		return Result{Quit: true}
	case tea.KeyEsc:
		m.Hide()
		return Result{Cancel: true}
	case tea.KeyLeft, tea.KeyRight, tea.KeyTab, tea.KeyShiftTab:
		m.noSel = !m.noSel
		return Result{}
	case tea.KeyEnter, tea.KeySpace:
		m.Hide()
		if !m.noSel {
			return Result{Quit: true}
		}
		return Result{Cancel: true}
	case tea.KeyRunes:
		switch string(key.Runes) {
		case "y", "Y":
			m.Hide()
			return Result{Quit: true}
		case "n", "N":
			m.Hide()
			return Result{Cancel: true}
		}
	}
	return Result{}
}

// View returns a canvas-sized string with the dialog centered, suitable
// for compose.Overlay'ing on top of the host's main view.
func (m Model) View() string {
	if !m.open || m.width <= 0 || m.height <= 0 {
		return ""
	}

	question := "Are you sure you want to quit?"

	yes := m.renderButton("Yep!", !m.noSel)
	no := m.renderButton("Nope", m.noSel)
	buttons := lipgloss.JoinHorizontal(lipgloss.Center, yes, "  ", no)

	body := lipgloss.JoinVertical(lipgloss.Center,
		question,
		"",
		buttons,
	)

	box := lipgloss.NewStyle().
		Background(m.theme.BgPanel).
		Foreground(m.theme.Fg).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BorderHi).
		Padding(1, 3).
		Render(body)

	// Stamp a gradient title rule above the box for visual identity.
	titleW := lipgloss.Width(box)
	title := m.theme.TitleRule("quit", titleW)
	stacked := lipgloss.JoinVertical(lipgloss.Center, title, box)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		stacked,
	)
}

func (m Model) renderButton(label string, selected bool) string {
	pad := strings.Repeat(" ", 2)
	text := pad + label + pad
	if selected {
		return m.theme.GradientStyle(text, lipgloss.NewStyle().Bold(true))
	}
	return lipgloss.NewStyle().
		Foreground(m.theme.FgMuted).
		Render(text)
}
