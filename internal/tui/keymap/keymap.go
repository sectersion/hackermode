// Package keymap turns config keymap entries into named bindings the TUI can
// query. It uses bubbles/key for matching against tea.KeyMsg so we get the
// same behavior bubbletea components expect.
package keymap

import (
	"github.com/charmbracelet/bubbles/key"
)

// Action names the host knows about. Modules can register additional actions
// later.
const (
	Quit            = "quit"
	TogglePanel     = "toggle_panel"
	CommandPalette  = "command_palette"
	NewTab          = "new_tab"
	CloseTab        = "close_tab"
	NextTab         = "next_tab"
	PrevTab         = "prev_tab"
	FocusInput      = "focus_input"
	ScrollUp        = "scroll_up"
	ScrollDown      = "scroll_down"
	HistoryPrev     = "history_prev"
	HistoryNext     = "history_next"
	Complete        = "complete"
	Submit          = "submit"
)

// Map holds resolved bindings keyed by action.
type Map struct {
	bindings map[string]key.Binding
	helpFor  map[string]string
}

// New builds a Map from raw "action -> keys" pairs (as parsed from config).
// Multiple keys can be comma-separated in the value.
func New(raw map[string]string) Map {
	m := Map{
		bindings: make(map[string]key.Binding, len(raw)),
		helpFor:  helpStrings(),
	}
	for action, spec := range raw {
		keys := splitKeys(spec)
		if len(keys) == 0 {
			continue
		}
		help := m.helpFor[action]
		m.bindings[action] = key.NewBinding(
			key.WithKeys(keys...),
			key.WithHelp(keys[0], help),
		)
	}
	return m
}

// Binding returns the bubbles key.Binding for an action. Missing actions
// return a disabled binding.
func (m Map) Binding(action string) key.Binding {
	if b, ok := m.bindings[action]; ok {
		return b
	}
	b := key.NewBinding()
	b.SetEnabled(false)
	return b
}

// Help returns (key, description) for an action, suitable for the statusline.
func (m Map) Help(action string) (string, string) {
	b := m.bindings[action]
	help := b.Help()
	return help.Key, help.Desc
}

func splitKeys(spec string) []string {
	out := []string{}
	cur := ""
	for _, r := range spec {
		switch r {
		case ',':
			if cur != "" {
				out = append(out, cur)
				cur = ""
			}
		case ' ', '\t':
			// allow "ctrl+c, ctrl+q"
		default:
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func helpStrings() map[string]string {
	return map[string]string{
		Quit:           "quit",
		TogglePanel:    "panel",
		CommandPalette: "cmd",
		NewTab:         "new tab",
		CloseTab:       "close",
		NextTab:        "next",
		PrevTab:        "prev",
		FocusInput:     "focus",
		ScrollUp:       "scroll up",
		ScrollDown:     "scroll down",
		HistoryPrev:    "history",
		HistoryNext:    "history",
		Complete:       "complete",
		Submit:         "submit",
	}
}
