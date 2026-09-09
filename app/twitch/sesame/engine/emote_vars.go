// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"ItsBagelBot/app/twitch/sesame/automod"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/pkg/tmpl"
)

// EmoteCatalogFrom adapts the automod emote fetcher to the command lane's
// reader.
//
// A nil fetcher (the process runs with the emote refresher switched off)
// returns a nil INTERFACE rather than an interface holding a nil pointer, so
// Deps.Emotes compares equal to nil and the scope simply does not mount. That
// distinction is the whole reason this constructor exists instead of the
// assignment it replaces: a typed nil in the field would mount the scope, and
// the tokens would resolve to "" forever on a service that never loads a code,
// which reads as an empty emote set rather than as a token this deployment
// does not answer.
func EmoteCatalogFrom(f *automod.EmoteFetcher) scope.EmoteSource {
	if f == nil {
		return nil
	}
	return emoteCatalog{fetcher: f}
}

// emoteCatalog is the engine half of the emote scope: it reads the fetcher's
// last installed snapshot and hands it over in the scope's shape. It never
// fetches — the refresher owns that, on its own hourly ticker — so the read is
// one atomic load.
type emoteCatalog struct {
	fetcher *automod.EmoteFetcher
}

func (c emoteCatalog) Emotes() scope.EmoteSets {
	cat := c.fetcher.Catalog()
	return scope.EmoteSets{SevenTV: cat.SevenTV, BTTV: cat.BTTV, FFZ: cat.FFZ}
}

// emotesScope builds the {7tvemotes} / {bttvemotes} / {ffzemotes} /
// {random.emote} scope for one command run.
//
// Mounting is gated on the SOURCE being wired and on nothing else: no opt-in
// module sits behind these tokens. The catalog they read is refreshed for the
// automod gate, which runs for every channel this service moderates, so gating
// the tokens on a broadcaster's module row would leave them literal for a
// channel whose codes are already in memory. A deployment with the emote
// refresher off wires no source, and then they stay literal — which is honest,
// because that process will never have a code to print.
//
// toks is the lexed template because the bare {random.emote} spans have to be
// COUNTED here: each is an independent draw, and the chain hands a scope
// distinct spans.
func (p *Pipeline) emotesScope(toks []tmpl.Token) (scope.Emotes, bool) {
	if p.emotes == nil {
		return scope.Emotes{}, false
	}
	return scope.Emotes{Source: p.emotes, Draws: emoteDrawsOf(toks)}, true
}

// emoteDrawsOf counts the template's bare {random.emote} spans. A span
// carrying a payload is not one of them: it names nothing this token reads and
// stays literal, so counting it would pay for a draw nobody renders.
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
