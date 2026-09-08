// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import "testing"

// pinTokens is the palette the pinning table resolves against. The ':' keys
// exist only to pin the one place the two legacy scanners disagreed.
var pinTokens = map[string]string{
	"user":  "bob",
	"title": "hi",
	"":      "EMPTY",
	"x:y":   "XY",
	"x:Y":   "XYUP",
}

// TestExpandPinsLegacyBehaviour pins, byte for byte, what the two scanners
// this package replaced produced for every brace and escape edge case: an
// unclosed '{', a bare '}', empty braces, nesting, adjacent tokens and a key
// carrying a ':' payload.
//
// The two originals (sesame module.Expand, outgress expandTokens) were run
// against this exact table before either was rewired. They agreed on all of it
// except the last two rows: outgress lowercased the whole key, so it read
// {x:Y} as x:y and answered "XY". sesame's name-only lowercasing is kept —
// the payload after ':' is data, not a name — and no outgress token has ever
// carried a payload, so no live reply changes.
func TestExpandPinsLegacyBehaviour(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"plain text", "plain text"},
		{"{user}", "bob"},
		{"hi {user}!", "hi bob!"},
		{"{USER}", "bob"},
		{"{User}", "bob"},
		{"{unknown}", "{unknown}"},
		{"{user", "{user"},
		{"{}", "EMPTY"},
		{"{{user}", "{{user}"},
		{"{{user}}", "{{user}}"},
		{"{a{user}", "{a{user}"},
		{"{user}{title}", "bobhi"},
		{"}{user}", "}bob"},
		{"{ user }", "{ user }"},
		{"{user}}", "bob}"},
		{"}}", "}}"},
		{"{", "{"},
		{"}", "}"},
		{"a{b}c{user}d", "a{b}cbobd"},
		{"{user}{", "bob{"},
		{"{us{er}", "{us{er}"},
		{"{user:}", "{user:}"},
		{"{:user}", "{:user}"},
		{"{x:y}", "XY"},
		{"{x:Y}", "XYUP"},
		{"{X:Y}", "XYUP"},
	}
	for _, tc := range cases {
		if got := Expand(tc.in, pinRepl); got != tc.want {
			t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func pinRepl(key string) (string, bool) {
	v, ok := pinTokens[key]
	return v, ok
}

// TestAppendKeepsCallerBuffer covers the pooled path sesame's hot loop uses:
// Append must write into the caller's slice and allocate nothing of its own.
func TestAppendKeepsCallerBuffer(t *testing.T) {
	dst := make([]byte, 0, 64)
	got := Append(append(dst, "pre "...), "{user} says {unknown}", pinRepl)
	if string(got) != "pre bob says {unknown}" {
		t.Fatalf("Append = %q", got)
	}
	if allocs := testing.AllocsPerRun(50, func() {
		_ = Append(dst[:0], "{user} and {title}", pinRepl)
	}); allocs != 0 {
		t.Errorf("Append allocated %v times per run, want 0", allocs)
	}
}
