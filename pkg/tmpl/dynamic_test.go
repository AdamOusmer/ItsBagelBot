// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import (
	"strconv"
	"testing"
)

// one lexes a single-span template and returns its Token, so a table can be
// written in the spans a broadcaster types rather than in struct literals.
func one(t *testing.T, span string) Token {
	t.Helper()
	toks := Lex(span)
	if len(toks) != 1 || toks[0].Kind != KindVar {
		t.Fatalf("Lex(%q) = %v, want one span", span, toks)
	}
	return toks[0]
}

// TestDynamicPinsPayloadEdges pins the two spellings that differ and have to
// keep differing: a name with NO payload is half a token and stays literal,
// while a name with an EMPTY payload named an empty thing and resolves. It is
// the same rule {2} and {2:} follow in the golden table, and it is what the
// dashboard preview and the web builder both encode.
func TestDynamicPinsPayloadEdges(t *testing.T) {
	for _, tc := range []struct {
		span string
		want string
		ok   bool
	}{
		{"{choice}", "", false},
		{"{choice:}", "", true},
		{"{choice:only}", "only", true},
		{"{random:}", "", false},
		{"{random:5-5}", "5", true},
		{"{Random:5-5}", "5", true},
		// A malformed range is a typo, not a reason to roll 1..100.
		{"{random:1..6}", "", false},
		{"{random:9-2}", "", false},
		{"{random:-5-5}", "", false},
		{"{nothing}", "", false},
	} {
		got, ok := Dynamic(one(t, tc.span))
		if got != tc.want || ok != tc.ok {
			t.Errorf("Dynamic(%s) = %q,%v want %q,%v", tc.span, got, ok, tc.want, tc.ok)
		}
	}
}

// TestDynamicBareRandomIsPercentile pins the payload-free roll's range.
func TestDynamicBareRandomIsPercentile(t *testing.T) {
	tok := one(t, "{random}")
	for i := 0; i < 200; i++ {
		got, ok := Dynamic(tok)
		if !ok {
			t.Fatalf("Dynamic({random}) not ok")
		}
		if !rollInRange(got, 1, randomDefaultMax) {
			t.Fatalf("Dynamic({random}) = %q, want 1..%d", got, randomDefaultMax)
		}
	}
}

// rollInRange reports whether a rendered roll parses as an integer inside
// [low, high]. A predicate rather than three ORed tests at the call site.
func rollInRange(text string, low, high int) bool {
	n, err := strconv.Atoi(text)
	return err == nil && n >= low && n <= high
}

// TestDynamicChoiceKeepsPayloadCase pins that only the NAME folds: the options
// are copy a broadcaster wrote and must come back spelled as written.
func TestDynamicChoiceKeepsPayloadCase(t *testing.T) {
	got := Expand("{CHOICE:Hi}", Dynamic)
	if got != "Hi" {
		t.Errorf("Expand({CHOICE:Hi}) = %q, want %q", got, "Hi")
	}
}

// TestDynamicThroughExpandFallsBackAndStaysLiteral pins how Dynamic composes
// with the span grammar: an unknown name keeps its braces, an empty value
// takes the fallback.
func TestDynamicThroughExpandFallsBackAndStaysLiteral(t *testing.T) {
	for _, tc := range [][2]string{
		{"{choice}", "{choice}"},
		{"{choice|none}", "{choice|none}"},
		{"[{choice:}]", "[]"},
		{"{choice:|none}", "none"},
		{"{choice:one}", "one"},
	} {
		if got := Expand(tc[0], Dynamic); got != tc[1] {
			t.Errorf("Expand(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

// TestNormalizeName pins the payload fold every stored-thing lookup shares.
// It moved here from the sesame scope package so that app/db can apply the
// same one without importing sesame; the rows are the ones that package
// already pinned, plus the '!' and case pair a chat author actually types.
func TestNormalizeName(t *testing.T) {
	for _, tc := range [][2]string{
		{"  !Deaths  ", "deaths"},
		{"  ", ""},
		{"A.B", "a.b"},
		{"!!twice", "!twice"},
	} {
		if got := NormalizeName(tc[0]); got != tc[1] {
			t.Errorf("NormalizeName(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}
