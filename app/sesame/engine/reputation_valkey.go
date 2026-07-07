package engine

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// ValkeyReputation stores a per-chatter automod strike counter at am:acct:<id>
// with a rolling TTL: a chatter's recent bad behaviour is remembered across pods
// for the window and forgotten when they go quiet (no database). Writes are
// best-effort and fire-and-forget, so a valkey blip degrades the signal rather
// than the message path.
type ValkeyReputation struct {
	client valkey.Client
	ttl    time.Duration
	log    *zap.Logger
}

func NewValkeyReputation(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyReputation {
	return &ValkeyReputation{client: client, ttl: ttl, log: log}
}

func repKey(chatterID string) string { return "am:acct:" + chatterID }

func (r *ValkeyReputation) Bump(ctx context.Context, chatterID string) {
	if chatterID == "" {
		return
	}
	key := repKey(chatterID)
	if err := r.client.Do(ctx, r.client.B().Incr().Key(key).Build()).Error(); err != nil {
		r.log.Debug("reputation incr failed", zap.String("chatter_id", chatterID), zap.Error(err))
		return
	}
	if err := r.client.Do(ctx, r.client.B().Expire().Key(key).Seconds(int64(r.ttl.Seconds())).Build()).Error(); err != nil {
		r.log.Debug("reputation expire failed", zap.String("chatter_id", chatterID), zap.Error(err))
	}
}

func (r *ValkeyReputation) Score(ctx context.Context, chatterID string) int {
	if chatterID == "" {
		return 0
	}
	n, err := r.client.Do(ctx, r.client.B().Get().Key(repKey(chatterID)).Build()).AsInt64()
	if err != nil {
		return 0 // missing key or backend error: treat as no reputation
	}
	return int(n)
}
