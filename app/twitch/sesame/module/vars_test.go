// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module_test

import (
	"fmt"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/pkg/tmpl"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandResolvesTokensThroughTheReplacer(t *testing.T) {
	repl := func(tok tmpl.Token) (string, bool) {
		switch tok.Key() {
		case "raider":
			return "CoolStreamer", true
		case "viewers":
			return "42", true
		case "user":
			return "sam", true
		case "choice:Hi,Yo":
			return "Hi", true
		default:
			return "", false
		}
	}
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"leaves unresolved tokens in place", "{raider} raided with {viewers}! {unknown}", "CoolStreamer raided with 42! {unknown}"},
		{"TestExpandKeyCaseInsensitive", "{User} says {CHOICE:Hi,Yo} to {USER}", "sam says Hi to sam"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, module.ExpandString(tt.in, repl))
			assert.Equal(t, tt.want, string(module.Expand(nil, tt.in, repl)))
		})
	}
}

func TestExpandDynamicTokensAreCaseInsensitive(t *testing.T) {
	assert.Equal(t, "5", module.ExpandString("{Random:5-5}", tmpl.Dynamic))
}

func TestStringPaletteExpandAndMerge(t *testing.T) {
	base := module.StringPalette{"player": "Feinberg", "elo": "1650"}
	merged := base.Merge(module.StringPalette{"elo": "1700"}, module.StringPalette{"rank": "12"})

	assert.Equal(t, "Feinberg: unrated · {rank}", module.StringPalette{"player": "Feinberg", "elo": "unrated"}.Expand("{player}: {elo} · {rank}"))
	assert.Equal(t, "5", base.Expand("{Random:5-5}"))
	assert.Equal(t, module.StringPalette{"player": "Feinberg", "elo": "1700", "rank": "12"}, merged)
	assert.Equal(t, module.StringPalette{"player": "Feinberg", "elo": "1650"}, base, "Merge must not mutate the receiver")
}

func TestKVNormalizesItsPairs(t *testing.T) {
	tests := []struct {
		name      string
		kv        []string
		template  string
		wantText  string
		wantNames []string
	}{
		{"drops a dangling name", []string{"a", "1", "dangling"}, "{a} {dangling}", "1 {dangling}", []string{"a"}},
		{"lets the last duplicate win without listing the name twice", []string{"a", "1", "a", "2"}, "{a}", "2", []string{"a"}},
		{"drops an empty name", []string{"", "x", "b", "2"}, "{b}", "2", []string{"b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := module.KV(tt.kv...)

			assert.Equal(t, tt.wantText, p.ExpandString(tt.template))
			assert.Equal(t, tt.wantNames, p.Names())
		})
	}
}

func TestPaletteRandomDiffersPerSpan(t *testing.T) {
	p := module.KV("user", "sam")
	for i := 0; i < 50; i++ {
		var a, b int
		_, err := fmt.Sscanf(p.ExpandString("{random:1-1000000} {random:1-1000000}"), "%d %d", &a, &b)
		require.NoError(t, err)
		if a != b {
			return
		}
	}
	t.Fatal("two {random} spans in one template never differed across 50 renders")
}

func TestPaletteWithLocaleWordsCountdownInFrench(t *testing.T) {
	en := module.KV("user", "sam").ExpandString("{countdown:9999-01-01}")
	fr := module.KV("user", "sam").WithLocale("fr").ExpandString("{countdown:9999-01-01}")

	assert.NotEqual(t, en, fr, "WithLocale(\"fr\") must change how {countdown} words itself")
}

func TestNamespacedPalettesRetainLegacyAndPureFallback(t *testing.T) {
	const text = "{valorant:tier} {tier} {VALORANT:RR} {valorant:missing|empty} {codm:tier} {random:5-5} {choice:KeepCase}"
	const want = "Immortal Immortal 67 {valorant:missing|empty} {codm:tier} 5 KeepCase"
	type reply struct{ tier string }
	palette := module.KV("tier", "Immortal", "rr", "67").WithNamespace("valorant").WithLocale(module.Locale("fr"))
	tokens := module.TokenExpander[reply]{"tier": func(r *reply) string { return r.tier }, "rr": func(*reply) string { return "67" }}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"palette", palette.ExpandString(text), want},
		{"string palette", module.StringPalette{"tier": "Immortal", "rr": "67"}.ExpandNamespaced("valorant", text), want},
		{"token expander", tokens.ExpandNamespaced("valorant", text, &reply{tier: "Immortal"}), want},
		{"namespaced conditional", palette.ExpandString("{if:valorant:tier:ranked:unknown}"), "ranked"},
		{"merged palette keeps the namespace", palette.Merge(module.KV("extra", "value")).ExpandString("{valorant:tier}"), "Immortal"},
		{"namespaced key does not answer a payload token", palette.ExpandString("{tier:payload}"), "{tier:payload}"},
		{
			"utility scopes and locale survive behind a namespace",
			module.KV().WithNamespace("valorant").WithLocale(module.Locale("fr")).ExpandString("{countup:2020-01-01}"),
			module.KV().WithLocale(module.Locale("fr")).ExpandString("{countup:2020-01-01}"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.got)
		})
	}
}
