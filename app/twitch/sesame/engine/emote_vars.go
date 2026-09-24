// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"
)

func EmoteCatalogFrom(f *automod.EmoteFetcher) scope.EmoteSource {
	if f == nil {
		return nil
	}
	return emoteCatalog{fetcher: f}
}

type emoteCatalog struct {
	fetcher *automod.EmoteFetcher
}

func (c emoteCatalog) Emotes() scope.EmoteSets {
	cat := c.fetcher.Catalog()
	return scope.EmoteSets{SevenTV: cat.SevenTV, BTTV: cat.BTTV, FFZ: cat.FFZ}
}

func (p *Pipeline) emotesScope(toks []tmpl.Token) (scope.Emotes, bool) {
	if p.emotes == nil {
		return scope.Emotes{}, false
	}
	return scope.Emotes{Source: p.emotes, Draws: emoteDrawsOf(toks)}, true
}

func emoteDrawsOf(toks []tmpl.Token) int {
	draws := 0
	for _, tok := range toks {
		if isBareRandomEmote(tok) {
			draws++
		}
	}
	return draws
}

func isBareRandomEmote(tok tmpl.Token) bool {
	return tok.Kind == tmpl.KindVar && tok.Name == scope.RandomEmoteToken && !tok.HasPayload
}
