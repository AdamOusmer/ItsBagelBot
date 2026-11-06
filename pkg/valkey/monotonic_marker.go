// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Out of order redelivery must never move the marker backward, and an expired target must never resurrect it.
var monotonicArmScript = NewScript(`
local current = redis.call('GET', KEYS[2])
if (current ~= false and tonumber(current) >= tonumber(ARGV[2])) or tonumber(ARGV[2]) <= tonumber(ARGV[1]) then
  return 0
end
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[3])
local pxat = tonumber(ARGV[1]) + tonumber(ARGV[5])
if pxat > tonumber(ARGV[2]) then pxat = tonumber(ARGV[2]) end
if redis.call('PEXPIRETIME', KEYS[1]) < pxat then redis.call('PEXPIREAT', KEYS[1], pxat) end
redis.call('SET', KEYS[2], ARGV[4], 'PXAT', pxat)
return 1`)

// MonotonicArm raises an indexed marker to expiresAtMS unless a later or equal value already holds it, or expiresAtMS has already passed. The key's own TTL stays capped at maxTTL from now; the stored value keeps the true expiry. It reports whether it armed the key.
func MonotonicArm(ctx context.Context, client valkey_go.Client, index, member, key, value string, expiresAtMS int64, maxTTL time.Duration) (bool, error) {
	now := time.Now()
	call := ScriptCall{
		Script: monotonicArmScript,
		Key:    index,
		Also:   []string{key},
		Args: []string{
			strconv.FormatInt(now.UnixMilli(), 10),
			strconv.FormatInt(expiresAtMS, 10),
			member,
			value,
			strconv.FormatInt(maxTTL.Milliseconds(), 10),
		},
	}
	n, err := RunScripts(ctx, client, []ScriptCall{call})[0].ToInt64()
	return n == 1, err
}
