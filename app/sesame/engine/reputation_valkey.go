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

func (r *ValkeyReputation) Bump(_ context.Context, chatterID string) {
	if chatterID == "" {
		return
	}
	key := repKey(chatterID)
	seconds := int64(r.ttl.Seconds())
	// Off the message path entirely (this used to await two sequential master
	// round trips inside the automod gate — up to ~13ms from a far node). The
	// strike counter is a best-effort signal; INCR and EXPIRE ride one
	// pipelined flush and any error only dims the signal.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, resp := range r.client.DoMulti(ctx,
			r.client.B().Incr().Key(key).Build(),
			r.client.B().Expire().Key(key).Seconds(seconds).Build(),
		) {
			if err := resp.Error(); err != nil {
				r.log.Debug("reputation bump failed", zap.String("chatter_id", chatterID), zap.Error(err))
				return
			}
		}
	}()
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
