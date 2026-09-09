// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeCatalog answers one canned snapshot and counts the reads, so "the
// catalog is read once per run" is asserted rather than assumed.
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

	assert.Equal(t, "PagMan Clap", render(t, "{7tvemotes}", chain, nil))
	assert.Equal(t, "KEKW", render(t, "{bttvemotes}", chain, nil))
	assert.Equal(t, "LUL ZULUL", render(t, "{ffzemotes}", chain, nil))
}

// A set that loaded EMPTY and a catalog that has not refreshed yet answer the
// same way: "" (so the fallback speaks), never the literal span. See the Get
// contract — a literal would claim the bot has no such variable, permanently,
// for a state that clears on the next refresh tick.
func TestEmoteListsRenderEmptyRatherThanLiteral(t *testing.T) {
	for _, chain := range []Chain{
		emoteChain(loaded(nil, nil, nil), 0, nil), // loaded, nothing in it
		emoteChain(&fakeCatalog{}, 0, nil),        // cold cache
		emoteChain(nil, 0, nil),                   // no source at all
	} {
		assert.Equal(t, "", render(t, "{7tvemotes}", chain, nil))
		assert.Equal(t, "none", render(t, "{7tvemotes|none}", chain, nil))
	}
}

// The list is truncated on a CODE boundary, never inside a code: half a code
// is a word chat renders as text.
func TestEmoteListTruncatesToOneChatLine(t *testing.T) {
	codes := make([]string, 200)
	for i := range codes {
		codes[i] = strings.Repeat("A", 9) // 9 bytes + a space = 10 per code
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
	// roundRobin walks the pool in order: 7TV first, then BTTV, then FFZ.
	got := render(t, "{random.emote} {random.emote} {random.emote}",
		emoteChain(cat, 3, roundRobin()), nil)

	assert.Equal(t, "PagMan KEKW LUL", got)
}

// Independent draws, capped at three: past the cap the last drawn code repeats
// rather than the span rendering empty (MaxEmoteDraws).
func TestRandomEmoteRepeatsPastTheDrawCap(t *testing.T) {
	cat := loaded([]string{"A", "B", "C", "D"}, nil, nil)
	got := render(t, "{random.emote}{random.emote}{random.emote}{random.emote}",
		emoteChain(cat, 4, roundRobin()), nil)

	assert.Equal(t, "ABCC", got)
}

// Nothing loaded means nothing to draw: the span renders empty so its fallback
// speaks, exactly as an empty room does for {random.chatter}.
func TestRandomEmoteRendersEmptyWithNothingLoaded(t *testing.T) {
	chain := emoteChain(&fakeCatalog{}, 1, roundRobin())
	assert.Equal(t, "🥯", render(t, "{random.emote|🥯}", chain, nil))
}

// One read per run, even when the template names a list and a draw: two reads
// of a catalog the refresher swaps could print a drawn code that is not in the
// list beside it.
func TestEmotesReadTheCatalogOnce(t *testing.T) {
	cat := loaded([]string{"PagMan", "Clap"}, nil, nil)
	got := render(t, "{7tvemotes} - {random.emote}", emoteChain(cat, 1, roundRobin()), nil)

	assert.Equal(t, "PagMan Clap - PagMan", got)
	assert.Equal(t, 1, cat.reads)
}

// None of the four takes a payload, so a span carrying one stays literal and
// shows the author their typo.
func TestEmoteSpansWithPayloadsStayLiteral(t *testing.T) {
	chain := emoteChain(loaded([]string{"PagMan"}, []string{"KEKW"}, nil), 1, roundRobin())
	for _, span := range []string{"{7tvemotes:100}", "{bttvemotes:global}", "{random.emote:7tv}"} {
		assert.Equal(t, span, render(t, span, chain, nil))
	}
}

// A template naming only a list never builds a draw pool, and a template
// naming only a draw joins no list.
func TestEmotesPlanOnlyWhatTheTemplateNames(t *testing.T) {
	vals, err := Emotes{Source: loaded([]string{"PagMan"}, nil, nil)}.
		Plan(t.Context(), []Var{{Name: BTTVEmotesToken}})
	assert.NoError(t, err)

	got, ok := vals.Get(Var{Name: SevenTVEmotesToken})
	assert.True(t, ok, "an unasked list still resolves, it is simply empty")
	assert.Equal(t, "", got)
}
