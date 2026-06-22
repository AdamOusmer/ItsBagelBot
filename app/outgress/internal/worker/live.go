package worker

import (
	"context"
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
}

func NewLiveWriter(client valkey.Client, nc *nats.Conn, cacheInvalidatePrefix string, ttl time.Duration, log *zap.Logger) *LiveWriter {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &LiveWriter{client: client, nc: nc, cacheInvalidate: cacheInvalidatePrefix, ttl: ttl, log: log}
}

// Write stores the live state for broadcasterID (SET with TTL when live, DEL when
// offline) and broadcasts a live cache invalidation so worker replicas drop their
// cached bool and read the fresh state.
func (w *LiveWriter) Write(ctx context.Context, broadcasterID string, isLive bool) error {
	key := livekey.KeyString(broadcasterID)

	var err error
	if isLive {
		err = w.client.Do(ctx, w.client.B().Set().Key(key).Value("1").ExSeconds(int64(w.ttl.Seconds())).Build()).Error()
	} else {
		err = w.client.Do(ctx, w.client.B().Del().Key(key).Build()).Error()
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
