// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const greetKeyPrefix = "bagel:greeted:"

type ValkeyGreetStore struct {
	client valkey.Client
	ttl    time.Duration
}

func NewValkeyGreetStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyGreetStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	_ = log
	return &ValkeyGreetStore{client: client, ttl: ttl}
}

func greetKey(id uint64) string { return cache.UserKey(greetKeyPrefix, id) }

func (s *ValkeyGreetStore) FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error) {
	return pkg_valkey.FreshMemberSet{Key: greetKey(broadcasterID)}.Add(ctx, s.client, chatterID, s.ttl)
}

func (s *ValkeyGreetStore) ResetGreets(ctx context.Context, broadcasterID uint64) error {
	return pkg_valkey.DelKeys(ctx, s.client, greetKey(broadcasterID))
}
