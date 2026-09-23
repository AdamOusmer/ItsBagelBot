// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"ItsBagelBot/internal/domain/rpc/manage"
	"ItsBagelBot/pkg/bus"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"
)

// viewerFetchRPCTimeout bounds the background chatters.get call ViewerRPC
// fires on a cold snapshot, the same budget uptime_rpc.go's cached reads use.
// It bounds the fetch goroutine only — see fetch's own context.
const viewerFetchRPCTimeout = 3500 * time.Millisecond

// viewerSnapshotState is Snapshot's three-way outcome for one command run.
type viewerSnapshotState int

const (
	// viewerSnapshotCold means no pool is available THIS run: the cache was
	// empty and, if this caller won the fetch lock, a background fetch is
	// now in flight for the next one. The caller (viewerSource) degrades to
	// a Roster-backed pool for this draw, same as viewerSnapshotMissingScope
	// — the two are distinguished here only so Snapshot itself can decide
	// whether to bother trying a fetch, not because the draw renders
	// differently.
	viewerSnapshotCold viewerSnapshotState = iota
	// viewerSnapshotOK means the cached chat list answers the draw.
	viewerSnapshotOK
	// viewerSnapshotMissingScope means this channel's grant cannot ever
	// answer Get Chatters; the caller degrades to a Roster-backed pool, and
	// Snapshot skips the fetch attempt entirely while latched.
	viewerSnapshotMissingScope
)

// ViewerLookup is {random.viewer}'s dependency on the Pipeline, so
// chatter_vars.go's viewerSource can be built over an interface rather than
// a concrete *ViewerRPC (tests substitute a fake).
type ViewerLookup interface {
	Snapshot(ctx context.Context, broadcasterID uint64) ([]chattersSnapshotEntry, viewerSnapshotState)
}

// ViewerRPC is the engine half of {random.viewer}: it reads/writes the
// shared Valkey snapshot (ValkeyChatters) and, on a cold cache, triggers at
// most one background fetch per channel through outgress's chatters.get —
// the very verb the loyalty watch tick already calls (see chatters.go /
// loyalty_tick.go), reused here rather than duplicated.
type ViewerRPC struct {
	store   *ValkeyChatters
	request func(context.Context, manage.ChattersRequest) (manage.ChattersReply, error)
	log     *zap.Logger

	// missingScope is a per-process latch: once a channel's grant is known
	// to lack moderator:read:chatters (or the bot lost its moderator seat),
	// Snapshot skips the fetch attempt for it until further notice. It is
	// per-process rather than shared in Valkey on purpose — a fleet-wide
	// latch would need its own invalidation path to ever heal after
	// re-consent, where a process restart already clears this one for free.
	// It is NOT the only way out, though: the loyalty watch tick calls the
	// very same chatters.get verb on its own schedule (loyalty_tick.go), so
	// a restored moderator seat repopulates the shared Valkey snapshot on
	// the tick's next fire regardless of this latch — and Snapshot reads
	// that snapshot BEFORE consulting the latch, so a hit there clears it
	// (clearMissingScope) rather than waiting for a process restart.
	//
	// down is a second, independent per-process latch for one thing only:
	// logging. A Valkey read failure (as opposed to a clean cache miss)
	// degrades the draw to the roster exactly like a cold cache does, but it
	// is unusual enough to be worth a log line — once per channel per
	// process, not once per command.
	mu           sync.Mutex
	missingScope map[uint64]bool
	down         map[uint64]bool
}

func NewViewerRPC(nc *nats.Conn, prefix string, store *ValkeyChatters, log *zap.Logger) *ViewerRPC {
	subject := strings.TrimSuffix(prefix, ".") + ".chatters.get"
	if log == nil {
		log = zap.NewNop()
	}
	return &ViewerRPC{
		store: store,
		request: func(ctx context.Context, req manage.ChattersRequest) (manage.ChattersReply, error) {
			return bus.RequestJSONTimeout[manage.ChattersReply](ctx, nc, subject, req, viewerFetchRPCTimeout)
		},
		log:          log,
		missingScope: make(map[uint64]bool),
		down:         make(map[uint64]bool),
	}
}

// Snapshot answers one command run's {random.viewer} read. It never blocks
// on a fetch: a cold cache triggers at most one background fetch (guarded by
// ValkeyChatters' cross-replica lock) and answers cold for THIS run
// regardless, so a paginated Get Chatters walk can never slow a chat reply.
//
// The snapshot read runs BEFORE the missingScope latch is consulted, not
// after: a hit clears the latch (see the field comment), so reading first is
// what lets a restored moderator seat heal this replica's answer as soon as
// the tick repopulates the snapshot, rather than only on process restart.
func (r *ViewerRPC) Snapshot(ctx context.Context, broadcasterID uint64) ([]chattersSnapshotEntry, viewerSnapshotState) {
	entries, ok, err := r.store.Snapshot(ctx, broadcasterID)
	if ok {
		r.clearMissingScope(broadcasterID)
		return entries, viewerSnapshotOK
	}
	if err != nil {
		r.logDownOnce(broadcasterID, err)
	}
	if r.isMissingScope(broadcasterID) {
		return nil, viewerSnapshotMissingScope
	}
	if r.store.TryFetchLock(ctx, broadcasterID) {
		go r.fetch(broadcasterID)
	}
	return nil, viewerSnapshotCold
}

// fetch runs the chatters.get call and writes the result back, entirely off
// the triggering command's context: a paginated fetch can run past any one
// reply's lifetime, and this must survive that reply's ctx cancelling to
// still warm the snapshot for the NEXT command. Every failure path releases
// the fetch lock early (abortFetch / explicit release) rather than leaving
// the next attempt blocked for the lock's full TTL.
func (r *ViewerRPC) fetch(broadcasterID uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), viewerFetchRPCTimeout)
	defer cancel()
	reply, err := r.request(ctx, manage.ChattersRequest{BroadcasterID: strconv.FormatUint(broadcasterID, 10)})
	if err != nil {
		r.abortFetch(broadcasterID, err)
		return
	}
	if reply.MissingScope {
		r.markMissingScope(broadcasterID)
		r.store.ReleaseFetchLock(context.Background(), broadcasterID)
		return
	}
	if reply.Error != "" {
		r.abortFetch(broadcasterID, errors.New(reply.Error))
		return
	}
	r.store.Store(context.Background(), broadcasterID, viewerSnapshotEntries(reply.Chatters))
}

// abortFetch logs a failed fetch and releases the cross-replica lock early;
// see fetch's own comment for why.
func (r *ViewerRPC) abortFetch(broadcasterID uint64, err error) {
	r.log.Warn("viewer: chatters fetch failed", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	r.store.ReleaseFetchLock(context.Background(), broadcasterID)
}

// viewerSnapshotEntries adapts the wire Chatter list; Helix carries no
// display name, so Name mirrors Login (see chattersSnapshotEntry).
func viewerSnapshotEntries(chatters []manage.Chatter) []chattersSnapshotEntry {
	out := make([]chattersSnapshotEntry, 0, len(chatters))
	for _, ch := range chatters {
		id, err := strconv.ParseUint(ch.ID, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		out = append(out, chattersSnapshotEntry{ID: id, Login: ch.Login, Name: ch.Login})
	}
	return out
}

func (r *ViewerRPC) isMissingScope(broadcasterID uint64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.missingScope[broadcasterID]
}

// markMissingScope latches the channel and logs once per channel per
// process, never again after — until clearMissingScope lifts it.
func (r *ViewerRPC) markMissingScope(broadcasterID uint64) {
	r.mu.Lock()
	already := r.missingScope[broadcasterID]
	r.missingScope[broadcasterID] = true
	r.mu.Unlock()
	if !already {
		r.log.Warn("viewer: chatters missing scope, degrading to roster draws", zap.Uint64("broadcaster_id", broadcasterID))
	}
}

// clearMissingScope lifts the latch on a snapshot hit: the tick's own
// chatters.get call answered fine, so whatever revoked the scope has been
// fixed (or the earlier failure was transient), and this replica should stop
// assuming otherwise.
func (r *ViewerRPC) clearMissingScope(broadcasterID uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.missingScope, broadcasterID)
}

// logDownOnce logs a Valkey read failure once per channel per process. It
// does not gate behavior (Snapshot already treats this the same as a cold
// cache either way) — it exists only so an outage shows up in the logs
// instead of silently degrading every draw to the roster.
func (r *ViewerRPC) logDownOnce(broadcasterID uint64, err error) {
	r.mu.Lock()
	already := r.down[broadcasterID]
	r.down[broadcasterID] = true
	r.mu.Unlock()
	if !already {
		r.log.Warn("viewer: chatters snapshot read failed, degrading to roster draws", zap.Uint64("broadcaster_id", broadcasterID), zap.Error(err))
	}
}
