// Package theme defines styling tokens used by the TUI. v0 ships one dark
// theme inspired by Crush's signature gradient — but rotated from
// purple→pink to a vivid blue→green. Modules read tokens via the SDK so
// they don't import lipgloss directly across the host boundary.
package theme

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucasb-eyer/go-colorful"
)

// Brand colors. Crush uses #6b50ff (charple) → #ff6daa (pink); we keep the
// same energy but in blue→green.
const (
	BrandStart = "#00b4ff" // electric blue
	BrandEnd   = "#00ffb2" // mint green ("julep" in charm parlance)
	BrandMid   = "#00e0d4" // teal — a useful single-tone fallback for the brand
)

type Theme struct {
	Name string

	// Surfaces
	Bg       lipgloss.Color
	BgSubtle lipgloss.Color
	BgPanel  lipgloss.Color
	Border   lipgloss.Color
	BorderHi lipgloss.Color

	// Text
	Fg       lipgloss.Color
	FgMuted  lipgloss.Color
	FgSubtle lipgloss.Color

	// Accents
	Primary lipgloss.Color
	Success lipgloss.Color
	Warning lipgloss.Color
	Error   lipgloss.Color
	Info    lipgloss.Color

	// Brand gradient endpoints (used by Gradient).
	GradStart lipgloss.Color
	GradEnd   lipgloss.Color
}

// Default returns the built-in dark theme.
func Default() Theme {
	return Theme{
		Name: "default",

		Bg:       lipgloss.Color("#0e1116"),
		BgSubtle: lipgloss.Color("#0a0d12"),
		BgPanel:  lipgloss.Color("#11161d"),
		Border:   lipgloss.Color("#1f2730"),
		BorderHi: lipgloss.Color(BrandMid),

		Fg:       lipgloss.Color("#d6e3f0"),
		FgMuted:  lipgloss.Color("#8a99ad"),
		FgSubtle: lipgloss.Color("#4a5566"),

		Primary: lipgloss.Color(BrandMid),
		Success: lipgloss.Color(BrandEnd),
		Warning: lipgloss.Color("#ffd166"),
		Error:   lipgloss.Color("#ff6b6b"),
		Info:    lipgloss.Color(BrandStart),

		GradStart: lipgloss.Color(BrandStart),
		GradEnd:   lipgloss.Color(BrandEnd),
	}
}

// Resolve picks a theme by name, falling back to Default.
func Resolve(name string) Theme {
	switch name {
	case "", "default":
		return Default()
	default:
		return Default()
	}
}

// Gradient renders s with a horizontal foreground gradient from t.GradStart
// to t.GradEnd. Each rune is colored independently. Bold is applied for
// visual weight (matching Crush's brand treatment).
func (t Theme) Gradient(s string) string {
	return t.GradientStyle(s, lipgloss.NewStyle().Bold(true))
}

// GradientStyle is Gradient with a caller-supplied base style. The base
// style's Foreground is overridden per rune; other attributes (background,
// bold, padding) are preserved. Useful when you want a gradient on top of
// an existing background (e.g. inside the input box).
func (t Theme) GradientStyle(s string, base lipgloss.Style) string {
	if s == "" {
		return s
	}
	start, err1 := colorful.Hex(string(t.GradStart))
	end, err2 := colorful.Hex(string(t.GradEnd))
	if err1 != nil || err2 != nil {
		return s
	}

	runes := []rune(s)
	n := len(runes)
	if n == 0 {
		return s
	}

	var b strings.Builder
	for i, r := range runes {
		var ratio float64
		if n == 1 {
			ratio = 0
		} else {
			ratio = float64(i) / float64(n-1)
		}
		c := start.BlendLuv(end, ratio).Clamped()
		hex := colorToHex(c)
		b.WriteString(base.Foreground(lipgloss.Color(hex)).Render(string(r)))
	}
	return b.String()
}

func colorToHex(c colorful.Color) string {
	r, g, bl := c.RGB255()
	return fmt.Sprintf("#%02x%02x%02x", r, g, bl)
}

// Divider chars used by the TUI. The diagonal matches Crush's signature look.
const (
	DividerDiag = "╱"
	DividerLine = "─"
)

// Divider returns a gradient diagonal divider of the given width.
func (t Theme) Divider(width int) string {
	if width <= 0 {
		return ""
	}
	return t.GradientStyle(strings.Repeat(DividerDiag, width), lipgloss.NewStyle())
}

// TitleRule renders `title ╱╱╱╱╱…` filling the remaining width with a
// gradient diagonal rule. If width is too small for any rule glyphs, the
// title alone is returned.
func (t Theme) TitleRule(title string, width int) string {
	if title == "" {
		return t.Divider(width)
	}
	titleStyled := t.Gradient(title)
	titleW := lipgloss.Width(titleStyled)
	remaining := width - titleW - 1 // 1 for the space
	if remaining <= 0 {
		return titleStyled
	}
	return titleStyled + " " + t.Divider(remaining)
}

// Styles bundles ready-to-use lipgloss.Style values derived from a Theme.
type Styles struct {
	App         lipgloss.Style
	Tab         lipgloss.Style
	TabActive   lipgloss.Style
	TabBar      lipgloss.Style
	Output      lipgloss.Style
	Input       lipgloss.Style
	InputPrompt lipgloss.Style
	Panel       lipgloss.Style
	PanelTitle  lipgloss.Style
	StatusBar   lipgloss.Style
	StatusKey   lipgloss.Style
	StatusDesc  lipgloss.Style
	Muted       lipgloss.Style
}

func NewStyles(t Theme) Styles {
	return Styles{
		App:    lipgloss.NewStyle().Background(t.Bg).Foreground(t.Fg),
		TabBar: lipgloss.NewStyle().Background(t.BgSubtle).Foreground(t.FgMuted),
		Tab: lipgloss.NewStyle().
			Padding(0, 2).
			Background(t.BgSubtle).
			Foreground(t.FgMuted),
		TabActive: lipgloss.NewStyle().
			Padding(0, 2).
			Background(t.Bg).
			Foreground(t.Primary).
			Bold(true),
		Output: lipgloss.NewStyle().Background(t.Bg).Foreground(t.Fg),
		Input: lipgloss.NewStyle().
			Background(t.BgSubtle).
			Foreground(t.Fg).
			Padding(0, 1),
		InputPrompt: lipgloss.NewStyle().Foreground(t.Primary).Bold(true),
		Panel: lipgloss.NewStyle().
			Background(t.BgPanel).
			Foreground(t.Fg).
			BorderStyle(lipgloss.NormalBorder()).
			BorderLeft(true).
			BorderForeground(t.Border).
			Padding(0, 1),
		PanelTitle: lipgloss.NewStyle().
			Foreground(t.Primary).
			Bold(true),
		StatusBar: lipgloss.NewStyle().
			Background(t.BgSubtle).
			Foreground(t.FgMuted).
			Padding(0, 1),
		StatusKey:  lipgloss.NewStyle().Foreground(t.Primary).Bold(true),
		StatusDesc: lipgloss.NewStyle().Foreground(t.FgMuted),
		Muted:      lipgloss.NewStyle().Foreground(t.FgSubtle),
	}
}
