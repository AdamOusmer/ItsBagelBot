// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// bindingCacheTTL bounds how long a cached guild->broadcaster answer is served
// without asking discord-data.
//
// Ten minutes, not indefinite and not seconds. The binding changes only when a
// broadcaster runs setup or unbind from the dashboard, which is rare and
// already invalidates the entry directly, so the TTL exists purely as the
// backstop for an invalidation this process never saw (a replica that was
// restarting, a dropped RPC). Shorter would put a round trip on the hot path of
// every gateway event for no gain; longer leaves a genuinely stale binding
// answering events for a channel that has moved on.
const bindingCacheTTL = 10 * time.Minute

// localStore is the node-local half of the state Discord features need: the
// whole Store surface, plus the cache and cooldown primitives that never leave
// the node and so are not part of the cross-process interface. Both the Valkey
// store and the in-memory test double satisfy it.
//
// NewRPC composes one of these rather than reimplementing voice occupancy,
// clone tracking and the desk lock over RPC: those keyspaces are ephemeral,
// engine-private and die with the channel they describe, so moving them to
// MySQL would buy durability nothing reads.
type localStore interface {
	Store

	// cachedBroadcaster reads the guild binding cache. It is exactly what
	// Store.Broadcaster does on the Valkey store; named separately so the RPC
	// store's fallback path reads as a cache read at the call site.
	cachedBroadcaster(ctx context.Context, g Guild) (Broadcaster, bool)
	// cacheBroadcaster stores one binding with bindingCacheTTL.
	cacheBroadcaster(ctx context.Context, g Guild, b Broadcaster)
	// dropBroadcaster invalidates one binding.
	dropBroadcaster(ctx context.Context, g Guild)
	// takeXPCooldown reports whether this message earns XP, taking the 60s
	// per-member cooldown when it does. It stays local by design: a rate
	// limiter with a TTL is the one thing Valkey is better at than MySQL, and
	// routing it through discord-data would put an RPC round trip on every
	// message in every guild.
	takeXPCooldown(ctx context.Context, m Member) bool
}

// newLocal builds the node-local store. A nil client yields the in-memory
// double, matching New's contract.
func newLocal(client valkey.Client) localStore {
	if client == nil {
		return NewMem()
	}
	return valkeyStore{client: client}
}

func (s valkeyStore) cachedBroadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	return s.Broadcaster(ctx, g)
}

func (s valkeyStore) cacheBroadcaster(ctx context.Context, g Guild, b Broadcaster) {
	if g.ID == "" || b.ID == "" {
		return
	}
	// Failing to cache is not failing the read: the next call pays another
	// round trip, which is strictly better than turning a served event into an
	// error because Valkey blinked.
	_ = s.client.Do(ctx, s.client.B().Set().Key(guildKey(g)).Value(b.ID).
		ExSeconds(int64(bindingCacheTTL.Seconds())).Build()).Error()
}

func (s valkeyStore) dropBroadcaster(ctx context.Context, g Guild) {
	_ = s.UnbindGuild(ctx, g)
}

func (s valkeyStore) takeXPCooldown(ctx context.Context, m Member) bool {
	err := s.client.Do(ctx, s.client.B().Set().Key(xpCDKey(m)).Value("1").Nx().ExSeconds(xpCooldown).Build()).Error()
	return err == nil
}

func (m *Mem) cachedBroadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	return m.Broadcaster(ctx, g)
}

func (m *Mem) cacheBroadcaster(ctx context.Context, g Guild, b Broadcaster) {
	_ = m.BindGuild(ctx, g, b)
}

func (m *Mem) dropBroadcaster(ctx context.Context, g Guild) {
	_ = m.UnbindGuild(ctx, g)
}

func (m *Mem) takeXPCooldown(_ context.Context, mem Member) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.takeXPCooldownLocked(mem)
}

// takeXPCooldownLocked assumes m.mu is already held.
func (m *Mem) takeXPCooldownLocked(mem Member) bool {
	k := mem.key()
	if m.xpCD[k] {
		return false
	}
	m.xpCD[k] = true
	return true
}
