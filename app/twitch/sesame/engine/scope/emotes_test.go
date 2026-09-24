// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakeCatalog struct {
	sets  EmoteSets
	reads int
}

func (f *fakeCatalog) Emotes() EmoteSets {
	f.reads++
	return f.sets
}

func loaded(sevenTV, bttv, ffz []string) *fakeCatalog {
	return &fakeCatalog{sets: EmoteSets{SevenTV: sevenTV, BTTV: bttv, FFZ: ffz}}
}

func emoteChain(cat EmoteSource, draws int, pick func(int) int) Chain {
	return Chain{Emotes{Source: cat, Draws: draws, Pick: pick}}
}

func TestEmoteListsRenderTheirOwnProvider(t *testing.T) {
	cat := loaded([]string{"PagMan", "Clap"}, []string{"KEKW"}, []string{"LUL", "ZULUL"})
	chain := emoteChain(cat, 0, nil)

	assert.Equal(t, "PagMan Clap", render(t, "{emotes:7tv}", chain, nil))
	assert.Equal(t, "KEKW", render(t, "{emotes:bttv}", chain, nil))
	assert.Equal(t, "LUL ZULUL", render(t, "{emotes:ffz}", chain, nil))
}

func TestEmoteListLegacyAliases(t *testing.T) {
	cat := loaded([]string{"PagMan", "Clap"}, []string{"KEKW"}, []string{"LUL", "ZULUL"})
	tests := []struct{ canonical, legacy string }{
		{"{emotes:7tv}", "{7tvemotes}"},
		{"{emotes:bttv}", "{bttvemotes}"},
		{"{emotes:ffz}", "{ffzemotes}"},
	}
	for _, tt := range tests {
		chain := emoteChain(cat, 0, nil)
		assert.Equal(t, render(t, tt.canonical, chain, nil), render(t, tt.legacy, chain, nil), tt.legacy)
	}

	chain := emoteChain(cat, 0, nil)
	assert.Equal(t, "PagMan Clap", render(t, "{emotes:7TV}", chain, nil), "the provider payload folds case")
}

func TestEmoteListsRenderEmptyRatherThanLiteral(t *testing.T) {
	for _, chain := range []Chain{
		emoteChain(loaded(nil, nil, nil), 0, nil),
		emoteChain(&fakeCatalog{}, 0, nil),
		emoteChain(nil, 0, nil),
	} {
		assert.Equal(t, "", render(t, "{7tvemotes}", chain, nil))
		assert.Equal(t, "none", render(t, "{7tvemotes|none}", chain, nil))
	}
}

func TestEmoteListTruncatesToOneChatLine(t *testing.T) {
	codes := make([]string, 200)
	for i := range codes {
		codes[i] = strings.Repeat("A", 9)
	}
	got := render(t, "{7tvemotes}", emoteChain(loaded(codes, nil, nil), 0, nil), nil)

	assert.LessOrEqual(t, len(got), MaxEmoteLine)
	assert.Greater(t, len(got), MaxEmoteLine-20, "the line should be filled, not cut early")
	for _, word := range strings.Fields(got) {
		assert.Equal(t, 9, len(word), "a code is never split")
	}
	assert.False(t, strings.HasSuffix(got, " "), "no trailing separator")
}

func TestRandomEmoteDrawsAcrossEveryLoadedSet(t *testing.T) {
	cat := loaded([]string{"PagMan"}, []string{"KEKW"}, []string{"LUL"})
	got := render(t, "{random.emote} {random.emote} {random.emote}",
		emoteChain(cat, 3, roundRobin()), nil)

	assert.Equal(t, "PagMan KEKW LUL", got)
}

func TestRandomEmoteRepeatsPastTheDrawCap(t *testing.T) {
	cat := loaded([]string{"A", "B", "C", "D"}, nil, nil)
	got := render(t, "{random.emote}{random.emote}{random.emote}{random.emote}",
		emoteChain(cat, 4, roundRobin()), nil)

	assert.Equal(t, "ABCC", got)
}

func TestRandomEmoteRendersEmptyWithNothingLoaded(t *testing.T) {
	chain := emoteChain(&fakeCatalog{}, 1, roundRobin())
	assert.Equal(t, "🥯", render(t, "{random.emote|🥯}", chain, nil))
}

func TestEmotesReadTheCatalogOnce(t *testing.T) {
	cat := loaded([]string{"PagMan", "Clap"}, nil, nil)
	got := render(t, "{7tvemotes} - {random.emote}", emoteChain(cat, 1, roundRobin()), nil)

	assert.Equal(t, "PagMan Clap - PagMan", got)
	assert.Equal(t, 1, cat.reads)
}

func TestEmoteSpansWithPayloadsStayLiteral(t *testing.T) {
	chain := emoteChain(loaded([]string{"PagMan"}, []string{"KEKW"}, nil), 1, roundRobin())
	for _, span := range []string{"{7tvemotes:100}", "{bttvemotes:global}", "{random.emote:7tv}"} {
		assert.Equal(t, span, render(t, span, chain, nil))
	}
}

func TestEmotesPlanOnlyWhatTheTemplateNames(t *testing.T) {
	vals, err := Emotes{Source: loaded([]string{"PagMan"}, nil, nil)}.
		Plan(t.Context(), []Var{{Name: BTTVEmotesToken}})
	assert.NoError(t, err)

	got, ok := vals.Get(Var{Name: SevenTVEmotesToken})
	assert.True(t, ok, "an unasked list still resolves, it is simply empty")
	assert.Equal(t, "", got)
}
