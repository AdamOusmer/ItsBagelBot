// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package live

import (
	"strconv"
	"time"
)

// Write only via SetScript and ClearScript; a plain SET or DEL breaks version ordering.
const KeyPrefix = "live:"

const InvalidateScope = "live"

func Key(id uint64) string { return KeyPrefix + strconv.FormatUint(id, 10) }

func KeyString(id string) string { return KeyPrefix + id }

const VerKeyPrefix = KeyPrefix + "ver:"

func VerKey(id uint64) string { return VerKeyPrefix + strconv.FormatUint(id, 10) }

func VerKeyString(id string) string { return VerKeyPrefix + id }

const VerTTL = 48 * time.Hour

func VersionNow() int64 { return time.Now().UnixMilli() }

func Value(version int64) string { return strconv.FormatInt(version, 10) }

const SetScript = `local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[3]))
return 1`

const ClearScript = `local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[2]))
return 1`
