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

type replyKey string

type chatReplier struct {
	c *module.Context

	points string
}

func newChatReplier(c *module.Context) chatReplier { return chatReplier{c: c} }

func newGameReplier(c *module.Context, pointsName string) chatReplier {
	return chatReplier{c: c, points: orDefault(strings.TrimSpace(pointsName), "points")}
}

func (g chatReplier) reply(emit module.Emit, override string, key replyKey, kv ...string) {
	line := override
	if line == "" {
		line = i18n.T(g.c.Locale, string(key))
	}
	p := module.Common(g.c)
	if g.points != "" {
		p = p.Merge(module.KV("points", g.points))
	}
	p = p.Merge(module.KV(kv...))
	text := p.ExpandString(line)
	emit(&module.Output{
		Type:          outgress.TypeChat,
		BroadcasterID: g.c.Env.BroadcasterUserID,
		Text:          text,
	})
}

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
