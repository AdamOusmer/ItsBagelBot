// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"strconv"
	"strings"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/event/lane"

	"go.uber.org/zap"
)

func mentionedViewerID(mentions []lane.Mention, login string) uint64 {
	for _, m := range mentions {
		if strings.EqualFold(m.UserLogin, login) {
			id, _ := strconv.ParseUint(m.UserID, 10, 64)
			return id
		}
	}
	return 0
}

type viewerTarget struct {
	login      string
	atPrefixed bool
}

type viewerResolver struct {
	accounts engine.TwitchAccountLookup
	log      *zap.Logger
}

func (r viewerResolver) resolve(ctx context.Context, mentions []lane.Mention, target viewerTarget) uint64 {
	if id := mentionedViewerID(mentions, target.login); id != 0 {
		return id
	}
	if target.atPrefixed {
		return 0
	}
	return r.lookup(ctx, target.login)
}

func (r viewerResolver) lookup(ctx context.Context, login string) uint64 {
	if r.accounts == nil {
		return 0
	}
	raw, found, err := r.accounts.ResolveLogin(ctx, login)
	if err != nil {
		r.log.Warn("login lookup failed", zap.String("login", login), zap.Error(err))
		return 0
	}
	if !found {
		return 0
	}
	id, _ := strconv.ParseUint(raw, 10, 64)
	return id
}
