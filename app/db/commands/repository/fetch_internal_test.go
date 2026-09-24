// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"testing"

	"ItsBagelBot/pkg/tmpl"
)

func lexOneSpan(t *testing.T, span string) tmpl.Token {
	t.Helper()
	toks := tmpl.Lex(span)
	if len(toks) != 1 || toks[0].Kind != tmpl.KindVar {
		t.Fatalf("Lex(%q) = %v, want one span", span, toks)
	}
	return toks[0]
}

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
