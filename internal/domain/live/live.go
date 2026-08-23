// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package live holds the shared contract for the broadcaster live-status
// projection: the Valkey key both the worker (reader/writer) and outgress (the
// Twitch re-check writer) agree on, and the cache-invalidation scope used to fan
// a live change to every worker replica. Keeping it here avoids the key format
// drifting between the two services that touch it.
package live

import (
	"strconv"
	"time"
)

// KeyPrefix is the per-broadcaster live key prefix: live:<broadcaster_id> = "1"
// while the stream is online; absence means not-known-live (offline or cold).
const KeyPrefix = "live:"

// InvalidateScope is the cache-invalidation scope (subject suffix) published when
// a live state changes, i.e. prefix + "." + InvalidateScope.
const InvalidateScope = "live"

// Key returns the live key for a broadcaster id.
func Key(id uint64) string { return KeyPrefix + strconv.FormatUint(id, 10) }

// KeyString returns the live key for a broadcaster id already in string form.
func KeyString(id string) string { return KeyPrefix + id }

// The key's VALUE is the applied version: the unix-millisecond instant of the
// stream event (Twitch EventSub message_timestamp) that last set it, or of the
// Twitch re-check that wrote it. Existence still means live; readers never
// parse the value (#561). Before versioning the value was the constant "1" and
// both writers used blind SET/DEL — so a rapid stream.online/offline pair,
// processed by different consumer goroutines, let SetLive land after ClearLive
// and resurrect a stale key with its 12h TTL, leaving every downstream reader
// treating an offline channel as live until natural expiry. Writers now apply
// through the two scripts below, which make each write conditional on the
// highest version already applied.
//
// The version lives under its OWN key (VerKey) rather than only inside the
// live key, because the DEL would otherwise destroy exactly the fact a later
// stale SET must be judged against: offline deletes the live key, and an old
// online redelivered afterwards would see no key, no version, and win. The
// version key survives both operations with its own TTL, so the ordering
// claim outlives every individual write.

// VerKeyPrefix holds the per-broadcaster last-applied version:
// ver:<broadcaster_id> = unix millis. It sorts under the shared live: prefix,
// which the expiry watcher and timer scans already skip (their id parse
// rejects anything non-numeric).
const VerKeyPrefix = KeyPrefix + "ver:"

// VerKey returns the version key for a broadcaster id.
func VerKey(id uint64) string { return VerKeyPrefix + strconv.FormatUint(id, 10) }

// VerKeyString returns the version key for a string broadcaster id.
func VerKeyString(id string) string { return VerKeyPrefix + id }

// VerTTL is how long a version claim outlives the write that made it. It must
// comfortably exceed the worst plausible event skew — EventSub redelivery plus
// lane queue backlog — because it is the whole memory the ordering has. Two
// days covers both many times over and keeps cold broadcasters from
// accumulating keys forever.
const VerTTL = 48 * time.Hour

// VersionNow is the fallback version for writers with no event timestamp (the
// projector write-back, outgress's authoritative re-check): the writer's own
// clock. Those writers ARE the authority at that instant, so now() is their
// correct ordering claim.
func VersionNow() int64 { return time.Now().UnixMilli() }

// Value renders a version as the key value stored in Valkey.
func Value(version int64) string { return strconv.FormatInt(version, 10) }

// SetScript applies SET live:<id> <version> EX <ttl> only when no newer
// version was already applied (checked and recorded in the companion ver: key).
// A legacy constant "1" live key carries no claim of its own — the ver: key is
// the sole arbiter — so rolling replicas converge without a flush.
// KEYS: live key, ver key. ARGV: version, ttl seconds, ver ttl seconds.
const SetScript = `local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('SET', KEYS[1], ARGV[1], 'EX', tonumber(ARGV[2]))
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[3]))
return 1`

// ClearScript applies DEL live:<id> only when the applied version is not newer
// than this event: a stale offline must not delete what a fresh re-check just
// confirmed, and it must still record its own recency so an OLDER online
// redelivered afterwards loses too. KEYS: live key, ver key. ARGV: version,
// ver ttl seconds.
const ClearScript = `local cur = redis.call('GET', KEYS[2])
if cur then
  local curv = tonumber(cur)
  if curv and curv > tonumber(ARGV[1]) then return 0 end
end
redis.call('DEL', KEYS[1])
redis.call('SET', KEYS[2], ARGV[1], 'EX', tonumber(ARGV[3]))
return 1`
