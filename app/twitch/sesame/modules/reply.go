// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/tmpl"
)

// replyKey names one localized line. A named type over the i18n key keeps the
// replier's callers from passing arbitrary strings where only a known key
// belongs; the broadcaster's override travels as a separate argument.
type replyKey string

// chatReplier is the shared voice of the templated modules: every line is
// either the broadcaster's customized template for that reply or the localized
// default, expanded with the caller's {token} values plus the constants every
// line gets for free.
//
// Seven command types (quotes, queue, raffle, songqueue, loyalty, gamble,
// duel) had each grown the same pick-template / expand / emit body, drifting
// only in whether they accepted an override. Embedding it rather than passing
// it around works because all seven already carried the *module.Context it
// needs; a free function would have to be handed that context on every call.
type chatReplier struct {
	c *module.Context

	// points is what {points} expands to: the channel's currency word, set
	// only by the wager games. Blank leaves the token to the dynamic palette,
	// so a quotes or queue template that happens to spell {points} keeps
	// reading as the literal text it always did.
	points string
}

// newChatReplier is the plain voice: localized defaults, broadcaster
// overrides, no currency word.
func newChatReplier(c *module.Context) chatReplier { return chatReplier{c: c} }

// newGameReplier is the wager games' voice: the same lines plus {points}. An
// unset currency name falls back to plain "points".
func newGameReplier(c *module.Context, pointsName string) chatReplier {
	return chatReplier{c: c, points: orDefault(strings.TrimSpace(pointsName), "points")}
}

// reply emits one chat line. override is the broadcaster's customized template
// ("" for the fixed system lines, or an uncustomized customizable one); when
// empty the localized default for key is used. kv are {token},value pairs
// (token names without braces); {user} (the invoking chatter) and the generic
// dynamic vars ({random}, {choice:…}) are always available, so a customized
// template can use them too.
func (g chatReplier) reply(emit module.Emit, override string, key replyKey, kv ...string) {
	line := override
	if line == "" {
		line = i18n.T(g.c.Locale, string(key))
	}
	text := module.ExpandString(line, func(tok tmpl.Token) (string, bool) {
		name := tok.Key()
		// kv is the variadic list this one call was given (never more than a
		// handful), so the scan is shorter than building a map would be — and
		// the map would be rebuilt for every reply anyway.
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == name {
				return kv[i+1], true
			}
		}
		switch {
		case name == "user":
			return g.c.Env.ChatterUserLogin, true
		case name == "points" && g.points != "":
			return g.points, true
		}
		return tmpl.Dynamic(tok)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: g.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

// loyaltyVoice is the currency word the wager games speak, and the runtime
// half of "gamble/duel cannot run without loyalty". The dashboard is the
// authoring half (nested toggles refuse to enable while loyalty is off).
//
// A nil projector skips the enable check: every existing game test wires
// Loyalty/Duel without Proj, and adding a fake projector to each one would
// only restate this gate. Production always has Proj. When Proj is set, an
// off or missing loyalty module makes the game inert even if its own row
// is still enabled (a stale flip, a forged write). The name always comes
// from loyalty when it is on, so a leftover pointsName on the game blob
// cannot drift from the ledger word.
func loyaltyVoice(ctx context.Context, d engine.Deps, c *module.Context, fallback string) (name string, ok bool) {
	if d.Proj == nil {
		return orDefault(strings.TrimSpace(fallback), "points"), true
	}
	cfg, on := engine.ReadLoyaltyConfig(ctx, d.Proj, c.BroadcasterID)
	if !on {
		return "", false
	}
	return cfg.Name(), true
}
