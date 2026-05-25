// Package commands — when-clause stub evaluator (Phase B).
//
// This is intentionally a tiny, dumb evaluator. It accepts:
//
//   - empty string / "always" → always true
//   - <ident> == "<string>"
//   - <ident> != "<string>"
//   - <ident> > <number>
//   - <ident> < <number>
//
// And nothing else. Phase E replaces this with a real recursive-descent
// parser supporting `&&`, `||`, `!`, parens, etc. Unparseable expressions
// are *accepted* (logged warning) and evaluate to `false`, so manifests
// using future grammar simply hide their commands until Phase E lands.
//
// Context is supplied as a flat string map per evaluation:
//
//	{
//	  "tab.module": "host",
//	  "tab.count":  "3",
//	  ...
//	}
//
// Numeric comparisons parse the context value as int64; non-numeric
// values compare false. Quoting in expressions accepts both ' and ".
package commands

import (
	"strconv"
	"strings"
)

// Context holds the variables a when-clause is evaluated against.
type Context map[string]string

type whenOp int

const (
	whenAlways whenOp = iota
	whenEq
	whenNe
	whenGt
	whenLt
	whenInvalid
)

// whenExpr is the parsed representation.
type whenExpr struct {
	op    whenOp
	ident string
	str   string // for eq/ne
	num   int64  // for gt/lt
}

// parseWhen returns a parsed expression. Unrecognized forms produce
// whenInvalid, which evaluates to false (the caller may log).
func parseWhen(src string) whenExpr {
	s := strings.TrimSpace(src)
	if s == "" || s == "always" {
		return whenExpr{op: whenAlways}
	}

	if e, ok := parseStringOp(s, "==", whenEq); ok {
		return e
	}
	if e, ok := parseStringOp(s, "!=", whenNe); ok {
		return e
	}
	if e, ok := parseNumOp(s, ">", whenGt); ok {
		return e
	}
	if e, ok := parseNumOp(s, "<", whenLt); ok {
		return e
	}
	return whenExpr{op: whenInvalid}
}

func parseStringOp(s, opTok string, op whenOp) (whenExpr, bool) {
	i := strings.Index(s, opTok)
	if i < 0 {
		return whenExpr{}, false
	}
	lhs := strings.TrimSpace(s[:i])
	rhs := strings.TrimSpace(s[i+len(opTok):])
	if !isIdent(lhs) {
		return whenExpr{}, false
	}
	str, ok := unquote(rhs)
	if !ok {
		return whenExpr{}, false
	}
	return whenExpr{op: op, ident: lhs, str: str}, true
}

func parseNumOp(s, opTok string, op whenOp) (whenExpr, bool) {
	// Avoid catching "==" / "!=" with the single-char ops.
	if strings.Contains(s, "==") || strings.Contains(s, "!=") {
		return whenExpr{}, false
	}
	i := strings.Index(s, opTok)
	if i < 0 {
		return whenExpr{}, false
	}
	lhs := strings.TrimSpace(s[:i])
	rhs := strings.TrimSpace(s[i+len(opTok):])
	if !isIdent(lhs) {
		return whenExpr{}, false
	}
	n, err := strconv.ParseInt(rhs, 10, 64)
	if err != nil {
		return whenExpr{}, false
	}
	return whenExpr{op: op, ident: lhs, num: n}, true
}

// eval runs the parsed expression against ctx. Invalid (unparseable)
// expressions evaluate to false.
func (e whenExpr) eval(ctx Context) bool {
	switch e.op {
	case whenAlways:
		return true
	case whenEq:
		return ctx[e.ident] == e.str
	case whenNe:
		return ctx[e.ident] != e.str
	case whenGt:
		n, err := strconv.ParseInt(ctx[e.ident], 10, 64)
		if err != nil {
			return false
		}
		return n > e.num
	case whenLt:
		n, err := strconv.ParseInt(ctx[e.ident], 10, 64)
		if err != nil {
			return false
		}
		return n < e.num
	default:
		return false
	}
}

func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r == '_':
		case r == '.' && i > 0:
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// unquote accepts "..." or '...' and returns the inner literal. No escape
// sequences are interpreted in the stub.
func unquote(s string) (string, bool) {
	if len(s) < 2 {
		return "", false
	}
	q := s[0]
	if (q != '"' && q != '\'') || s[len(s)-1] != q {
		return "", false
	}
	return s[1 : len(s)-1], true
}
