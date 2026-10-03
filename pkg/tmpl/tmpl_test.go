// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl_test

import (
	"testing"

	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func lookup(values map[string]string) func(tmpl.Token) (string, bool) {
	return func(tok tmpl.Token) (string, bool) {
		val, ok := values[tok.Key()]
		return val, ok
	}
}

func TestLexRoundTrips(t *testing.T) {
	for _, in := range []string{
		"", "plain", "{user}", "hi {user}!", "{user}{title}", "a{b}c{user}d",
		"{user", "{{user}}", "{}", "}{user}", "{user}}", "{1|everyone}",
		"{choice:a|b|c}", "{so:Name|nobody} and {x}",
		"}}", "{", "}", "{us{er}", "{user:}", "{:user}",
	} {
		t.Run(in, func(t *testing.T) {
			var got string
			for _, tok := range tmpl.Lex(in) {
				if tok.Kind == tmpl.KindLiteral {
					got += tok.Text
					continue
				}
				got += tok.Raw
			}

			assert.Equal(t, in, got)
		})
	}
}

func TestLexSplitsSpanParts(t *testing.T) {
	type parts struct {
		name, payload, fallback string
		hasPayload, hasFallback bool
		key                     string
	}
	tests := []struct {
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
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			toks := tmpl.Lex(tc.in)
			require.Len(t, toks, 1)
			require.Equal(t, tmpl.KindVar, toks[0].Kind)
			tok := toks[0]

			assert.Equal(t, tc.want, parts{
				name: tok.Name, payload: tok.Payload, fallback: tok.Fallback,
				hasPayload: tok.HasPayload, hasFallback: tok.HasFallback, key: tok.Key(),
			})
		})
	}
}

func TestResolveChoosesBetweenValueFallbackAndLiteral(t *testing.T) {
	tests := []struct {
		name  string
		span  string
		value string
		known bool
		want  string
	}{
		{name: "leaves an unknown name literal", span: "{1|everyone}", want: "{1|everyone}"},
		{name: "renders the fallback for an empty value", span: "{1|everyone}", known: true, want: "everyone"},
		{name: "renders the value over the fallback", span: "{1|everyone}", value: "bob", known: true, want: "bob"},
		{name: "renders nothing for an empty value without a fallback", span: "{1}", known: true, want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tmpl.Lex(tc.span)[0].Resolve(tc.value, tc.known))
		})
	}
}

func TestAppendKeepsCallerBufferWithoutAllocating(t *testing.T) {
	repl := lookup(map[string]string{"user": "bob", "title": "hi"})
	dst := make([]byte, 0, 64)

	got := tmpl.Append(append(dst, "pre "...), "{user} says {unknown}", repl)

	assert.Equal(t, "pre bob says {unknown}", string(got))
	assert.Zero(t, testing.AllocsPerRun(50, func() {
		_ = tmpl.Append(dst[:0], "{user} and {title}", repl)
	}))
}

func TestNormalizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  !Deaths  ", "deaths"},
		{"  ", ""},
		{"A.B", "a.b"},
		{"!!twice", "!twice"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, tmpl.NormalizeName(tc.in))
		})
	}
}
