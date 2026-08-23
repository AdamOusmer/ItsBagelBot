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

// LiveWriter persists the result of a Twitch live re-check into the shared live
// projection and fans the change out to the worker fleet. It is the write-back
// side of the worker's key-expiry / cold-miss escalation: outgress owns the
// Twitch call, the worker owns reading the key.
type LiveWriter struct {
	client          valkey.Client
	nc              *nats.Conn
	cacheInvalidate string // core-NATS prefix; subject = prefix + "." + scope
	ttl             time.Duration
	log             *zap.Logger

	// The two writes apply through the shared versioned scripts
	// (internal/domain/live): the re-check result carries its own instant as
	// the version, so a stale stream.offline event landing after this write
	// cannot delete what Twitch just confirmed, whichever replica processes it.
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

// Write stores the live state for broadcasterID (versioned SET with TTL when
// live, conditional DEL when offline) and broadcasts a live cache invalidation
// so worker replicas drop their cached bool and read the fresh state.
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
