// Package streambridge wires a stream-mode module's stdout into the host
// TUI as Bubble Tea messages.
package streambridge

import (
	"bufio"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

// ChunkMsg carries a chunk of bytes read from a module's stdout.
type ChunkMsg struct {
	TabID string
	Data  []byte
}

// ClosedMsg is sent when the module's stdout closes.
type ClosedMsg struct {
	TabID string
}

// Start spawns a goroutine that reads from r, sending ChunkMsg / ClosedMsg
// to send. The goroutine exits when r is closed.
func Start(tabID string, r io.Reader, send func(tea.Msg)) {
	go func() {
		br := bufio.NewReader(r)
		buf := make([]byte, 4096)
		for {
			n, err := br.Read(buf)
			if n > 0 {
				cp := make([]byte, n)
				copy(cp, buf[:n])
				send(ChunkMsg{TabID: tabID, Data: cp})
			}
			if err != nil {
				send(ClosedMsg{TabID: tabID})
				return
			}
		}
	}()
}
