// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

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
		return 0
	}
	return int(n)
}
