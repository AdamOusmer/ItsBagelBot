// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// fakeEmotes is the loaded catalog without the fetcher: the tokens read a
// snapshot somebody else refreshed, so a test hands them one directly rather
// than standing up three HTTP servers.
type fakeEmotes struct {
	sets scope.EmoteSets
}

func (f fakeEmotes) Emotes() scope.EmoteSets { return f.sets }

// emotePipeline is one channel whose custom command names the emote tokens.
// source nil is the deployment with the emote refresher switched off, where
// every one of them stays literal.
func emotePipeline(t *testing.T, response string, source scope.EmoteSource) *Pipeline {
	t.Helper()
	d := Deps{
		Proj: fakeReader{
			cmd:      projection.Command{Name: "brag", Response: response, IsActive: true, Perm: "everyone"},
			cmdFound: true,
			modules:  map[string]projection.ModuleView{},
		},
		Live:     liveAlways{},
		Cooldown: NoopCooldown{},
		Pub:      &fakePublisher{},
		Log:      zap.NewNop(),
		Emotes:   source,
	}
	return NewPipeline(d, NewRegistry(zap.NewNop()), Config{
		OutgressPremium:  premiumSubj,
		OutgressStandard: standardSubj,
	})
}

func emoteCatalogOf(sevenTV, bttv, ffz []string) scope.EmoteSource {
	return fakeEmotes{sets: scope.EmoteSets{SevenTV: sevenTV, BTTV: bttv, FFZ: ffz}}
}

// Each token prints its own provider's codes, space-joined.
func TestEmoteListTokensPrintTheirProvider(t *testing.T) {
	source := emoteCatalogOf([]string{"PagMan", "Clap"}, []string{"KEKW"}, []string{"LUL"})
	p := emotePipeline(t, "7tv: {7tvemotes} bttv: {bttvemotes} ffz: {ffzemotes}", source)

	assert.Equal(t, "7tv: PagMan Clap bttv: KEKW ffz: LUL", expandViewer(t, p, "!brag"))
}

// No module gates these: a channel with nothing switched on still expands
// them, because the catalog behind them is refreshed for the automod gate.
func TestEmoteTokensNeedNoModule(t *testing.T) {
	p := emotePipeline(t, "{random.emote}", emoteCatalogOf([]string{"PagMan"}, nil, nil))
	assert.Equal(t, "PagMan", expandViewer(t, p, "!brag"))
}

// A source that is wired but holds nothing — a cold cache, moments after a
// deploy — renders empty, so a fallback speaks and the braces never reach
// chat. This is the case that must NOT be literal: it clears itself on the
// next refresh tick, and a template whose shape changed under it would read as
// a broken bot.
func TestEmoteTokensRenderTheirFallbackWhenNothingIsLoaded(t *testing.T) {
	p := emotePipeline(t, "{7tvemotes|nothing loaded} {random.emote|🥯}", fakeEmotes{})
	assert.Equal(t, "nothing loaded 🥯", expandViewer(t, p, "!brag"))
}

// A deployment with no emote refresher wires no source, and then the tokens
// are the unknown-token case: braces and all, because that process will never
// have a code to print.
func TestEmoteTokensStayLiteralWithoutASource(t *testing.T) {
	p := emotePipeline(t, "{7tvemotes} {random.emote}", nil)
	assert.Equal(t, "{7tvemotes} {random.emote}", expandViewer(t, p, "!brag"))
}

// None of them takes a payload, and a misspelling keeps its braces: the
// unknown-token rule, pinned beside the tokens that answer.
func TestEmoteTokensStayLiteralWhenMisspelled(t *testing.T) {
	p := emotePipeline(t, "{7tvemotes:100} {random.emote:7tv} {twitchemotes}",
		emoteCatalogOf([]string{"PagMan"}, nil, nil))

	assert.Equal(t, "{7tvemotes:100} {random.emote:7tv} {twitchemotes}", expandViewer(t, p, "!brag"))
}

// Two draws in one response are two independent picks, so the engine has to
// have counted both spans before the chain was built. With one code loaded the
// answer is fixed without pinning the dice.
func TestRandomEmoteDrawsPerSpan(t *testing.T) {
	p := emotePipeline(t, "{random.emote} {random.emote}", emoteCatalogOf([]string{"PagMan"}, nil, nil))
	assert.Equal(t, "PagMan PagMan", expandViewer(t, p, "!brag"))
}
