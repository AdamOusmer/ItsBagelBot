// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestNamespacedPalettesRetainLegacyAndPureFallback(t *testing.T) {
	const text = "{valorant:tier} {tier} {VALORANT:RR} {valorant:missing|empty} {codm:tier} {random:5-5} {choice:KeepCase}"
	const want = "Immortal Immortal 67 {valorant:missing|empty} {codm:tier} 5 KeepCase"
	palette := KV("tier", "Immortal", "rr", "67").WithNamespace("valorant").WithLocale(Locale("fr"))
	assert.Equal(t, want, palette.ExpandString(text))
	assert.Equal(t, want, StringPalette{"tier": "Immortal", "rr": "67"}.ExpandNamespaced("valorant", text))
	type reply struct{ tier string }
	tokens := TokenExpander[reply]{"tier": func(r *reply) string { return r.tier }, "rr": func(*reply) string { return "67" }}
	assert.Equal(t, want, tokens.ExpandNamespaced("valorant", text, &reply{tier: "Immortal"}))
	assert.Equal(t, "ranked", palette.ExpandString("{if:valorant:tier:ranked:unknown}"))
	assert.Equal(t, "Immortal", palette.Merge(KV("extra", "value")).ExpandString("{valorant:tier}"))
	assert.Equal(t, "{tier:payload}", palette.ExpandString("{tier:payload}"))
	// Main's utility scopes and localized output remain available behind module fields.
	assert.Equal(t, KV().WithLocale(Locale("fr")).ExpandString("{countup:2020-01-01}"), KV().WithNamespace("valorant").WithLocale(Locale("fr")).ExpandString("{countup:2020-01-01}"))
}
