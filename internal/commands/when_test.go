package commands

import "testing"

func TestParseWhen_Always(t *testing.T) {
	for _, s := range []string{"", "always", "  always  "} {
		w := parseWhen(s)
		if w.op != whenAlways {
			t.Errorf("%q should be whenAlways, got %v", s, w.op)
		}
	}
}

func TestParseWhen_Equality(t *testing.T) {
	w := parseWhen(`tab.module == "acme.email"`)
	if w.op != whenEq || w.ident != "tab.module" || w.str != "acme.email" {
		t.Fatalf("unexpected parse: %+v", w)
	}
	// Single quotes also accepted.
	if w2 := parseWhen(`tab.module == 'host'`); w2.op != whenEq || w2.str != "host" {
		t.Fatalf("single quotes: %+v", w2)
	}
}

func TestParseWhen_Comparisons(t *testing.T) {
	gt := parseWhen("tab.count > 0")
	if gt.op != whenGt || gt.ident != "tab.count" || gt.num != 0 {
		t.Fatalf("gt parse: %+v", gt)
	}
	lt := parseWhen("tab.count < 10")
	if lt.op != whenLt || lt.num != 10 {
		t.Fatalf("lt parse: %+v", lt)
	}
}

func TestParseWhen_Invalid(t *testing.T) {
	for _, s := range []string{
		"tab.module",                  // no op
		"!network.online",             // negation (Phase E)
		"a && b",                      // logical and (Phase E)
		"123 == 'x'",                  // non-ident LHS
		`tab.module == acme.email`,    // unquoted RHS
		`tab.module=="x"`,             // we strip spaces; this actually parses — adjust expectation
	} {
		if w := parseWhen(s); w.op != whenInvalid && s != `tab.module=="x"` {
			t.Errorf("%q should be invalid, got %v", s, w.op)
		}
	}
}

func TestEval_Equality(t *testing.T) {
	w := parseWhen(`tab.module == "host"`)
	if !w.eval(Context{"tab.module": "host"}) {
		t.Fatal("expected true")
	}
	if w.eval(Context{"tab.module": "other"}) {
		t.Fatal("expected false")
	}
}

func TestEval_NotEqual(t *testing.T) {
	w := parseWhen(`tab.module != "host"`)
	if w.eval(Context{"tab.module": "host"}) {
		t.Fatal("expected false")
	}
	if !w.eval(Context{"tab.module": "other"}) {
		t.Fatal("expected true")
	}
}

func TestEval_Numeric(t *testing.T) {
	gt := parseWhen("tab.count > 1")
	if !gt.eval(Context{"tab.count": "5"}) {
		t.Fatal("5 > 1 should be true")
	}
	if gt.eval(Context{"tab.count": "0"}) {
		t.Fatal("0 > 1 should be false")
	}
	// Non-numeric context → false.
	if gt.eval(Context{"tab.count": "abc"}) {
		t.Fatal("non-numeric should be false")
	}
}

func TestEval_AlwaysAndInvalid(t *testing.T) {
	if !parseWhen("always").eval(nil) {
		t.Fatal("always should be true")
	}
	if parseWhen("garbage @@@ nope").eval(nil) {
		t.Fatal("invalid should be false")
	}
}
