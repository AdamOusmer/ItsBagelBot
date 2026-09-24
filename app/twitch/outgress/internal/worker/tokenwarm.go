// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"net/http"
	"time"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
	"ItsBagelBot/internal/domain/invalidate"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/pkg/codec"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

const (
	tokenWarmSlots   = 16
	tokenWarmTimeout = 10 * time.Second
)

func (w *Worker) SubscribeTokenWarm(nc *nats.Conn, prefix string) (*nats.Subscription, error) {
	return nc.Subscribe(prefix+"."+outgress.TokenWarmScope, func(msg *nats.Msg) {
		var dto invalidate.DTO
		if err := codec.Unmarshal(msg.Data, &dto); err != nil {
			w.log.Debug("token-warm: bad payload", zap.Error(err))
			return
		}
		if dto.BroadcasterID == "" {
			return
		}
		w.scheduleTokenWarm(dto.BroadcasterID)
	})
}

func (w *Worker) scheduleTokenWarm(broadcasterID string) {
	select {
	case w.tokenWarm <- struct{}{}:
	default:
		w.log.Warn("token-warm skipped: checks saturated", zap.String("broadcaster_id", broadcasterID))
		return
	}
	go func() {
		defer func() { <-w.tokenWarm }()
		ctx, cancel := context.WithTimeout(context.Background(), tokenWarmTimeout)
		defer cancel()
		w.warmBroadcasterToken(ctx, broadcasterID)
	}()
}

func (w *Worker) warmBroadcasterToken(ctx context.Context, broadcasterID string) {
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
