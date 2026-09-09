// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"sort"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/engine/scope"
	"ItsBagelBot/app/twitch/sesame/module"
	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// Game-stat token families for custom commands.
//
// Every external-stats module already renders its own command's reply through a
// palette (valRankTokens, fortniteStatsTokens, clashStatsTokens, the mcsr
// StringPalettes, urchin's inline maps). A custom command could not reach any of
// them, so a homemade "!rank" could not say {tier} without calling !val. What
// each module contributes here is that same palette under a prefix — {val.tier},
// {fn.kd}, {cr.trophies} — and nothing else: no second formatter, no second
// account resolution, no second cache.
//
// The prefix is what keeps them apart. Three modules answer a {player} and two
// answer an {elo}, so an unprefixed merge would have made the meaning of a token
// depend on which modules a broadcaster happens to have on.

// GameFamilies is every prefixed game-stat token family the modules contribute,
// for engine.Deps.Games. Order is irrelevant — the prefixes are disjoint — so it
// reads in the order the modules are registered in All.
//
// It takes no Deps: a family is a palette plus a route, and everything that
// varies per run (the gossip caller, the broadcaster's config row, the chat
// context) reaches it through engine.GameMount when the engine mounts it.
func GameFamilies() []engine.GameFamilySpec {
	specs := []engine.GameFamilySpec{
		valFamily(),
		fnFamily(),
	}
	specs = append(specs, crFamilies()...)
	return append(specs, mcsrFamilies()...)
}

// gameFamily is one prefixed family: which palette renders it, who a bare span
// is about, and what the upstream is asked for.
//
// The three strategy fields are deliberately the SAME shapes statsHandler's are
// (external.go), because they are filled with that handler's own strategies: a
// family reuses its command's target and request functions rather than
// restating them, so a payload goes through the very validation the typed
// argument does and a bare span resolves to the very account the command
// defaults to.
type gameFamily[C any, R any] struct {
	// prefix carries its trailing separator ("val."), the spelling the scope
	// splits token names on.
	prefix string
	// moduleName is the opt-in module row that gates the family: the row the
	// module's own commands check.
	moduleName string
	route      engine.GossipRoute
	fields     []string
	palette    func(statsCall[C], *R) scope.Palette
	target     func(statsCall[C]) statsSubject
	request    func(statsCall[C], statsSubject) gossiprpc.Request

	// empty reports a reply that carries nothing renderable: no session
	// baseline yet, no match ever played. It is the token twin of the
	// commands' empty-state override (externalCommand.special), and it exists
	// for the same reason: every numeric field would render zero, and a zero
	// reads as a wrong answer where an empty span reads as no answer and lets
	// the broadcaster's own fallback speak. nil means every reply renders.
	empty func(*R) bool
}

// spec hands the family to the engine.
func (f gameFamily[C, R]) spec() engine.GameFamilySpec {
	return engine.GameFamilySpec{
		Prefix: f.prefix,
		Module: f.moduleName,
		Fields: f.fields,
		Lookup: f.lookup,
	}
}

// lookup binds the family to one command run: the broadcaster's config blob is
// decoded ONCE here, at mount, and every span in that response reads the same
// linked account — where decoding per span would let one response resolve two
// different accounts if the row changed underneath it.
func (f gameFamily[C, R]) lookup(m engine.GameMount) scope.GameLookup {
	var cfg C
	if len(m.Config) > 0 {
		_ = codec.Unmarshal(m.Config, &cfg)
	}
	return func(ctx context.Context, player string) (scope.Palette, bool, error) {
		return f.read(ctx, m, statsCall[C]{Ctx: m.Ctx, Cfg: cfg, Args: player})
	}
}

// read runs one player's lookup.
//
// An unresolvable subject (a linked-only module with nothing linked, a payload
// that peels down to nothing) is found=false rather than a call with an empty
// account: the upstream would answer "player not found" for it, at the cost of
// a round trip, and the span renders the same empty either way.
func (f gameFamily[C, R]) read(ctx context.Context, m engine.GameMount, call statsCall[C]) (scope.Palette, bool, error) {
	subject := f.target(call)
	if subject.Account == "" {
		return nil, false, nil
	}
	var reply R
	if err := m.Gossip.Call(ctx, f.route, f.request(call, subject), &reply); err != nil {
		logGameLookup(m, f.prefix, subject.Display, err)
		return nil, false, err
	}
	if f.empty != nil && f.empty(&reply) {
		return nil, false, nil
	}
	return f.palette(call, &reply), true, nil
}

// logGameLookup reports a failed family lookup. It is logged HERE rather than in
// the scope because this is the side that knows which broadcaster and which
// upstream; the error is still returned so a test can pin that the span renders
// empty, and the scope treats it as the family's own nothing (see
// scope.GameLookup).
func logGameLookup(m engine.GameMount, prefix, display string, err error) {
	if m.Log == nil {
		return
	}
	m.Log.Warn("game token: lookup failed",
		zap.String("family", prefix), zap.String("player", display),
		module.BIDField(m.Ctx.BroadcasterID), zap.Error(err))
}

// linkedGameFamily is the shape every linked-account game family shares: the
// module's own palette over one gossip route, the payload (or the linked
// account) resolved through linkedTarget, and an account-only request.
//
// preferUUID follows the upstream exactly as it does for the commands: Hypixel
// requires a stored Mojang uuid and MCSR Ranked accepts one, while the
// name-keyed and tag-keyed providers (PaceMan, Fortnite, Clash Royale) must keep
// the typed handle.
func linkedGameFamily[C linkedConfig, R any](prefix, moduleName string, route engine.GossipRoute, tokens module.TokenExpander[R], preferUUID bool) gameFamily[C, R] {
	return gameFamily[C, R]{
		prefix:     prefix,
		moduleName: moduleName,
		route:      route,
		fields:     paletteFields(tokens),
		palette:    expandedPalette[C](tokens),
		target:     linkedTarget[C](preferUUID),
		request:    accountRequest[C],
	}
}

// expandedPalette renders a TokenExpander palette into the scope's map: the
// command's own accessors, run once each over one reply, so a token prints the
// byte-identical value the command's template does.
func expandedPalette[C any, R any](tokens module.TokenExpander[R]) func(statsCall[C], *R) scope.Palette {
	return func(_ statsCall[C], reply *R) scope.Palette {
		out := make(scope.Palette, len(tokens))
		for name, field := range tokens {
			out[name] = field(reply)
		}
		return out
	}
}

// paletteFields lists a TokenExpander's field names.
//
// Derived from the palette rather than spelled beside it, because the field list
// is what decides whether {val.teir} stays literal: a hand-written list would be
// free to drift from the palette it describes, and every entry it missed would
// be a token that silently never expands.
func paletteFields[R any](tokens module.TokenExpander[R]) []string {
	return sortedKeys(len(tokens), func(yield func(string)) {
		for name := range tokens {
			yield(name)
		}
	})
}

// stringPaletteFields is paletteFields for the already-rendered palettes (mcsr's
// locale-dependent ones). The palette it is given must be a sample built from a
// zero reply: only the keys are read.
func stringPaletteFields(sample module.StringPalette) []string {
	return sortedKeys(len(sample), func(yield func(string)) {
		for name := range sample {
			yield(name)
		}
	})
}

// sortedKeys collects names into a stable list. Sorted because map order is
// random and the list ends up in a Fields slice a test compares against.
func sortedKeys(size int, each func(func(string))) []string {
	out := make([]string, 0, size)
	each(func(name string) { out = append(out, name) })
	sort.Strings(out)
	return out
}
