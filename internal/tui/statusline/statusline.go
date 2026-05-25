// Package statusline renders the bottom row of keybind hints. Hints depend
// on focus (input/output/tabs/panel) and the active module's contributions.
package statusline

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sectersion/hackermode/internal/tui/keymap"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

type Focus int

const (
	FocusInput Focus = iota
	FocusOutput
	FocusTabs
	FocusPanel
)

type Hint struct {
	Key  string
	Desc string
}

type Model struct {
	keys   keymap.Map
	styles theme.Styles
	theme  theme.Theme
	width  int
	focus  Focus
	extra  []Hint // module-contributed
	msg    string // optional ephemeral message (right-aligned)
}

func New(keys keymap.Map, th theme.Theme, styles theme.Styles) Model {
	return Model{keys: keys, styles: styles, theme: th}
}

func (m *Model) SetStyles(s theme.Styles) { m.styles = s }
func (m *Model) SetTheme(t theme.Theme)   { m.theme = t }
func (m *Model) SetWidth(w int)           { m.width = w }
func (m *Model) SetFocus(f Focus)         { m.focus = f }
func (m *Model) SetMessage(s string)      { m.msg = s }
func (m *Model) SetExtraHints(h []Hint)   { m.extra = h }
func (m *Model) SetKeys(k keymap.Map)     { m.keys = k }

func (m Model) View() string {
	hints := m.hintsFor(m.focus)

	var b strings.Builder
	keyBase := lipgloss.NewStyle().
		Background(m.styles.StatusBar.GetBackground()).
		Bold(true)
	for i, h := range hints {
		if i > 0 {
			b.WriteString(m.styles.Muted.Render("  ·  "))
		}
		b.WriteString(m.theme.GradientStyle(h.Key, keyBase))
		b.WriteString(" ")
		b.WriteString(m.styles.StatusDesc.Render(h.Desc))
	}

	left := b.String()
	right := m.msg

	if m.width <= 0 {
		return m.styles.StatusBar.Render(left)
	}

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := m.width - leftW - rightW - 2 // padding
	if gap < 1 {
		gap = 1
	}
	row := left + strings.Repeat(" ", gap) + right
	return m.styles.StatusBar.Width(m.width).Render(row)
}

func (m Model) hintsFor(f Focus) []Hint {
	add := func(out *[]Hint, action string) {
		k, d := m.keys.Help(action)
		if k == "" {
			return
		}
		*out = append(*out, Hint{Key: k, Desc: d})
	}

	out := []Hint{}
	switch f {
	case FocusInput:
		add(&out, keymap.Submit)
		add(&out, keymap.Complete)
		add(&out, keymap.HistoryPrev)
		add(&out, keymap.NewTab)
		add(&out, keymap.CloseTab)
		add(&out, keymap.TogglePanel)
		add(&out, keymap.CommandPalette)
		add(&out, keymap.Quit)
	case FocusOutput:
		add(&out, keymap.ScrollUp)
		add(&out, keymap.ScrollDown)
		add(&out, keymap.FocusInput)
		add(&out, keymap.Quit)
	case FocusTabs:
		add(&out, keymap.NextTab)
		add(&out, keymap.PrevTab)
		add(&out, keymap.NewTab)
		add(&out, keymap.CloseTab)
		add(&out, keymap.FocusInput)
	case FocusPanel:
		add(&out, keymap.TogglePanel)
		add(&out, keymap.FocusInput)
	}
	out = append(out, m.extra...)
	return out
}
