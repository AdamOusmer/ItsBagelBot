// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// fakeGame answers a canned palette per player ("" being the linked account)
// and records every player it was asked for, so the batching claim ("one
// upstream call per player per response") is asserted rather than assumed.
type fakeGame struct {
	values map[string]Palette
	fail   error
	asked  []string
}

func (f *fakeGame) lookup(_ context.Context, player string) (Palette, bool, error) {
	f.asked = append(f.asked, player)
	if f.fail != nil {
		return nil, false, f.fail
	}
	palette, found := f.values[player]
	return palette, found, nil
}

// valFields is a stand-in for one module's palette: the fields a real family
// derives from the very TokenExpander its own chat command renders through.
var valFields = []string{"player", "peaktier", "rr", "tier"}

func gameFamilyOf(prefix string, fields []string, game *fakeGame) GameFamily {
	return GameFamily{Prefix: prefix, Fields: fields, Lookup: game.lookup}
}

func linkedPalette() *fakeGame {
	return &fakeGame{values: map[string]Palette{
		"": {"player": "Bagel#EUW", "tier": "Ascendant 2", "rr": "51", "peaktier": "Immortal 1"},
	}}
}

func TestGamesLeavesTokensLiteralWhenNoFamilyIsMounted(t *testing.T) {
	const template = "{val.tier} {fn.kd}"
	assert.Equal(t, template, render(t, template, Chain{Games{}}, nil),
		"an unmounted family owns nothing, so its spans stay literal like a typo")
}

func TestGamesRendersTheLinkedAccountOnce(t *testing.T) {
	game := linkedPalette()
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "Bagel#EUW is Ascendant 2 on 51 RR (peak Immortal 1)",
		render(t, "{val.player} is {val.tier} on {val.rr} RR (peak {val.peaktier})", chain, nil))
	assert.Equal(t, []string{""}, game.asked, "four spans, one lookup")
}

// A payload names another player; the family resolves what it means, so the
// scope only has to keep the two apart.
func TestGamesLooksUpAPayloadSeparately(t *testing.T) {
	game := linkedPalette()
	game.values["Frosty#EUW1"] = Palette{"tier": "Radiant"}
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "Ascendant 2 vs Radiant", render(t, "{val.tier} vs {val.tier:Frosty#EUW1}", chain, nil))
	assert.Equal(t, []string{"", "Frosty#EUW1"}, game.asked)
}

// An unknown field under a KNOWN prefix stays literal, which is what shows a
// broadcaster their typo instead of a blank that never fills in.
func TestGamesLeavesAnUnknownFieldLiteral(t *testing.T) {
	game := linkedPalette()
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "{val.teir} Ascendant 2", render(t, "{val.teir} {val.tier}", chain, nil))
	assert.Equal(t, []string{""}, game.asked, "a typo costs no upstream call")
}

// The pinned answer for "the lookup ran and the player is nobody the upstream
// knows": every field is empty, so the span's fallback speaks. Literal would
// claim the bot has no such variable.
func TestGamesRendersAnUnknownPlayerEmpty(t *testing.T) {
	game := &fakeGame{values: map[string]Palette{}}
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "no rank yet", render(t, "{val.tier|no rank yet}", chain, nil))
}

// A failed lookup is the same empty answer: one broken third-party API costs
// its own tokens, never the reply.
func TestGamesRendersAFailedLookupEmpty(t *testing.T) {
	game := &fakeGame{fail: errors.New("upstream down")}
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "unknown", render(t, "{val.tier|unknown}", chain, nil))
}

// A field the palette simply did not carry (an unranked reply that reports no
// peak) is empty too, not literal: the lookup ran.
func TestGamesRendersAMissingFieldEmpty(t *testing.T) {
	game := &fakeGame{values: map[string]Palette{"": {"tier": "Unranked"}}}
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "Unranked/none", render(t, "{val.tier}/{val.peaktier|none}", chain, nil))
}

// MaxGamePlayers holds a response to two distinct players per family; the
// third renders empty rather than literal, because the template is not wrong,
// it is only asking for more upstream calls than one chat line can carry.
func TestGamesCapsDistinctPlayersPerFamily(t *testing.T) {
	game := &fakeGame{values: map[string]Palette{
		"":  {"tier": "Ascendant 2"},
		"a": {"tier": "Radiant"},
		"b": {"tier": "Immortal 3"},
	}}
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "Ascendant 2 Radiant -",
		render(t, "{val.tier} {val.tier:a} {val.tier:b|-}", chain, nil))
	assert.Equal(t, []string{"", "a"}, game.asked, "the third player is never asked for")
}

// The cap is per family, because the families are separate upstreams with
// separate budgets: a second game must not be spending the first one's.
func TestGamesCapsEachFamilyOnItsOwn(t *testing.T) {
	val := &fakeGame{values: map[string]Palette{"": {"tier": "Ascendant 2"}, "a": {"tier": "Radiant"}}}
	fn := &fakeGame{values: map[string]Palette{"": {"kd": "1.42"}, "a": {"kd": "3.10"}}}
	chain := Chain{Games{Families: []GameFamily{
		gameFamilyOf("val.", valFields, val),
		gameFamilyOf("fn.", []string{"kd"}, fn),
	}}}

	assert.Equal(t, "Ascendant 2 Radiant 1.42 3.10",
		render(t, "{val.tier} {val.tier:a} {fn.kd} {fn.kd:a}", chain, nil))
	assert.Equal(t, []string{"", "a"}, val.asked)
	assert.Equal(t, []string{"", "a"}, fn.asked)
}

// Only the mounted families answer: a channel with Valorant on and Fortnite off
// expands one and leaves the other visible.
func TestGamesAnswersOnlyMountedFamilies(t *testing.T) {
	val := linkedPalette()
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, val)}}}

	assert.Equal(t, "Ascendant 2 {fn.kd}", render(t, "{val.tier} {fn.kd}", chain, nil))
}

// A payload is trimmed before it keys a lookup, so a template that spaces its
// spans out does not pay twice for one player.
func TestGamesTrimsThePayloadItKeysOn(t *testing.T) {
	game := &fakeGame{values: map[string]Palette{"Frosty#EUW1": {"tier": "Radiant"}}}
	chain := Chain{Games{Families: []GameFamily{gameFamilyOf("val.", valFields, game)}}}

	assert.Equal(t, "Radiant Radiant", render(t, "{val.tier: Frosty#EUW1 } {val.tier:Frosty#EUW1}", chain, nil))
	assert.Equal(t, []string{"Frosty#EUW1"}, game.asked)
}
