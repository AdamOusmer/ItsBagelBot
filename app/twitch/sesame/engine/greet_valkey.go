// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const greetKeyPrefix = "bagel:greeted:"

type ValkeyGreetStore struct {
	client valkey.Client
	ttlArg string
}

var firstGreetScript = valkey.NewLuaScript(`
local added = redis.call('SADD', KEYS[1], ARGV[1])
if added == 1 then redis.call('EXPIRE', KEYS[1], ARGV[2]) end
return added`)

func NewValkeyGreetStore(client valkey.Client, ttl time.Duration, log *zap.Logger) *ValkeyGreetStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	_ = log
	return &ValkeyGreetStore{client: client, ttlArg: strconv.FormatInt(int64(ttl.Seconds()), 10)}
}

func greetKey(id uint64) string { return cache.UserKey(greetKeyPrefix, id) }

func (s *ValkeyGreetStore) FirstGreet(ctx context.Context, broadcasterID uint64, chatterID string) (bool, error) {
	key := greetKey(broadcasterID)
	added, err := firstGreetScript.Exec(ctx, s.client, []string{key}, []string{
		chatterID, s.ttlArg,
	}).AsInt64()
	if err != nil {
		return false, err
	}
	return added > 0, nil
}

func (s *ValkeyGreetStore) ResetGreets(ctx context.Context, broadcasterID uint64) error {
	return s.client.Do(ctx, s.client.B().Del().Key(greetKey(broadcasterID)).Build()).Error()
}
