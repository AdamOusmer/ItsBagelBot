// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strings"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/outgress"
)

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

// reply emits one chat line. override is the broadcaster's customized
// template ("" for the fixed system lines); kv are {token},value pairs
// (token names without braces).
func (g gameReplier) reply(emit module.Emit, override, key string, kv ...string) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(g.c.Locale, key)
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i] == k {
				return kv[i+1], true
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
