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
var setVersionedSlotScript = NewScript(`
local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[3]))
return 1`)

var clearVersionedSlotScript = NewScript(`
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

// Set stamps version as the slot's value for ttl and as the version stamp for verTTL, unless a higher version already holds the stamp.
func (s VersionedSlot) Set(ctx context.Context, client valkey_go.Client, version int64, ttl, verTTL time.Duration) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: setVersionedSlotScript,
		Key:    s.Key,
		Also:   []string{s.VerKey},
		Args: []string{
			strconv.FormatInt(version, 10),
			strconv.FormatInt(int64(ttl.Seconds()), 10),
			strconv.FormatInt(int64(verTTL.Seconds()), 10),
		},
	}})[0].ToInt64()
	return n == 1, err
}

// Clear deletes the slot and stamps version as the version stamp for verTTL, unless a higher version already holds the stamp.
func (s VersionedSlot) Clear(ctx context.Context, client valkey_go.Client, version int64, verTTL time.Duration) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: clearVersionedSlotScript,
		Key:    s.Key,
		Also:   []string{s.VerKey},
		Args: []string{
			strconv.FormatInt(version, 10),
			strconv.FormatInt(int64(verTTL.Seconds()), 10),
		},
	}})[0].ToInt64()
	return n == 1, err
}
