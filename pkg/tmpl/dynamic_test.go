// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import (
	"strconv"
	"testing"
)

func one(t *testing.T, span string) Token {
	t.Helper()
	toks := Lex(span)
	if len(toks) != 1 || toks[0].Kind != KindVar {
		t.Fatalf("Lex(%q) = %v, want one span", span, toks)
	}
	return toks[0]
}

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

func rollInRange(text string, low, high int) bool {
	n, err := strconv.Atoi(text)
	return err == nil && n >= low && n <= high
}

func TestDynamicChoiceKeepsPayloadCase(t *testing.T) {
	got := Expand("{CHOICE:Hi}", Dynamic)
	if got != "Hi" {
		t.Errorf("Expand({CHOICE:Hi}) = %q, want %q", got, "Hi")
	}
}

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
