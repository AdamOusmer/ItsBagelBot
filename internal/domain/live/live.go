// Package live holds the shared contract for the broadcaster live-status
// projection: the Valkey key both the worker (reader/writer) and outgress (the
// Twitch re-check writer) agree on, and the cache-invalidation scope used to fan
// a live change to every worker replica. Keeping it here avoids the key format
// drifting between the two services that touch it.
package live

import "strconv"

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
