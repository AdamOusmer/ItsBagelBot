// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/nats-io/nats.go"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type LiveWriter struct {
	client          valkey.Client
	nc              *nats.Conn
	cacheInvalidate string
	ttl             time.Duration
	log             *zap.Logger
}

func NewLiveWriter(client valkey.Client, nc *nats.Conn, cacheInvalidatePrefix string, ttl time.Duration, log *zap.Logger) *LiveWriter {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &LiveWriter{client: client, nc: nc, cacheInvalidate: cacheInvalidatePrefix, ttl: ttl, log: log}
}

func (w *LiveWriter) Write(ctx context.Context, broadcasterID string, isLive bool) error {
	key := livekey.KeyString(broadcasterID)
	version := livekey.VersionNow()
	slot := pkg_valkey.VersionedSlot{Key: key, VerKey: livekey.VerKeyString(broadcasterID)}

	var err error
	if isLive {
		_, err = slot.Set(ctx, w.client, pkg_valkey.SlotWrite{Version: version, TTL: w.ttl, VerTTL: livekey.VerTTL})
	} else {
		_, err = slot.Clear(ctx, w.client, version, livekey.VerTTL)
	}
	if err != nil {
		return err
	}

	if w.nc != nil && w.cacheInvalidate != "" {
		if perr := invalidate.Publish(w.nc, w.cacheInvalidate, livekey.InvalidateScope, broadcasterID); perr != nil {
			w.log.Warn("live writer: failed to broadcast invalidation", zap.String("broadcaster_id", broadcasterID), zap.Error(perr))
		}
	}
	return nil
}
