// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestEmoteListsRender(t *testing.T) {
	sets := func() *fakeCatalog {
		return loaded([]string{"PagMan", "Clap"}, []string{"KEKW"}, []string{"LUL", "ZULUL"})
	}
	tests := []struct {
		name     string
		chain    Chain
		template string
		want     string
	}{
		{"renders the 7tv list", emoteChain(sets(), 0, nil), "{emotes:7tv}", "PagMan Clap"},
		{"renders the bttv list", emoteChain(sets(), 0, nil), "{emotes:bttv}", "KEKW"},
		{"renders the ffz list", emoteChain(sets(), 0, nil), "{emotes:ffz}", "LUL ZULUL"},
		{"folds the provider payload case", emoteChain(sets(), 0, nil), "{emotes:7TV}", "PagMan Clap"},
		{"keeps the legacy 7tv alias", emoteChain(sets(), 0, nil), "{7tvemotes}", "PagMan Clap"},
		{"keeps the legacy bttv alias", emoteChain(sets(), 0, nil), "{bttvemotes}", "KEKW"},
		{"keeps the legacy ffz alias", emoteChain(sets(), 0, nil), "{ffzemotes}", "LUL ZULUL"},
		{"renders empty rather than literal for empty sets", emoteChain(loaded(nil, nil, nil), 0, nil), "{7tvemotes}", ""},
		{"renders the fallback for empty sets", emoteChain(loaded(nil, nil, nil), 0, nil), "{7tvemotes|none}", "none"},
		{"renders empty for an unloaded catalog", emoteChain(&fakeCatalog{}, 0, nil), "{7tvemotes}", ""},
		{"renders the fallback for an unloaded catalog", emoteChain(&fakeCatalog{}, 0, nil), "{7tvemotes|none}", "none"},
		{"renders empty without a catalog", emoteChain(nil, 0, nil), "{7tvemotes}", ""},
		{"renders the fallback without a catalog", emoteChain(nil, 0, nil), "{7tvemotes|none}", "none"},
		{
			name:     "draws a random emote across every loaded set",
			chain:    emoteChain(loaded([]string{"PagMan"}, []string{"KEKW"}, []string{"LUL"}), 3, roundRobin()),
			template: "{random.emote} {random.emote} {random.emote}",
			want:     "PagMan KEKW LUL",
		},
		{
			name:     "repeats the last draw past the draw cap",
			chain:    emoteChain(loaded([]string{"A", "B", "C", "D"}, nil, nil), 4, roundRobin()),
			template: "{random.emote}{random.emote}{random.emote}{random.emote}",
			want:     "ABCC",
		},
		{
			name:     "renders the random fallback with nothing loaded",
			chain:    emoteChain(&fakeCatalog{}, 1, roundRobin()),
			template: "{random.emote|🥯}",
			want:     "🥯",
		},
		{
			name:     "keeps a payloaded 7tv alias literal",
			chain:    emoteChain(loaded([]string{"PagMan"}, []string{"KEKW"}, nil), 1, roundRobin()),
			template: "{7tvemotes:100}",
			want:     "{7tvemotes:100}",
		},
		{
			name:     "keeps a payloaded bttv alias literal",
			chain:    emoteChain(loaded([]string{"PagMan"}, []string{"KEKW"}, nil), 1, roundRobin()),
			template: "{bttvemotes:global}",
			want:     "{bttvemotes:global}",
		},
		{
			name:     "keeps a payloaded random emote literal",
			chain:    emoteChain(loaded([]string{"PagMan"}, []string{"KEKW"}, nil), 1, roundRobin()),
			template: "{random.emote:7tv}",
			want:     "{random.emote:7tv}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, tt.chain, nil))
		})
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

func TestEmotesReadTheCatalogOnce(t *testing.T) {
	cat := loaded([]string{"PagMan", "Clap"}, nil, nil)

	assert.Equal(t, "PagMan Clap - PagMan", render(t, "{7tvemotes} - {random.emote}", emoteChain(cat, 1, roundRobin()), nil))
	assert.Equal(t, 1, cat.reads)
}

func TestEmotesPlanOnlyWhatTheTemplateNames(t *testing.T) {
	vals, err := Emotes{Source: loaded([]string{"PagMan"}, nil, nil)}.
		Plan(t.Context(), []Var{{Name: BTTVEmotesToken}})
	require.NoError(t, err)

	got, ok := vals.Get(Var{Name: SevenTVEmotesToken})
	assert.True(t, ok, "an unasked list still resolves, it is simply empty")
	assert.Equal(t, "", got)
}
