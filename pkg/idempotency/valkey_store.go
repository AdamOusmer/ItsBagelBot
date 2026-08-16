// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary and unlicensed. See LICENSE.md.

package idempotency

import (
	"context"
	"sync/atomic"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	valkey_go "github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// claimValue is the placeholder stored under a claim key. The value is never
// read; presence alone is the claim, so it is kept to one byte.
const claimValue = "1"

// ValkeyStore implements Store as a single SET NX PX on the fleet Valkey
// primary. SET ... NX is a write, so the shared client routes it to the
// Sentinel-elected master on its own; the explicit Primary pin makes that
// invariant local to this store and keeps it correct if a read-back is ever
// added (a claim tested against a lagging node-local replica could miss a
// just-written duplicate).
//
// It fails OPEN: on any Valkey error Seen returns not-seen so the caller runs
// the effect. A guard exists to suppress a rare double-apply, not to gate the
// live firehose behind Valkey's availability; a Valkey outage that dropped every
// event would be far worse than the duplicates the guard normally absorbs.
type ValkeyStore struct {
	client   valkey_go.Client
	prefix   string
	log      *zap.Logger
	failOpen atomic.Int64
}

// storedKey is a prefix-composed Valkey key: what this store actually writes,
// as opposed to the caller's bare key that Seen and Release receive. Only Key
// produces one, so a raw caller key can never reach a command builder by being
// the same underlying type as the composed one.
type storedKey string

// Key composes the Valkey key a claim is stored under: a store's prefix followed
// by the caller's key. It is exported and used by Seen/Release so the prefix-join
// convention lives in one place — a caller that folds a claim into its own atomic
// script (rather than going through Seen) writes the exact same byte string.
//
// It returns a plain string because it is the cross-package spelling of the
// convention; inside this store the result travels as a storedKey.
func Key(prefix, key string) string {
	return prefix + key
}

// NewValkeyStore builds a store keyed under prefix (the fleet convention is a
// colon-delimited namespace such as "sesame:seen:"). client is the shared fleet
// client; it is pinned to the primary here.
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

// Seen claims the key with SET <prefix+key> "1" NX PX <ttl>. A successful SET is
// a fresh claim (not a duplicate); a Valkey nil reply means NX declined because
// the key already existed (a duplicate); any other error fails open.
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

// Release deletes the claim key. A missing key (already expired, or never
// claimed because the store failed open) is not an error.
func (s *ValkeyStore) Release(ctx context.Context, key string) error {
	full := Key(s.prefix, key)
	err := s.client.Do(ctx, s.client.B().Del().Key(full).Build()).Error()
	if err != nil && !valkey_go.IsValkeyNil(err) {
		s.log.Debug("idempotency: claim release failed", zap.String("key", full), zap.Error(err))
		return err
	}
	return nil
}

// noteFailOpen counts a claim Valkey refused to answer and logs the first, then
// every thousandth. Sustained fail-open is exactly the case where a line per
// event would emit at the rate of the lane the guard is protecting.
func (s *ValkeyStore) noteFailOpen(key storedKey, err error) {
	n := s.failOpen.Add(1)
	if n == 1 || n%1000 == 0 {
		s.log.Warn("idempotency: valkey claim failed; admitting event (fail-open)",
			zap.String("key", string(key)), zap.Int64("fail_open_total", n), zap.Error(err))
	}
}

// FailOpenCount is the running number of Valkey failures the store admitted
// through. It exists so a caller can surface the fail-open rate as a metric.
func (s *ValkeyStore) FailOpenCount() int64 { return s.failOpen.Load() }

// setNXOutcome classifies a SET NX result. ok=false marks an infra failure the
// caller must fail open on; when ok is true, seen reports whether the key was a
// duplicate.
func setNXOutcome(err error) (seen, ok bool) {
	switch {
	case err == nil:
		return false, true // SET applied: a fresh claim
	case valkey_go.IsValkeyNil(err):
		return true, true // NX declined: the key already existed
	default:
		return false, false // infra failure: fail open
	}
}

// millis floors a ttl at one millisecond so a rounding-to-zero duration can
// never publish a claim with no expiry.
func millis(ttl time.Duration) int64 {
	ms := ttl.Milliseconds()
	if ms < 1 {
		return 1
	}
	return ms
}
