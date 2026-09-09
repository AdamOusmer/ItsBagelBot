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

func pinRepl(tok Token) (string, bool) {
	key := tok.Key()
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

// TestLexRoundTrips pins the lexer's structural invariant: concatenating every
// token's literal Text and span Raw reproduces the input exactly. It is what
// lets a caller plan work off the token list and still render the original
// bytes for anything it did not resolve.
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

// TestLexSplitsSpanParts pins the parts a scope chain plans against: a
// lowercased name, a case-preserved payload that knows whether it exists at
// all, and a fallback cut at the LAST pipe.
func TestLexSplitsSpanParts(t *testing.T) {
	// The parts only mean anything together, so the expectation is one
	// comparable value and the assertion is one equality. Comparing the six
	// fields one at a time made this loop complex enough to trip the health
	// gate, and the per-field messages said less than a %+v diff does.
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

// TestResolveThreeWay pins the render rule every surface shares: an unknown
// name keeps its braces, an empty value falls back, anything else renders.
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
