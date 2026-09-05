// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"github.com/valkey-io/valkey-go"
)

// cachedConfigEntry is what the settings cache key holds: the blob plus the
// version, because a caller that reads from cache still has to echo a version
// back on its next write.
type cachedConfigEntry struct {
	Config  ddiscord.Config `json:"config"`
	Version int             `json:"version"`
}

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

// configCacheTTL bounds how long a cached guild settings blob is served
// without asking discord-data.
//
// One minute, an order of magnitude shorter than bindingCacheTTL, because the
// two entries fail differently. A stale binding sends events to the wrong
// broadcaster's config; a stale settings blob only delays a toggle the
// streamer just flipped. Outgress DELs this key on every successful save, so
// the TTL covers exactly one case -- an invalidation this replica never saw --
// and a minute of a just-flipped switch not taking effect is the worst that
// costs.
const configCacheTTL = 60 * time.Second

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
	// cachedConfig reads the guild settings cache. Unlike cachedBroadcaster
	// this is NOT what Store.GuildConfig does on the Valkey store: settings
	// have no Valkey store of record, so only the RPC store may serve them,
	// and only as a cache in front of discord-data.
	cachedConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool)
	// cacheConfig stores one guild's settings with configCacheTTL.
	cacheConfig(ctx context.Context, g Guild, cfg ddiscord.Config, version int)
	// dropConfig invalidates one guild's settings.
	dropConfig(ctx context.Context, g Guild)
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

func (s valkeyStore) cachedConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(cfgKey(g)).Build()).ToString()
	if err != nil || raw == "" {
		return ddiscord.Config{}, 0, false
	}
	var entry cachedConfigEntry
	if err := codec.FastUnmarshal([]byte(raw), &entry); err != nil {
		return ddiscord.Config{}, 0, false
	}
	return entry.Config, entry.Version, true
}

func (s valkeyStore) cacheConfig(ctx context.Context, g Guild, cfg ddiscord.Config, version int) {
	if g.ID == "" {
		return
	}
	body, err := codec.FastMarshal(cachedConfigEntry{Config: cfg, Version: version})
	if err != nil {
		return
	}
	// Failing to cache is not failing the read, same as cacheBroadcaster: the
	// next event pays another round trip rather than being dropped.
	_ = s.client.Do(ctx, s.client.B().Set().Key(cfgKey(g)).Value(string(body)).
		ExSeconds(int64(configCacheTTL.Seconds())).Build()).Error()
}

func (s valkeyStore) dropConfig(ctx context.Context, g Guild) {
	_ = s.client.Do(ctx, s.client.B().Del().Key(cfgKey(g)).Build()).Error()
}

func (m *Mem) cachedConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool) {
	return m.GuildConfig(ctx, g)
}

func (m *Mem) cacheConfig(_ context.Context, g Guild, cfg ddiscord.Config, version int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[g.ID] = memConfig{Config: cfg, Version: version}
}

func (m *Mem) dropConfig(_ context.Context, g Guild) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.configs, g.ID)
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
