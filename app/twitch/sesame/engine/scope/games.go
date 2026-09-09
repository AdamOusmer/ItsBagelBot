// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"strings"
)

// MaxGamePlayers bounds how many DISTINCT players one response may look up per
// game family.
//
// Decision record. Every distinct player is one gossip round trip to a
// third-party API on somebody else's rate-limit budget, spent on a command any
// viewer can run. Two is the cap because two is what the only shape that needs
// more than one asks for: a comparison line ("{val.rr} vs {val.rr:Frosty#EUW1}").
// A third player does not fit the sentence either — a 500-byte Twitch line
// carrying three players' tiers and ratings has no room left for the words
// between them — so paying a third upstream call buys nothing a broadcaster
// can print.
//
// It is counted PER FAMILY rather than per response, because the families are
// separate upstreams with separate budgets: a template that reads one Valorant
// player and one Fortnite player spends one call on each, and folding them into
// a single allowance would make adding a second game silently break the first.
//
// Past the cap a span renders EMPTY (so its fallback speaks) rather than
// staying literal, for the reason the named-channel cap does: the template is
// not wrong, it is only asking for more lookups than one chat line can carry,
// and a literal "{val.tier:Someone}" in chat reads as the bot not knowing the
// token at all.
const MaxGamePlayers = 2

// Palette is one player's resolved stat fields, keyed by the field name a token
// names after its prefix ("tier" for {val.tier}). It is module.StringPalette's
// shape on purpose: every game module already renders its command's reply into
// exactly this map, so a token and the command it borrows from cannot format
// the same number two ways.
type Palette map[string]string

// GameLookup resolves one player's stats for one family.
//
// player is what the span's payload named, already trimmed, and EMPTY means
// "whoever this broadcaster linked on the module page" — the family resolves
// that itself, through the very function its own chat command resolves a
// missing argument with, so a token and the command agree about whose stats
// they are.
//
// found=false is the resolved-but-nothing answer: an unlinked module, a payload
// that names nobody the upstream knows, a lookup that ran and came back empty.
// Every field of that family then renders "" and the span's fallback speaks.
// err is the same outcome for the reader; it is returned so the implementation
// can log with its own broadcaster fields rather than have this package invent
// a logger.
type GameLookup func(ctx context.Context, player string) (Palette, bool, error)

// GameFamily is one prefixed family of game-stat tokens: what the tokens are
// spelled with ("val."), which fields exist under it, and the lookup that fills
// them.
//
// Fields is what makes an unknown field under a KNOWN prefix stay literal:
// {val.tier} resolves and {val.teir} does not, so a typo is visible to the
// broadcaster who wrote it instead of quietly rendering empty forever.
type GameFamily struct {
	// Prefix includes its trailing separator ("val.", "cr.pol."), so Owns is a
	// string split rather than a re-derivation of the spelling.
	Prefix string
	// Fields are the names that resolve under Prefix, without it.
	Fields []string
	// Lookup runs one player's read. Never nil on a mounted family.
	Lookup GameLookup
}

// Games answers the game-stat token families a broadcaster has switched on.
//
// One scope holds every family rather than one scope per game, because they
// differ in nothing but their prefix, their field list and the route their
// lookup calls: the batching, the per-player deduplication and the cap are the
// same rules for all of them, and five copies of those rules would be five
// places for the cap to drift.
//
// Mounting stays per family: each is gated by its own opt-in module row, so a
// channel with Valorant on and Fortnite off expands {val.tier} and leaves
// {fn.kd} visible. A family that is not in this slice owns nothing, and its
// spans stay literal exactly like a typo.
type Games struct {
	Families []GameFamily
}

// Owns claims a name only when a mounted family carries that prefix AND that
// field.
func (g Games) Owns(name string) bool {
	_, _, ok := g.fieldOf(name)
	return ok
}

// fieldOf resolves a token name to the family that answers it and the field it
// asks for.
func (g Games) fieldOf(name string) (family int, field string, ok bool) {
	for i, fam := range g.Families {
		if rest, cut := fam.Field(name); cut {
			return i, rest, true
		}
	}
	return 0, "", false
}

// Field splits a token name against this family's prefix and field list,
// returning the field it names.
//
// It is exported because the engine has to recognize a family in a lexed
// template BEFORE it reads any per-broadcaster module row — so that a command
// naming no game token costs no projection read at all — and it must recognize
// it by exactly the rule this scope answers by. A second spelling on that side
// would be a family that is mounted and then owns nothing, or worse, one that
// spends a projection read on a typo.
//
// It is a method rather than a free function taking the prefix and the field
// list because those two travel together everywhere: a caller holding one
// without the other can only get the answer wrong, and the pair already has a
// name here.
func (f GameFamily) Field(name string) (string, bool) {
	rest, cut := strings.CutPrefix(name, f.Prefix)
	if !cut || !f.hasField(rest) {
		return "", false
	}
	return rest, true
}

func (f GameFamily) hasField(name string) bool {
	for _, have := range f.Fields {
		if have == name {
			return true
		}
	}
	return false
}

// Plan runs every lookup the template needs — once per family per player,
// before a byte is rendered.
//
// The batching is the deduplication: "{val.tier} · {val.rr} · peak
// {val.peaktier}" is three spans over ONE upstream call, and a fourth span
// naming another player is one more.
//
// It never returns an error. A family whose lookup failed is already the empty
// answer its own spans render (see GameLookup), and failing here would blank
// the families beside it: one broken third-party API must cost its own tokens,
// not the reply.
func (g Games) Plan(ctx context.Context, wants []Var) (Values, error) {
	out := &gameValues{games: g, palettes: make(map[gameKey]Palette, len(wants))}
	for _, want := range wants {
		g.planOne(ctx, out, want)
	}
	return out, nil
}

// planOne resolves one span's player unless an earlier span already did, or the
// family has spent its player budget.
func (g Games) planOne(ctx context.Context, out *gameValues, want Var) {
	key, ok := out.keyOf(want)
	if !ok {
		return
	}
	if _, done := out.palettes[key]; done {
		return
	}
	if !out.admit(key) {
		return
	}
	palette, found, _ := g.Families[key.family].Lookup(ctx, key.player)
	if !found {
		palette = Palette{}
	}
	out.palettes[key] = palette
}

// gameKey addresses one lookup: which family, and whose stats.
//
// The player half is the span's RAW payload rather than the account the family
// resolves it to, because only the family knows how a payload folds (a Riot ID
// carries a '#', a Clash Royale tag a '#' of its own meaning, and a
// linked-only module ignores the payload entirely). Two spellings of one player
// therefore cost two lookups; both are cached upstream by gossip, and folding
// them here would mean re-deriving each provider's identity rules in a package
// that must not know them.
type gameKey struct {
	family int
	player string
}

// gameValues is one run's resolved lookups.
type gameValues struct {
	games    Games
	palettes map[gameKey]Palette
	// spent counts the distinct players each family has already looked up, to
	// hold every family under MaxGamePlayers on its own.
	spent map[int]int
}

// keyOf reads which family and player a span addresses. ok=false marks a span
// no mounted family owns, which stays literal.
func (v *gameValues) keyOf(tok Var) (gameKey, bool) {
	family, _, ok := v.games.fieldOf(tok.Name)
	return gameKey{family: family, player: strings.TrimSpace(tok.Payload)}, ok
}

// admit reports whether a not-yet-read player may be looked up, charging it
// against that family's budget.
func (v *gameValues) admit(key gameKey) bool {
	if v.spent == nil {
		v.spent = make(map[int]int, len(v.games.Families))
	}
	if v.spent[key.family] >= MaxGamePlayers {
		return false
	}
	v.spent[key.family]++
	return true
}

// Get answers every span this scope owns. ok is true throughout for a span a
// mounted family claims: the lookup ran (or was deliberately not run, past the
// cap), and an empty result renders the span's fallback rather than the literal
// token, which would claim the bot has no such variable.
func (v *gameValues) Get(tok Var) (string, bool) {
	family, field, ok := v.games.fieldOf(tok.Name)
	if !ok {
		return "", false
	}
	key := gameKey{family: family, player: strings.TrimSpace(tok.Payload)}
	return v.palettes[key][field], true
}
