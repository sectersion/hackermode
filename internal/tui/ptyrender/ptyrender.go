// Package ptyrender turns a PTY master byte stream into a renderable
// snapshot the host can paint each frame. It owns a vt10x terminal
// emulator per tab; bytes from the module's PTY are fed in, then the host
// reads cells out as a styled string.
//
// Why this exists: a TUI-mode module emits raw escape sequences targeted
// at a real terminal. The host can't simply append those into a scrollback —
// cursor moves, alt-screen, color changes don't make sense out of context.
// Instead the host runs an in-process VT100/VT220 emulator, lets it absorb
// the module's output, then samples the emulator's cell grid to produce a
// snapshot suitable for lipgloss layout.
//
// Phase B trades fidelity for simplicity: the snapshot is a per-cell render
// using the cell's printable character. We support 16-color ANSI foreground
// and background; xterm 256-color and 24-bit truecolor degrade to nearest
// ANSI. Bold / underline attributes are not surfaced in v0.
package ptyrender

import (
	"bufio"
	"fmt"
	"io"
	"strings"
	"sync"

	vt "github.com/hinshun/vt10x"
)

// Renderer wraps a vt10x terminal for a single TUI-mode tab.
type Renderer struct {
	mu     sync.Mutex
	term   vt.Terminal
	cols   int
	rows   int
	closed bool
}

// New returns a Renderer with the given initial size.
func New(cols, rows int) *Renderer {
	if cols < 1 {
		cols = 80
	}
	if rows < 1 {
		rows = 24
	}
	return &Renderer{
		term: vt.New(vt.WithSize(cols, rows)),
		cols: cols,
		rows: rows,
	}
}

// Resize updates the underlying emulator's dimensions.
func (r *Renderer) Resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	r.mu.Lock()
	r.cols, r.rows = cols, rows
	r.term.Resize(cols, rows)
	r.mu.Unlock()
}

// Pump consumes bytes from src and feeds them into the emulator until src
// closes. Safe to call once per Renderer.
func (r *Renderer) Pump(src io.Reader) {
	br := bufio.NewReader(src)
	for {
		if r.isClosed() {
			return
		}
		if err := r.term.Parse(br); err != nil {
			return
		}
	}
}

// Close marks the renderer closed; Pump will exit on the next iteration.
func (r *Renderer) Close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
}

func (r *Renderer) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// Snapshot returns the current screen as a styled string (rows joined by
// newlines). Color information is emitted as standard SGR escape sequences
// so downstream lipgloss / compose code sees normal styled text.
func (r *Renderer) Snapshot() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	prevFG, prevBG := vt.Color(7), vt.Color(0)
	first := true
	for y := 0; y < r.rows; y++ {
		if !first {
			b.WriteByte('\n')
		}
		first = false
		for x := 0; x < r.cols; x++ {
			g := r.term.Cell(x, y)
			fg, bg := g.FG, g.BG
			if fg != prevFG || bg != prevBG || (x == 0 && y == 0) {
				writeSGR(&b, fg, bg)
				prevFG, prevBG = fg, bg
			}
			if g.Char == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(g.Char)
			}
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func writeSGR(b *strings.Builder, fg, bg vt.Color) {
	b.WriteString("\x1b[")
	b.WriteString(sgrFG(fg))
	b.WriteByte(';')
	b.WriteString(sgrBG(bg))
	b.WriteByte('m')
}

func sgrFG(c vt.Color) string {
	if c < 8 {
		return fmt.Sprintf("3%d", int(c))
	}
	if c < 16 {
		return fmt.Sprintf("9%d", int(c-8))
	}
	// 256-color / truecolor — keep simple: emit as 38;5;n.
	return fmt.Sprintf("38;5;%d", int(c))
}

func sgrBG(c vt.Color) string {
	if c < 8 {
		return fmt.Sprintf("4%d", int(c))
	}
	if c < 16 {
		return fmt.Sprintf("10%d", int(c-8))
	}
	return fmt.Sprintf("48;5;%d", int(c))
}
