// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package idempotency

import (
	"context"
	"sync/atomic"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type ValkeyStore struct {
	client   valkey_go.Client
	prefix   string
	log      *zap.Logger
	failOpen atomic.Int64
}

type storedKey string

// Callers claiming inside their own script must write exactly this key.
func Key(prefix, key string) string {
	return prefix + key
}

func NewValkeyStore(client valkey_go.Client, prefix string, log *zap.Logger) *ValkeyStore {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyStore{
		client: pkg_valkey.Primary(client),
		prefix: prefix,
		log:    log,
	}
}

func (s *ValkeyStore) Seen(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	full := storedKey(Key(s.prefix, key))
	won, err := pkg_valkey.ClaimOnce(ctx, s.client, string(full), time.Duration(millis(ttl))*time.Millisecond)
	if err != nil {
		s.noteFailOpen(full, err)
		return false, err
	}
	return !won, nil
}

func (s *ValkeyStore) Release(ctx context.Context, key string) error {
	full := Key(s.prefix, key)
	err := s.client.Do(ctx, s.client.B().Del().Key(full).Build()).Error()
	if err != nil && !valkey_go.IsValkeyNil(err) {
		s.log.Debug("idempotency: claim release failed", zap.String("key", full), zap.Error(err))
		return err
	}
	return nil
}

func (s *ValkeyStore) noteFailOpen(key storedKey, err error) {
	n := s.failOpen.Add(1)
	if n == 1 || n%1000 == 0 {
		s.log.Warn("idempotency: valkey claim failed; admitting event (fail-open)",
			zap.String("key", string(key)), zap.Int64("fail_open_total", n), zap.Error(err))
	}
}

func (s *ValkeyStore) FailOpenCount() int64 { return s.failOpen.Load() }

func millis(ttl time.Duration) int64 {
	ms := ttl.Milliseconds()
	if ms < 1 {
		return 1
	}
	return ms
}
