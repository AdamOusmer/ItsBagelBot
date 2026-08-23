// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"strings"

	"ItsBagelBot/app/sesame/module"
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

// reply emits one chat line. override is the broadcaster's customized
// template ("" for the fixed system lines); kv are {token},value pairs
// (token names without braces).
func (g gameReplier) reply(emit module.Emit, override string, key replyKey, tokens ...token) {
	tmpl := override
	if tmpl == "" {
		tmpl = i18n.T(g.c.Locale, string(key))
	}
	text := module.ExpandString(tmpl, func(k string) (string, bool) {
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
