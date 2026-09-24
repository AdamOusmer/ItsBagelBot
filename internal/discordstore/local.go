// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"time"

	ddiscord "ItsBagelBot/internal/domain/discord"
	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

type cachedConfigEntry struct {
	Config  ddiscord.Config `json:"config"`
	Version int             `json:"version"`
}

const bindingCacheTTL = 10 * time.Minute

const configCacheTTL = 60 * time.Second

const guildsCacheTTL = 60 * time.Second

type localStore interface {
	Store

	cachedBroadcaster(ctx context.Context, g Guild) (Broadcaster, bool)
	cacheBroadcaster(ctx context.Context, g Guild, b Broadcaster)
	dropBroadcaster(ctx context.Context, g Guild)
	cachedConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool)
	cacheConfig(ctx context.Context, g Guild, cfg ddiscord.Config, version int)
	dropConfig(ctx context.Context, g Guild)
	cachedGuilds(ctx context.Context, b Broadcaster) ([]Binding, bool)
	cacheGuilds(ctx context.Context, b Broadcaster, guilds []Binding)
	dropGuilds(ctx context.Context, b Broadcaster)
	takeXPCooldown(ctx context.Context, m Member) bool
}

func newLocal(client valkey.Client) localStore {
	if client == nil {
		return NewMem()
	}
	return valkeyStore{client: client}
}

func (s valkeyStore) kv() pkg_valkey.KV { return pkg_valkey.NewKV(s.client) }

func (s valkeyStore) cachedBroadcaster(ctx context.Context, g Guild) (Broadcaster, bool) {
	return s.Broadcaster(ctx, g)
}

func (s valkeyStore) cacheBroadcaster(ctx context.Context, g Guild, b Broadcaster) {
	if g.ID == "" || b.ID == "" {
		return
	}
	_ = s.kv().Set(ctx, pkg_valkey.Key{Name: guildKey(g), TTL: bindingCacheTTL}, b.ID)
}

func (s valkeyStore) dropBroadcaster(ctx context.Context, g Guild) {
	_ = s.kv().Del(ctx, guildKey(g))
}

func (s valkeyStore) cachedConfig(ctx context.Context, g Guild) (ddiscord.Config, int, bool) {
	entry, ok := pkg_valkey.GetJSON[cachedConfigEntry](ctx, s.kv(), cfgKey(g))
	if !ok {
		return ddiscord.Config{}, 0, false
	}
	return entry.Config, entry.Version, true
}

func (s valkeyStore) cacheConfig(ctx context.Context, g Guild, cfg ddiscord.Config, version int) {
	if g.ID == "" {
		return
	}
	at := pkg_valkey.Key{Name: cfgKey(g), TTL: configCacheTTL}
	_ = pkg_valkey.SetJSON(ctx, s.kv(), at, cachedConfigEntry{Config: cfg, Version: version})
}

func (s valkeyStore) dropConfig(ctx context.Context, g Guild) {
	_ = s.kv().Del(ctx, cfgKey(g))
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
	return pkg_valkey.GetJSON[[]Binding](ctx, s.kv(), guildsKey(b))
}

func (s valkeyStore) cacheGuilds(ctx context.Context, b Broadcaster, guilds []Binding) {
	if b.ID == "" {
		return
	}
	_ = pkg_valkey.SetJSON(ctx, s.kv(), pkg_valkey.Key{Name: guildsKey(b), TTL: guildsCacheTTL}, guilds)
}

func (s valkeyStore) dropGuilds(ctx context.Context, b Broadcaster) {
	if b.ID == "" {
		return
	}
	_ = s.kv().Del(ctx, guildsKey(b))
}

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

func (m *Mem) takeXPCooldownLocked(mem Member) bool {
	k := mem.key()
	if m.xpCD[k] {
		return false
	}
	m.xpCD[k] = true
	return true
}
