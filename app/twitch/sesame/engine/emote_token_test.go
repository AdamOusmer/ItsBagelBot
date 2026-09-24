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

type fakeEmotes struct {
	sets scope.EmoteSets
}

func (f fakeEmotes) Emotes() scope.EmoteSets { return f.sets }

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

func TestEmoteListTokensPrintTheirProvider(t *testing.T) {
	source := emoteCatalogOf([]string{"PagMan", "Clap"}, []string{"KEKW"}, []string{"LUL"})
	p := emotePipeline(t, "7tv: {7tvemotes} bttv: {bttvemotes} ffz: {ffzemotes}", source)

	assert.Equal(t, "7tv: PagMan Clap bttv: KEKW ffz: LUL", expandViewer(t, p, "!brag"))
}

func TestEmoteTokensNeedNoModule(t *testing.T) {
	p := emotePipeline(t, "{random.emote}", emoteCatalogOf([]string{"PagMan"}, nil, nil))
	assert.Equal(t, "PagMan", expandViewer(t, p, "!brag"))
}

func TestEmoteTokensRenderTheirFallbackWhenNothingIsLoaded(t *testing.T) {
	p := emotePipeline(t, "{7tvemotes|nothing loaded} {random.emote|🥯}", fakeEmotes{})
	assert.Equal(t, "nothing loaded 🥯", expandViewer(t, p, "!brag"))
}

func TestEmoteTokensStayLiteralWithoutASource(t *testing.T) {
	p := emotePipeline(t, "{7tvemotes} {random.emote}", nil)
	assert.Equal(t, "{7tvemotes} {random.emote}", expandViewer(t, p, "!brag"))
}

func TestEmoteTokensStayLiteralWhenMisspelled(t *testing.T) {
	p := emotePipeline(t, "{7tvemotes:100} {random.emote:7tv} {twitchemotes}",
		emoteCatalogOf([]string{"PagMan"}, nil, nil))

	assert.Equal(t, "{7tvemotes:100} {random.emote:7tv} {twitchemotes}", expandViewer(t, p, "!brag"))
}

func TestRandomEmoteDrawsPerSpan(t *testing.T) {
	p := emotePipeline(t, "{random.emote} {random.emote}", emoteCatalogOf([]string{"PagMan"}, nil, nil))
	assert.Equal(t, "PagMan PagMan", expandViewer(t, p, "!brag"))
}
