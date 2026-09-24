// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl

import "testing"

var pinTokens = map[string]string{
	"user":  "bob",
	"title": "hi",
	"":      "EMPTY",
	"x:y":   "XY",
	"x:Y":   "XYUP",
}

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

func pinRepl(tok Token) (string, bool) {
	key := tok.Key()
	v, ok := pinTokens[key]
	return v, ok
}

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

func TestLexRoundTrips(t *testing.T) {
	for _, in := range []string{
		"", "plain", "{user}", "hi {user}!", "{user}{title}", "a{b}c{user}d",
		"{user", "{{user}}", "{}", "}{user}", "{user}}", "{1|everyone}",
		"{choice:a|b|c}", "{so:Name|nobody} and {x}",
	} {
		var got string
		for _, tok := range Lex(in) {
			if tok.Kind == KindLiteral {
				got += tok.Text
				continue
			}
			got += tok.Raw
		}
		if got != in {
			t.Errorf("Lex(%q) round-trips to %q", in, got)
		}
	}
}

func TestLexSplitsSpanParts(t *testing.T) {
	type parts struct {
		name, payload, fallback string
		hasPayload, hasFallback bool
		key                     string
	}
	cases := []struct {
		in   string
		want parts
	}{
		{"{User}", parts{name: "user", key: "user"}},
		{"{choice}", parts{name: "choice", key: "choice"}},
		{"{choice:}", parts{name: "choice", hasPayload: true, key: "choice:"}},
		{"{CHOICE:Hi,Yo}", parts{name: "choice", payload: "Hi,Yo", hasPayload: true, key: "choice:Hi,Yo"}},
		{"{1|everyone}", parts{name: "1", fallback: "everyone", hasFallback: true, key: "1"}},
		{"{1|}", parts{name: "1", hasFallback: true, key: "1"}},
		{"{so:Name|nobody}", parts{name: "so", payload: "Name", fallback: "nobody", hasPayload: true, hasFallback: true, key: "so:Name"}},
		{"{choice:a|b|c}", parts{name: "choice", payload: "a|b", fallback: "c", hasPayload: true, hasFallback: true, key: "choice:a|b"}},
	}
	for _, tc := range cases {
		toks := Lex(tc.in)
		if len(toks) != 1 || toks[0].Kind != KindVar {
			t.Fatalf("Lex(%q) = %#v, want one var token", tc.in, toks)
		}
		tok := toks[0]
		got := parts{
			name:        tok.Name,
			payload:     tok.Payload,
			fallback:    tok.Fallback,
			hasPayload:  tok.HasPayload,
			hasFallback: tok.HasFallback,
			key:         tok.Key(),
		}
		if got != tc.want {
			t.Errorf("Lex(%q) parts = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestResolveThreeWay(t *testing.T) {
	tok := Lex("{1|everyone}")[0]
	if got := tok.Resolve("", false); got != "{1|everyone}" {
		t.Errorf("unknown name resolved to %q", got)
	}
	if got := tok.Resolve("", true); got != "everyone" {
		t.Errorf("empty value resolved to %q", got)
	}
	if got := tok.Resolve("bob", true); got != "bob" {
		t.Errorf("value resolved to %q", got)
	}
	bare := Lex("{1}")[0]
	if got := bare.Resolve("", true); got != "" {
		t.Errorf("empty value with no fallback resolved to %q", got)
	}
}
