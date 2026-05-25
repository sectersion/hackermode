// Package compose stacks rendered TUI layers (the base view, the command
// palette, future dialogs) into a single string, preserving the visible
// area of the base where the overlay is transparent.
//
// "Transparent" here means: a cell in the overlay that is whitespace with
// no styling. Concretely — when an overlay produces a line like
//
//	"          ╭─palette─╮          "
//
// The leading and trailing space runs are transparent: they let the base
// view show through. The styled "╭─palette─╮" replaces the base cells
// underneath. This is enough to give us a real centered overlay over a
// running UI without rebuilding it from scratch.
//
// The implementation walks both strings as sequences of grapheme clusters
// paired with their currently-active ANSI escape state. We pass styled
// cells through unchanged and skip plain-space cells in the overlay.
//
// Why not a full cell grid? Because every rendered byte already encodes
// styling — re-parsing into RGB cells and re-emitting ANSI would (a) be
// lossy for terminal-specific sequences (kitty graphics, sixel, hyperlinks)
// and (b) several times slower for every frame. The visible-token approach
// is good enough for our overlays, which are always rectangular boxes
// with a colored background.
package compose

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Overlay places top on top of base, treating whitespace-only runs in top
// (with no styling) as transparent. The output has at most max(lines(base),
// lines(top)) rows.
//
// Both inputs are expected to be the same rendered size as the destination
// surface; if they differ, base is padded with empty lines and top is
// padded with transparent ones.
func Overlay(base, top string) string {
	if top == "" {
		return base
	}
	baseLines := splitLines(base)
	topLines := splitLines(top)
	n := len(baseLines)
	if len(topLines) > n {
		n = len(topLines)
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		var b, t string
		if i < len(baseLines) {
			b = baseLines[i]
		}
		if i < len(topLines) {
			t = topLines[i]
		}
		out[i] = overlayLine(b, t)
	}
	return strings.Join(out, "\n")
}

// overlayLine composites one line: cells of top win unless they're plain
// whitespace, in which case the base cell is kept.
func overlayLine(base, top string) string {
	if top == "" {
		return base
	}
	// If the entire overlay line is plain whitespace, base wins outright.
	if isTransparentLine(top) {
		return base
	}

	// Walk top, collect per-cell (style, rune) info. Where the cell is
	// plain space, substitute the base cell at that column.
	topCells := decode(top)
	baseCells := decode(base)

	out := make([]cell, 0, len(topCells))
	for col, tc := range topCells {
		if tc.transparent() {
			if col < len(baseCells) {
				out = append(out, baseCells[col])
			} else {
				out = append(out, tc) // base ran out; keep transparent space
			}
		} else {
			out = append(out, tc)
		}
	}
	// If base is wider than the overlay, append the remaining base cells.
	if len(baseCells) > len(topCells) {
		out = append(out, baseCells[len(topCells):]...)
	}
	return encode(out)
}

// cell holds the styling escape sequence active when the rune was emitted
// (everything before it in the SGR-running state) and the rune itself.
type cell struct {
	style string // SGR / OSC etc accumulated up to this cell
	r     rune
}

// transparent reports whether the cell is unstyled whitespace — a space or
// tab with no active SGR foreground/background. We approximate "no active
// styling" by checking style == "" or that it is only the reset sequence.
func (c cell) transparent() bool {
	if c.r != ' ' && c.r != '\t' && c.r != 0 {
		return false
	}
	if c.style == "" {
		return true
	}
	// SGR reset only.
	return c.style == "\x1b[0m" || c.style == "\x1b[m"
}

// decode walks s, splitting it into cells. Each rune carries the most
// recent escape sequence preceding it. Resets re-clear the style.
func decode(s string) []cell {
	var (
		cells   []cell
		curStyle strings.Builder
	)
	emitReset := false

	i := 0
	for i < len(s) {
		// ANSI escape: ESC '['... or ESC ']'... — keep collecting bytes
		// until a terminating final-byte (CSI: 0x40-0x7E; OSC: BEL or ESC \).
		if s[i] == 0x1b && i+1 < len(s) {
			end := i + 1
			switch s[i+1] {
			case '[':
				end = i + 2
				for end < len(s) {
					c := s[end]
					end++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
			case ']':
				end = i + 2
				for end < len(s) {
					c := s[end]
					if c == 0x07 { // BEL terminator
						end++
						break
					}
					if c == 0x1b && end+1 < len(s) && s[end+1] == '\\' {
						end += 2
						break
					}
					end++
				}
			default:
				end = i + 2
			}
			esc := s[i:end]
			// A bare reset clears the running style; otherwise append.
			if esc == "\x1b[0m" || esc == "\x1b[m" {
				curStyle.Reset()
				emitReset = true
			} else {
				if emitReset {
					curStyle.WriteString("\x1b[0m")
					emitReset = false
				}
				curStyle.WriteString(esc)
			}
			i = end
			continue
		}
		// Normal rune.
		r, sz := decodeRune(s, i)
		i += sz
		cells = append(cells, cell{style: curStyle.String(), r: r})
	}
	return cells
}

// encode rebuilds a string from cells, emitting style changes on demand
// and a final reset at the end.
func encode(cells []cell) string {
	var b strings.Builder
	prev := ""
	for _, c := range cells {
		if c.style != prev {
			if prev != "" && c.style == "" {
				b.WriteString("\x1b[0m")
			}
			b.WriteString(c.style)
			prev = c.style
		}
		if c.r != 0 {
			b.WriteRune(c.r)
		}
	}
	if prev != "" {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// decodeRune returns the next rune at i and the byte size. We avoid the
// utf8 package to keep this tight; ASCII-only fallback handles the common
// case and unicode falls through to a simple multi-byte read.
func decodeRune(s string, i int) (rune, int) {
	c := s[i]
	if c < 0x80 {
		return rune(c), 1
	}
	// Tiny utf8 decoder.
	switch {
	case c < 0xc0:
		return '?', 1
	case c < 0xe0:
		if i+1 >= len(s) {
			return '?', 1
		}
		return rune(c&0x1f)<<6 | rune(s[i+1]&0x3f), 2
	case c < 0xf0:
		if i+2 >= len(s) {
			return '?', 1
		}
		return rune(c&0x0f)<<12 | rune(s[i+1]&0x3f)<<6 | rune(s[i+2]&0x3f), 3
	default:
		if i+3 >= len(s) {
			return '?', 1
		}
		return rune(c&0x07)<<18 | rune(s[i+1]&0x3f)<<12 | rune(s[i+2]&0x3f)<<6 | rune(s[i+3]&0x3f), 4
	}
}

// splitLines splits s on \n. Unlike strings.Split, a trailing newline does
// not produce an extra empty element.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// isTransparentLine reports whether the (rendered) line contains only
// unstyled whitespace.
func isTransparentLine(line string) bool {
	if line == "" {
		return true
	}
	// Strip ANSI to check the actual characters.
	for _, c := range stripANSI(line) {
		if c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}

// stripANSI removes CSI / OSC sequences. Approximate but sufficient for
// the "is this line content-bearing?" check.
func stripANSI(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[':
				j := i + 2
				for j < len(s) {
					c := s[j]
					j++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
				i = j
				continue
			case ']':
				j := i + 2
				for j < len(s) {
					if s[j] == 0x07 {
						j++
						break
					}
					if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
				i = j
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Width is exported for callers that need to know the visible cell width
// of a rendered string. Currently a thin wrapper over lipgloss.Width.
func Width(s string) int { return lipgloss.Width(s) }
