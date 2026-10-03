// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automod

import (
	"testing"

	"ItsBagelBot/internal/moderation"

	"github.com/stretchr/testify/require"
)

var (
	zwsp = string(rune(0x200b))
	cyrA = string(rune(0x0430))
)

type fakeVocab struct{ codes map[string]struct{} }

func (f fakeVocab) Known(_ uint64, code string) bool { _, ok := f.codes[code]; return ok }

func newGateWithEmotes(codes ...string) *Gate {
	g := New()
	g.SetEmotes(NewEmoteSet(codes))
	return g
}

func newTestBaseline() *Baseline {
	b := NewBaseline(DefaultCeiling())
	b.nowUnix = func() int64 { return 1_800_000_000 }
	return b
}

func newTestVocab() *Vocab {
	v := NewVocab()
	v.nowUnix = func() int64 { return 1_800_000_000 }
	return v
}

func floorTerm(t *testing.T) string {
	t.Helper()
	terms := EmbeddedLexicon().Terms(moderation.CatHate)
	require.NotEmpty(t, terms, "embedded hate list is empty")
	return terms[0]
}
