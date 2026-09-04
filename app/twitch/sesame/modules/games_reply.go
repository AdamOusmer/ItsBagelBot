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
)

// replyKey names one localized line of the wager games. A named type over
// the i18n key keeps the replier's callers from passing arbitrary strings
// where only a known key (or a broadcaster override) belongs.
type replyKey string

// token is one {placeholder},value pair handed to a reply. A named pair
// keeps the expansion surface typed instead of a loose key/value string
// slice.
type token struct {
	name  string
	value string
}

// tk builds one placeholder substitution.
func tk(name, value string) token { return token{name: name, value: value} }

// gameReplier is the shared voice of the wager games: every line is either
// the broadcaster's customized template for that reply or the localized
// default, expanded with per-line tokens plus the two constants ({user}, the
// invoking chatter, and {points}, the channel's currency word). Gamble and
// duel embed it so their handlers stay free of template plumbing.
type gameReplier struct {
	c      *module.Context
	points string
}

// newGameReplier builds the replier; an unset currency name falls back to
// plain "points".
func newGameReplier(c *module.Context, pointsName string) gameReplier {
	if strings.TrimSpace(pointsName) == "" {
		pointsName = "points"
	}
	return gameReplier{c: c, points: pointsName}
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
		return firstNonEmpty(strings.TrimSpace(fallback), "points"), true
	}
	cfg, on := engine.ReadLoyaltyConfig(ctx, d.Proj, c.BroadcasterID)
	if !on {
		return "", false
	}
	return cfg.Name(), true
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// reply emits one chat line. override is the broadcaster's customized
// template ("" for the fixed system lines); kv are {token},value pairs
// (token names without braces).
func (g gameReplier) reply(emit module.Emit, override string, key replyKey, tokens ...token) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(g.c.Locale, string(key))
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		// tokens is the variadic list this one call was given (never more
		// than a handful), so the scan is shorter than building a map would
		// be — and the map would be rebuilt for every reply anyway.
		for _, t := range tokens {
			if t.name == k {
				return t.value, true
			}
		}
		switch k {
		case "user":
			return g.c.Env.ChatterUserLogin, true
		case "points":
			return g.points, true
		}
		return module.ParseDynamic(k)
	})
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: g.c.Env.BroadcasterUserID,
		Text:          text,
	})
}
