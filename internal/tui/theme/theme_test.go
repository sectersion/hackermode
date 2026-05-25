package theme

import (
	"strings"
	"testing"
)

// stripANSI removes simple CSI sequences so we can assert on plain text.
func stripANSI(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEsc = true
		case inEsc:
			if (r >= 0x40 && r <= 0x7e) && r != '[' && r != ';' {
				inEsc = false
			}
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestResolve_FallsBackToDefault(t *testing.T) {
	if Resolve("does-not-exist").Name != "default" {
		t.Fatal("unknown theme should fall back to default")
	}
	if Resolve("").Name != "default" {
		t.Fatal("empty name should resolve to default")
	}
}

func TestGradient_PreservesText(t *testing.T) {
	th := Default()
	out := th.Gradient("hello")
	plain := stripANSI(out)
	if plain != "hello" {
		t.Fatalf("gradient changed text: got %q", plain)
	}
}

func TestGradient_EmptyStringStaysEmpty(t *testing.T) {
	if Default().Gradient("") != "" {
		t.Fatal("gradient on empty should return empty")
	}
}

func TestDivider_LengthMatchesWidth(t *testing.T) {
	th := Default()
	out := th.Divider(10)
	plain := stripANSI(out)
	if len([]rune(plain)) != 10 {
		t.Fatalf("expected 10 runes, got %d (%q)", len([]rune(plain)), plain)
	}
	for _, r := range plain {
		if string(r) != DividerDiag {
			t.Fatalf("expected only %q runes, got %q", DividerDiag, r)
		}
	}
}

func TestDivider_ZeroWidth(t *testing.T) {
	if Default().Divider(0) != "" {
		t.Fatal("zero-width divider should be empty")
	}
	if Default().Divider(-5) != "" {
		t.Fatal("negative-width divider should be empty")
	}
}

func TestTitleRule_IncludesTitleAndFiller(t *testing.T) {
	th := Default()
	rule := stripANSI(th.TitleRule("modules", 30))
	if !strings.HasPrefix(rule, "modules ") {
		t.Fatalf("title should lead: %q", rule)
	}
	// The remainder should be all divider chars.
	rest := strings.TrimPrefix(rule, "modules ")
	for _, r := range rest {
		if string(r) != DividerDiag {
			t.Fatalf("expected only divider chars after title, got %q", r)
		}
	}
}

func TestTitleRule_TitleOnlyWhenNarrow(t *testing.T) {
	th := Default()
	// Width equal to title length leaves no room for divider — title only.
	rule := stripANSI(th.TitleRule("hi", 2))
	if rule != "hi" {
		t.Fatalf("expected just 'hi', got %q", rule)
	}
}
