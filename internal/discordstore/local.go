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

// guildsCacheTTL bounds how long a cached "which guilds does this broadcaster
// own" answer is served without asking discord-data.
//
// Sixty seconds, matching configCacheTTL rather than bindingCacheTTL. The
// listing feeds two callers with opposite tolerances: the dashboard's server
// picker, which a streamer reloads seconds after adding a server, and the
// Twitch fan-out, which posts to every guild on it. Both write verbs drop the
// key directly, so the TTL is only the backstop for an invalidation a replica
// never saw -- and a minute is short enough that a server added on another
// replica shows up before the streamer reaches for the reload button.
const guildsCacheTTL = 60 * time.Second

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
	// cachedGuilds reads one broadcaster's cached guild list.
	cachedGuilds(ctx context.Context, b Broadcaster) ([]Binding, bool)
	// cacheGuilds stores one broadcaster's guild list with guildsCacheTTL.
	cacheGuilds(ctx context.Context, b Broadcaster, guilds []Binding)
	// dropGuilds invalidates one broadcaster's guild list. Both write verbs
	// call it, which is what keeps the listing symmetric with the binding
	// cache: bind and unbind invalidate directly, the TTL is the backstop.
	dropGuilds(ctx context.Context, b Broadcaster)
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
	// The binding key and its cache entry are the same key here, so dropping
	// the cache is the DEL UnbindGuild issues -- minus the guild-list
	// invalidation, which has no broadcaster to address at this point.
	_ = s.client.Do(ctx, s.client.B().Del().Key(guildKey(g)).Build()).Error()
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

func (s valkeyStore) cachedGuilds(ctx context.Context, b Broadcaster) ([]Binding, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(guildsKey(b)).Build()).ToString()
	if err != nil || raw == "" {
		return nil, false
	}
	var entry []Binding
	if err := codec.FastUnmarshal([]byte(raw), &entry); err != nil {
		return nil, false
	}
	return entry, true
}

func (s valkeyStore) cacheGuilds(ctx context.Context, b Broadcaster, guilds []Binding) {
	if b.ID == "" {
		return
	}
	body, err := codec.FastMarshal(guilds)
	if err != nil {
		return
	}
	// Failing to cache is not failing the read, same as cacheBroadcaster.
	_ = s.client.Do(ctx, s.client.B().Set().Key(guildsKey(b)).Value(string(body)).
		ExSeconds(int64(guildsCacheTTL.Seconds())).Build()).Error()
}

func (s valkeyStore) dropGuilds(ctx context.Context, b Broadcaster) {
	if b.ID == "" {
		return
	}
	_ = s.client.Do(ctx, s.client.B().Del().Key(guildsKey(b)).Build()).Error()
}

// The memory double's guild-list cache is its own map, not its GuildsOf: the
// RPC store composes a local half purely for these three, and a double whose
// cache methods were no-ops would let a caching bug pass every test.
func (m *Mem) cachedGuilds(_ context.Context, b Broadcaster) ([]Binding, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	got, ok := m.guildsCache[b.ID]
	return got, ok
}

func (m *Mem) cacheGuilds(_ context.Context, b Broadcaster, guilds []Binding) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.guildsCache[b.ID] = guilds
}

func (m *Mem) dropGuilds(_ context.Context, b Broadcaster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.guildsCache, b.ID)
}

func (s valkeyStore) takeXPCooldown(ctx context.Context, m Member) bool {
	err := s.client.Do(ctx, s.client.B().Set().Key(xpCDKey(m)).Value("1").Nx().ExSeconds(xpCooldown).Build()).Error()
	return err == nil
}

func (m *Mem) cachedBroadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	return m.Broadcaster(ctx, g)
}

func (m *Mem) cacheBroadcaster(ctx context.Context, g Guild, b Broadcaster) {
	_ = m.BindGuild(ctx, Binding{Guild: g, Broadcaster: b})
}

func (m *Mem) dropBroadcaster(ctx context.Context, g Guild) {
	_ = m.UnbindGuild(ctx, Binding{Guild: g})
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
