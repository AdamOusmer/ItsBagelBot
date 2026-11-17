// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/pkg/cache"

	"go.uber.org/zap"
)

const (
	tokenWarmSlots   = 16
	tokenWarmTimeout = 10 * time.Second
)

func (w *Worker) WatchTokenWarm(h cache.Feed) {
	h.Watch(invalidate.ScopeTokenWarm, func(e cache.Event) {
		w.scheduleTokenWarm(strconv.FormatUint(e.ID, 10))
	})
}

func (w *Worker) scheduleTokenWarm(broadcasterID string) {
	if !w.backgroundAllowed(context.Background(), broadcasterID) {
		return
	}
	if !runBounded(w.tokenWarm, tokenWarmTimeout, func(ctx context.Context) { w.warmBroadcasterToken(ctx, broadcasterID) }) {
		w.log.Warn("token-warm skipped: checks saturated", zap.String("broadcaster_id", broadcasterID))
	}
}

func (w *Worker) warmBroadcasterToken(ctx context.Context, broadcasterID string) {
	if !w.backgroundAllowed(ctx, broadcasterID) {
		return
	}
	if err := w.takeSystemHelix(ctx); err != nil {
		w.log.Warn("token-warm: no system budget, skipping",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}

	res, err := w.callTwitch(ctx, twitch.IdentityBroadcaster, broadcasterID,
		twitch.HelixCall{Method: http.MethodGet, Endpoint: "/helix/users"})
	if err != nil {
		w.log.Warn("token-warm: broadcaster token unavailable",
			zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	drainResponse(res)

	w.log.Debug("token-warm: broadcaster token warmed",
		zap.String("broadcaster_id", broadcasterID))
}
