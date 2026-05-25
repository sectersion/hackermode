// Package output is the per-tab append-only scrollback. Phase A uses an
// in-memory buffer per tab; once modules wire in, each tab's buffer is fed
// from RPC frames or PTY output.
package output

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/sectersion/hackermode/internal/tui/theme"
)

type Model struct {
	vp      viewport.Model
	buffers map[string]*strings.Builder
	active  string
	styles  theme.Styles
}

func New(styles theme.Styles) Model {
	vp := viewport.New(0, 0)
	vp.MouseWheelEnabled = true
	return Model{
		vp:      vp,
		buffers: map[string]*strings.Builder{},
		styles:  styles,
	}
}

func (m *Model) SetStyles(s theme.Styles) { m.styles = s }

func (m *Model) SetSize(w, h int) {
	m.vp.Width = w
	m.vp.Height = h
	m.refresh()
}

// SetActive switches which tab's buffer is shown.
func (m *Model) SetActive(tabID string) {
	m.active = tabID
	if _, ok := m.buffers[tabID]; !ok {
		m.buffers[tabID] = &strings.Builder{}
	}
	m.refresh()
	m.vp.GotoBottom()
}

// Forget removes the buffer for a closed tab.
func (m *Model) Forget(tabID string) {
	delete(m.buffers, tabID)
}

// Append writes to the active tab's buffer, auto-scrolling to bottom if
// the user is already at the bottom.
func (m *Model) Append(tabID, text string) {
	buf, ok := m.buffers[tabID]
	if !ok {
		buf = &strings.Builder{}
		m.buffers[tabID] = buf
	}
	buf.WriteString(text)
	if tabID == m.active {
		atBottom := m.vp.AtBottom()
		m.refresh()
		if atBottom {
			m.vp.GotoBottom()
		}
	}
}

// Appendln writes a line.
func (m *Model) Appendln(tabID, line string) { m.Append(tabID, line+"\n") }

func (m *Model) ScrollUp(n int)   { m.vp.ScrollUp(n) }
func (m *Model) ScrollDown(n int) { m.vp.ScrollDown(n) }

// Update forwards messages (mouse/keys) to the underlying viewport when
// the output area is focused.
func (m *Model) Update(msg interface{}) {
	if teaMsg, ok := msg.(viewport.Model); ok {
		_ = teaMsg
	}
	// Real wiring happens through tea.Msg in the parent; bubbles/viewport
	// expects tea.Msg there.
}

func (m *Model) refresh() {
	if buf, ok := m.buffers[m.active]; ok {
		m.vp.SetContent(buf.String())
	} else {
		m.vp.SetContent("")
	}
}

func (m *Model) Viewport() *viewport.Model { return &m.vp }

func (m Model) View() string {
	return m.styles.Output.Render(m.vp.View())
}
