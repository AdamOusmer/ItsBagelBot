// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"testing"

	"ItsBagelBot/pkg/tmpl"
)

// lexOneSpan lexes a single-span template so the tables below can be written
// in the spans a broadcaster types rather than in tmpl.Token literals.
func lexOneSpan(t *testing.T, span string) tmpl.Token {
	t.Helper()
	toks := tmpl.Lex(span)
	if len(toks) != 1 || toks[0].Kind != tmpl.KindVar {
		t.Fatalf("Lex(%q) = %v, want one span", span, toks)
	}
	return toks[0]
}

// TestReferencesFetch pins the "which commands break if I delete this
// definition" scan against the spans a broadcaster actually types.
//
// The first two rows are the regressions that motivated replacing the
// hand-rolled substring scan: it looked for the literal "{urlfetch:" + name in
// a lower-cased response and accepted only '}' or '.' as the next byte, so it
// missed the '|' fallback grammar entirely (a definition every
// fallback-using command referenced reported ZERO referrers and deleted
// clean) and it never folded the CALLER's name, so a mixed-case argument
// matched nothing.
//
// The table is also what holds urlFetchTokenName to the spelling the sesame
// scope resolves: the rows are literal spans, not the constant.
func TestReferencesFetch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		response string
		want     string
		found    bool
	}{
		{"fallback ends the payload", "it is {urlfetch:weather|n/a} out", "weather", true},
		{"the name folds on both sides", "{URLFETCH:Weather.a}", "weather", true},
		{"a dot-path still names the definition", "{urlfetch:weather.main.temp}", "weather", true},
		{"a bare reference matches", "{urlfetch:weather}", "weather", true},
		{"a longer name is a different definition", "{urlfetch:weather2}", "weather", false},
		{"a prefix is a different definition", "{urlfetch:weath}", "weather", false},
		{"payloads are trimmed and unbanged", "{urlfetch: !Weather }", "weather", true},
		{"a payload-free span names nothing", "{urlfetch}", "weather", false},
		{"an empty payload names nothing", "{urlfetch:}", "weather", false},
		{"another token is not this one", "{counter:weather}", "weather", false},
		{"an unclosed brace is literal text", "{urlfetch:weather", "weather", false},
		{"the name is matched, not the text", "talking about urlfetch:weather", "weather", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := referencesFetch(tc.response, tc.want); got != tc.found {
				t.Errorf("referencesFetch(%q, %q) = %v, want %v", tc.response, tc.want, got, tc.found)
			}
		})
	}
}

// TestFetchDefNameDropsSelector pins that only the leading segment names the
// definition; the rest is a path INTO the fetched document.
func TestFetchDefNameDropsSelector(t *testing.T) {
	for _, tc := range [][2]string{
		{"{urlfetch:Weather.main.temp}", "weather"},
		{"{urlfetch:weather}", "weather"},
		{"{urlfetch:.leading}", ""},
		{"{urlfetch}", ""},
	} {
		tok := lexOneSpan(t, tc[0])
		if got := fetchDefName(tok); got != tc[1] {
			t.Errorf("fetchDefName(%s) = %q, want %q", tc[0], got, tc[1])
		}
	}
}
