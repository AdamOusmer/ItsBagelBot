// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/invalidate"
	livekey "ItsBagelBot/internal/domain/live"

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

	setScript   *valkey.Lua
	clearScript *valkey.Lua
}

func NewLiveWriter(client valkey.Client, nc *nats.Conn, cacheInvalidatePrefix string, ttl time.Duration, log *zap.Logger) *LiveWriter {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &LiveWriter{
		client: client, nc: nc, cacheInvalidate: cacheInvalidatePrefix, ttl: ttl, log: log,
		setScript:   valkey.NewLuaScript(livekey.SetScript),
		clearScript: valkey.NewLuaScript(livekey.ClearScript),
	}
}

func (w *LiveWriter) Write(ctx context.Context, broadcasterID string, isLive bool) error {
	key := livekey.KeyString(broadcasterID)
	version := livekey.VersionNow()

	var err error
	if isLive {
		_, err = w.setScript.Exec(ctx, w.client, []string{key, livekey.VerKeyString(broadcasterID)}, []string{
			livekey.Value(version), strconv.FormatInt(int64(w.ttl.Seconds()), 10), strconv.FormatInt(int64(livekey.VerTTL.Seconds()), 10),
		}).AsInt64()
	} else {
		_, err = w.clearScript.Exec(ctx, w.client, []string{key, livekey.VerKeyString(broadcasterID)}, []string{
			livekey.Value(version), strconv.FormatInt(int64(livekey.VerTTL.Seconds()), 10),
		}).AsInt64()
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
