// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// A lower or equal version must never overwrite a higher one already recorded in VerKey.
var VersionedSlotSetScript = NewScript(`
local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[3]))
return 1`)

// Not retryable: a retried DEL racing a concurrent Set could delete a freshly confirmed key.
var VersionedSlotClearScript = NewScript(`
local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[2]))
return 1`)

// VersionedSlot is a value guarded by a monotonically increasing version stamped in a sibling key, so a stale write never overwrites a newer one.
type VersionedSlot struct {
	Key    string
	VerKey string
}

// SlotWrite is one versioned value and how long the value and its version stamp live.
type SlotWrite struct {
	Version int64
	TTL     time.Duration
	VerTTL  time.Duration
}

// Set stamps the write's version as the slot's value and as the version stamp, unless a higher version already holds the stamp.
func (s VersionedSlot) Set(ctx context.Context, client valkey_go.Client, write SlotWrite) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: VersionedSlotSetScript,
		Key:    s.Key,
		Also:   []string{s.VerKey},
		Args: []string{
			strconv.FormatInt(write.Version, 10),
			strconv.FormatInt(int64(write.TTL.Seconds()), 10),
			strconv.FormatInt(int64(write.VerTTL.Seconds()), 10),
		},
	}})[0].ToInt64()
	return n == 1, err
}

// Clear deletes the slot and stamps version as the version stamp for verTTL, unless a higher version already holds the stamp.
func (s VersionedSlot) Clear(ctx context.Context, client valkey_go.Client, version int64, verTTL time.Duration) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: VersionedSlotClearScript,
		Key:    s.Key,
		Also:   []string{s.VerKey},
		Args: []string{
			strconv.FormatInt(version, 10),
			strconv.FormatInt(int64(verTTL.Seconds()), 10),
		},
	}})[0].ToInt64()
	return n == 1, err
}
