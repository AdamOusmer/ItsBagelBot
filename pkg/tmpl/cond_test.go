// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tmpl_test

import (
	"testing"

	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCondSplitsTestFromBranches(t *testing.T) {
	type split struct {
		refKey  string
		want    string
		hasWant bool
		then    string
		els     string
	}
	tests := []struct {
		in   string
		want split
	}{
		{"{if:user:hi}", split{refKey: "user", then: "hi"}},
		{"{if:user:hi:bye}", split{refKey: "user", then: "hi", els: "bye"}},
		{"{if:touser=bob:yes:no}", split{refKey: "touser", want: "bob", hasWant: true, then: "yes", els: "no"}},
		{"{if:1=:none:some}", split{refKey: "1", hasWant: true, then: "none", els: "some"}},
		{"{if:count:deaths:none:some}", split{refKey: "count:deaths", then: "none", els: "some"}},
		{"{if:count:deaths=0:none:some}", split{refKey: "count:deaths", want: "0", hasWant: true, then: "none", els: "some"}},
		{"{if:val.rank:one:two}", split{refKey: "val.rank", then: "one", els: "two"}},
		{"{IF:User:Hi}", split{refKey: "user", then: "Hi"}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			cond, ok := tmpl.Lex(tc.in)[0].Cond()

			require.True(t, ok, "must lex as a conditional")
			assert.Equal(t, tc.want, split{cond.Ref.Key(), cond.Want, cond.HasWant, cond.Then, cond.Else})
		})
	}
}

func TestMalformedOrUnknownCondStaysLiteral(t *testing.T) {
	unknown := func(tmpl.Token) (string, bool) { return "", false }
	tests := []struct {
		name         string
		in           string
		notCondToken bool
	}{
		{name: "bare if", in: "{if}", notCondToken: true},
		{name: "empty if", in: "{if:}", notCondToken: true},
		{name: "if with only a test", in: "{if:user}", notCondToken: true},
		{name: "a name that only starts with if", in: "{iffy:user:hi}", notCondToken: true},
		{name: "an unknown test", in: "{if:missing:x}"},
		{name: "an unknown test with an else", in: "{if:missing:x:y}"},
		{name: "an unknown equality test", in: "{if:missing=1:x:y}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, isCond := tmpl.Lex(tc.in)[0].Cond()

			assert.Equal(t, !tc.notCondToken, isCond)
			assert.Equal(t, tc.in, tmpl.Expand(tc.in, unknown))
		})
	}
}

func TestCondBranchesAreLiteralText(t *testing.T) {
	repl := lookup(map[string]string{"user": "bob", "touser": ""})

	assert.Equal(t, "hi bob", tmpl.Expand("{if:user:hi bob:hi nobody}", repl))
	assert.Equal(t, "nobody {touser}", tmpl.Expand("{if:touser:named:nobody {touser}}", repl))
}

func TestWithCondRefsAppendsOneRefPerConditional(t *testing.T) {
	plain := tmpl.Lex("hi {user}")
	assert.Len(t, tmpl.WithCondRefs(plain), len(plain), "a plain template must not grow")

	toks := tmpl.Lex("{if:followage:back again:hi} and {if:count:deaths=0:clean:messy}")
	got := tmpl.WithCondRefs(toks)

	require.Len(t, got, len(toks)+2)
	refs := got[len(toks):]
	assert.Equal(t, []string{"followage", "count:deaths"}, []string{refs[0].Key(), refs[1].Key()})
	assert.Equal(t, [3]any{"count", "deaths", true}, [3]any{refs[1].Name, refs[1].Payload, refs[1].HasPayload})
}
