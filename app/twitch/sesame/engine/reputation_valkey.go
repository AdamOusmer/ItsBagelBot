// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strings"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/idempotency"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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

func (r *ValkeyReputation) Observe(ctx context.Context, event ReputationEvent) (int, error) {
	if event.Viewer == "" {
		return 0, nil
	}
	receipt := ""
	if event.Identity != "" {
		receipt = "am:event:" + event.Identity + ":" + event.Viewer
	}
	ctx, cancel := context.WithTimeout(ctx, effectStoreTimeout)
	defer cancel()
	retention := bus.DeliveryEffectRetention(ctx)
	if retention == 0 {
		retention = bus.TwitchEffectRetention()
	}
	score, err := pkg_valkey.ObserveReputation(ctx, r.client, pkg_valkey.ReputationEvent{
		Account: repKey(event.Viewer), Receipt: receipt, TTL: r.ttl, ReplayTTL: retention,
	})
	return int(score), err
}

func (r *ValkeyReputation) Bump(ctx context.Context, chatterID string) {
	if chatterID == "" {
		return
	}
	chatterID = strings.Clone(chatterID)
	go idempotency.Async(ctx, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_, err := pkg_valkey.Incr(ctx, r.client, repKey(chatterID), r.ttl)
		if err != nil {
			r.log.Debug("reputation bump failed", zap.String("chatter_id", chatterID), zap.Error(err))
		}
		return err
	})()
}

func (r *ValkeyReputation) Score(ctx context.Context, chatterID string) int {
	if chatterID == "" {
		return 0
	}
	n, _ := pkg_valkey.GetInt(ctx, r.client, repKey(chatterID))
	return int(n)
}
