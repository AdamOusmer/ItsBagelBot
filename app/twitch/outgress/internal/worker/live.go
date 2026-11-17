// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"ItsBagelBot/pkg/bus"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type LiveWriter struct {
	client        valkey.Client
	invalidations *bus.Invalidations
	log           *zap.Logger
	ttl           time.Duration
}

func NewLiveWriter(client valkey.Client, h *bus.Invalidations, ttl time.Duration, logs ...*zap.Logger) *LiveWriter {
	if h == nil {
		h = bus.NewInvalidations(nil, "", nil)
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	log := zap.NewNop()
	if len(logs) > 0 {
		log = logs[0]
	}
	return &LiveWriter{client: client, invalidations: h, log: log, ttl: ttl}
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

	id, err := strconv.ParseUint(broadcasterID, 10, 64)
	if err != nil {
		w.log.Warn("skipping live cache invalidation: invalid broadcaster id", zap.String("broadcaster_id", broadcasterID), zap.Error(err))
		return nil
	}
	w.invalidations.Publish(invalidate.ScopeLive, id)
	return nil
}
