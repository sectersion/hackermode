// Command echo-tui is a minimal hackermode tui-mode module: a bubbletea
// program that echoes what you type. The host spawns this process,
// allocates a PTY for it, and renders the resulting cell grid in the
// active tab.
package main

import (
	"context"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sectersion/hackermode/pkg/module"
)

type model struct {
	input string
	lines []string
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if m.input != "" {
				m.lines = append(m.lines, m.input)
				m.input = ""
			}
		case tea.KeyBackspace:
			if n := len(m.input); n > 0 {
				m.input = m.input[:n-1]
			}
		case tea.KeyRunes, tea.KeySpace:
			m.input += string(k.Runes)
			if k.Type == tea.KeySpace {
				m.input += " "
			}
		}
	}
	return m, nil
}

var (
	titleStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	hintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	echoStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	promptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))
)

func (m model) View() string {
	var b []byte
	b = append(b, titleStyle.Render("echo-tui (Bubble Tea over PTY)")...)
	b = append(b, '\n', '\n')
	b = append(b, hintStyle.Render("type and press enter — esc/ctrl+c to quit")...)
	b = append(b, '\n', '\n')
	for _, l := range m.lines {
		b = append(b, echoStyle.Render("• "+l)...)
		b = append(b, '\n')
	}
	b = append(b, promptStyle.Render("❯ ")...)
	b = append(b, m.input...)
	return string(b)
}

func main() {
	err := module.RunTUI(context.Background(),
		module.Info{ID: "examples.echo-tui", Version: "0.1.0"},
		func(ctx context.Context, h module.Host) (tea.Model, error) {
			return model{}, nil
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "echo-tui:", err)
		os.Exit(1)
	}
}
