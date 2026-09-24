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

const claimValue = "1"

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
	cmd := s.client.B().Set().Key(string(full)).Value(claimValue).Nx().PxMilliseconds(millis(ttl)).Build()
	err := s.client.Do(ctx, cmd).Error()
	seen, ok := setNXOutcome(err)
	if !ok {
		s.noteFailOpen(full, err)
		return false, err
	}
	return seen, nil
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

func setNXOutcome(err error) (seen, ok bool) {
	switch {
	case err == nil:
		return false, true
	case valkey_go.IsValkeyNil(err):
		return true, true
	default:
		return false, false
	}
}

func millis(ttl time.Duration) int64 {
	ms := ttl.Milliseconds()
	if ms < 1 {
		return 1
	}
	return ms
}
