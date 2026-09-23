// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	"ItsBagelBot/pkg/cache"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// chattersSnapshotPrefix is the shared, replica-wide chat-list cache behind
// {random.viewer}: chatters:<broadcaster_id>. It is deliberately not under
// the am:* automod-state family — it has nothing to do with moderation — nor
// under the per-replica roster (roster.go), which answers a different
// question ("who has spoken") from local memory alone.
const chattersSnapshotPrefix = "chatters:"

// chattersFetchLockPrefix guards the one-fetch-per-channel rule: many
// replicas can miss the snapshot for the same channel in the same
// millisecond (every one of them just ran a command naming {random.viewer}),
// and without this only the FIRST of them should pay for a Get Chatters
// call. SET NX is the cross-replica lock; pkg/cache's own singleflight only
// dedups within one process.
const chattersFetchLockPrefix = "chatters:fetch:"

// chattersSnapshotTTL is derived from the loyalty watch tick's own schedule
// (watchTickInterval + watchTickJitter, loyalty_tick.go) rather than a
// second, independently-tuned number.
//
// Decision record: staleness is bounded by the tick interval, not by this
// constant — the tick write-warms every live channel's snapshot on its own
// schedule (accrue), so a TTL shorter than that interval would let the
// snapshot go cold BETWEEN ticks on a channel the tick is actively keeping
// warm, forcing a redundant lazy fetch for no freshness gained (a prior 90s
// value did exactly this against the 5-minute tick). Setting the TTL to the
// tick's own worst-case period means a live channel's snapshot survives
// gap-free from one tick to the next; the lazy fetch (ViewerRPC) exists for
// channels the tick has not reached yet (not live, or newly live), not to
// keep a live channel's snapshot fresher than the tick already does.
const chattersSnapshotTTL = watchTickInterval + watchTickJitter

const (
	// chattersFetchLockTTL bounds how long one replica's fetch attempt blocks
	// every other replica's: longer than the RPC timeout the fetch itself
	// uses (see viewerFetchRPCTimeout) so a slow paginated fetch is not
	// raced by a second one before the first could plausibly have finished,
	// short enough that a fetch that silently died (a crash mid-fetch)
	// unblocks the next command's attempt well inside one snapshot TTL.
	chattersFetchLockTTL = 10 * time.Second
	// chattersSnapshotCap bounds the stored list: {random.viewer} draws one
	// name, so nothing is gained storing more than any channel could
	// plausibly need to draw from, and a mega-channel's full chatter list
	// would otherwise bloat every replica's read.
	chattersSnapshotCap = 5000
)

// chattersSnapshotEntry is the JSON wire shape of one cached chat-list entry.
// Helix Get Chatters (outgress's GetChatters) returns only an id and a
// login — no display name — so Name mirrors Login at write time; it stays
// its own field rather than being dropped so the render side (chatter_vars.go)
// never needs to know this snapshot's provenance, the same shape
// engine.Viewer and scope.Chatter already share.
type chattersSnapshotEntry struct {
	ID    uint64 `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

// ValkeyChatters is the shared chat-list cache behind {random.viewer}: a
// short-TTL JSON blob per channel, plus the cross-replica lock that keeps a
// cold cache from being fetched twice at once. A nil receiver degrades to
// "always cold, never locks" (every method checks), matching chatterRoster's
// nil-safe convention, so a caller need not special-case an unwired store.
type ValkeyChatters struct {
	client valkey.Client
	log    *zap.Logger
}

func NewValkeyChatters(client valkey.Client, log *zap.Logger) *ValkeyChatters {
	if log == nil {
		log = zap.NewNop()
	}
	return &ValkeyChatters{client: client, log: log}
}

func chattersSnapshotKey(broadcasterID uint64) string {
	return cache.UserKey(chattersSnapshotPrefix, broadcasterID)
}

func chattersFetchLockKey(broadcasterID uint64) string {
	return cache.UserKey(chattersFetchLockPrefix, broadcasterID)
}

// Snapshot reads the cached chat list. ok=false covers a nil store and a
// clean cache miss (no snapshot yet, or it expired) alike, both with err=nil
// — normal, unremarkable outcomes a caller never logs. err is set only when
// Valkey itself could not answer (a transport failure, or a decode failure
// on a key that does exist and should not be trusted): the caller decides
// whether and how often to log that, since this store does not know if it is
// the first occurrence this process has seen (see ViewerRPC).
func (v *ValkeyChatters) Snapshot(ctx context.Context, broadcasterID uint64) (entries []chattersSnapshotEntry, ok bool, err error) {
	if v == nil || v.client == nil {
		return nil, false, nil
	}
	raw, err := v.client.Do(ctx, v.client.B().Get().Key(chattersSnapshotKey(broadcasterID)).Build()).AsBytes()
	if err != nil {
		if valkey.IsValkeyNil(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var out []chattersSnapshotEntry
	if err := codec.Unmarshal(raw, &out); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// Store writes the channel's chat list, capped and TTL'd. Errors are logged
// rather than returned: this always runs off a render's own reply path (a
// background fetch, or the loyalty tick's write-through), so there is no
// caller left to hand a failure to by the time it would happen.
func (v *ValkeyChatters) Store(ctx context.Context, broadcasterID uint64, entries []chattersSnapshotEntry) {
	if v == nil || v.client == nil {
		return
	}
	if len(entries) > chattersSnapshotCap {
		entries = entries[:chattersSnapshotCap]
	}
	body, err := codec.Marshal(entries)
	if err != nil {
		v.log.Warn("chatters: snapshot encode failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
		return
	}
	seconds := int64(chattersSnapshotTTL.Seconds())
	err = v.client.Do(ctx, v.client.B().Set().Key(chattersSnapshotKey(broadcasterID)).Value(string(body)).ExSeconds(seconds).Build()).Error()
	if err != nil {
		v.log.Warn("chatters: snapshot write failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}

// TryFetchLock claims the one-fetch-per-channel lock. true means this caller
// won the race and must perform the fetch (and, on success, Store its
// result); false means another replica already holds it, so this caller
// draws nothing this run rather than racing a second fetch.
func (v *ValkeyChatters) TryFetchLock(ctx context.Context, broadcasterID uint64) bool {
	if v == nil || v.client == nil {
		return false
	}
	seconds := int64(chattersFetchLockTTL.Seconds())
	got, err := v.client.Do(ctx, v.client.B().Set().Key(chattersFetchLockKey(broadcasterID)).Value("1").Nx().ExSeconds(seconds).Build()).ToString()
	return err == nil && got == "OK"
}

// ReleaseFetchLock clears the lock early on a fetch that failed or found
// nothing worth storing, so a retry is not blocked for the lock's full TTL.
// Expiry remains the backstop for a fetch that crashed before reaching here
// at all (see chattersFetchLockTTL).
func (v *ValkeyChatters) ReleaseFetchLock(ctx context.Context, broadcasterID uint64) {
	if v == nil || v.client == nil {
		return
	}
	err := v.client.Do(ctx, v.client.B().Del().Key(chattersFetchLockKey(broadcasterID)).Build()).Error()
	if err != nil {
		v.log.Warn("chatters: fetch lock release failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
