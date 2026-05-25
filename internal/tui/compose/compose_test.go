package compose

import (
	"strings"
	"testing"
)

// Reuse stripANSI for assertions.
func plain(s string) string { return stripANSI(s) }

func TestOverlay_EmptyTopReturnsBase(t *testing.T) {
	got := Overlay("hello\nworld", "")
	if got != "hello\nworld" {
		t.Fatalf("expected base unchanged, got %q", got)
	}
}

func TestOverlay_FullyTransparentTopPreservesBase(t *testing.T) {
	base := "ABCDEF\nGHIJKL"
	top := "      \n      "
	got := Overlay(base, top)
	if plain(got) != "ABCDEF\nGHIJKL" {
		t.Fatalf("expected base preserved, got %q", plain(got))
	}
}

func TestOverlay_OpaqueTopReplacesBase(t *testing.T) {
	base := "ABCDEF\nGHIJKL"
	top := "xxxxxx\nyyyyyy" // no spaces — fully opaque
	got := Overlay(base, top)
	if plain(got) != "xxxxxx\nyyyyyy" {
		t.Fatalf("expected top to win, got %q", plain(got))
	}
}

func TestOverlay_PartialTransparency(t *testing.T) {
	// The top has a centered block flanked by spaces. Those spaces should
	// let the base show through.
	base := "1234567890"
	top := "   ABCD   "
	got := plain(Overlay(base, top))
	if got != "123ABCD890" {
		t.Fatalf("expected mixed line '123ABCD890', got %q", got)
	}
}

func TestOverlay_StyledSpaceIsNotTransparent(t *testing.T) {
	// A space carrying an SGR (background color) should *not* be treated
	// as transparent — that's how the palette box paints its interior.
	base := "1234567890"
	// "\x1b[44m   \x1b[0m" — three blue-bg spaces. These must overwrite
	// the base cells underneath.
	top := "\x1b[44m   \x1b[0m1234567"
	got := plain(Overlay(base, top))
	if got != "   1234567" {
		t.Fatalf("expected styled spaces to overwrite, got %q", got)
	}
}

func TestOverlay_TopLongerThanBaseExtends(t *testing.T) {
	base := "AB"
	top := "  CDEF"
	got := plain(Overlay(base, top))
	// Cols 0-1 transparent → AB; cols 2-5 opaque → CDEF.
	if got != "ABCDEF" {
		t.Fatalf("expected merged 'ABCDEF', got %q", got)
	}
}

func TestOverlay_MoreLinesInTopAdded(t *testing.T) {
	base := "one"
	top := "one\ntwo"
	got := plain(Overlay(base, top))
	if got != "one\ntwo" {
		t.Fatalf("expected extra line added, got %q", got)
	}
}

func TestDecode_PreservesRunesAndAttachesStyle(t *testing.T) {
	in := "a\x1b[31mb\x1b[0mc"
	cells := decode(in)
	if len(cells) != 3 {
		t.Fatalf("expected 3 cells, got %d", len(cells))
	}
	if cells[0].r != 'a' || cells[0].style != "" {
		t.Fatalf("cell 0: %+v", cells[0])
	}
	if cells[1].r != 'b' || !strings.Contains(cells[1].style, "31m") {
		t.Fatalf("cell 1 should be red 'b': %+v", cells[1])
	}
	// After the reset, the next rune's style should be empty.
	if cells[2].r != 'c' || cells[2].style != "" {
		t.Fatalf("cell 2 expected unstyled 'c', got %+v", cells[2])
	}
}

func TestRoundTrip_DecodeEncodePreservesContent(t *testing.T) {
	in := "hello \x1b[32mgreen\x1b[0m world"
	out := encode(decode(in))
	if plain(out) != plain(in) {
		t.Fatalf("round trip changed visible content: %q → %q", plain(in), plain(out))
	}
}
