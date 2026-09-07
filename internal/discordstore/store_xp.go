// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// XP keyspace: message XP, the per-member award cooldown and the daily bonus.
//
// One of the four keyspace files store.go was split into; see store_voice.go
// for why the split runs by keyspace across both implementations rather than
// by implementation.

package discordstore

import (
	"context"
	"strconv"

	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/valkey-io/valkey-go"
)

const (
	xpPerMessage = 15
	xpCooldown   = 60
	dailyXP      = 50
	dailyTTL     = 24 * 60 * 60
)

func xpKey(m Member) string { return "discord:xp:" + m.key() }

func xpCDKey(m Member) string { return "discord:xpcd:" + m.key() }

func dailyKey(m Member) string { return "discord:daily:" + m.key() }

func (s valkeyStore) AddXP(ctx context.Context, m Member) (int, bool, int) {
	if !s.takeXPCooldown(ctx, m) {
		xp, level := s.Rank(ctx, m)
		return xp, false, level
	}
	before, _ := s.Rank(ctx, m)
	n, err := s.client.Do(ctx, s.client.B().Incrby().Key(xpKey(m)).Increment(xpPerMessage).Build()).AsInt64()
	if err != nil {
		return before, false, levelOf(before)
	}
	xp := int(n)
	return xp, levelOf(xp) > levelOf(before), levelOf(xp)
}

func (s valkeyStore) ClaimDaily(ctx context.Context, m Member) (bool, int) {
	err := s.client.Do(ctx, s.client.B().Set().Key(dailyKey(m)).Value("1").Nx().ExSeconds(dailyTTL).Build()).Error()
	if valkey.IsValkeyNil(err) {
		xp, _ := s.Rank(ctx, m)
		return false, xp
	}
	if err != nil {
		return false, 0
	}
	n, err := s.client.Do(ctx, s.client.B().Incrby().Key(xpKey(m)).Increment(dailyXP).Build()).AsInt64()
	if err != nil {
		return true, dailyXP
	}
	return true, int(n)
}

func (s valkeyStore) Rank(ctx context.Context, m Member) (int, int) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(xpKey(m)).Build()).ToString()
	if err != nil {
		return 0, 0
	}
	if raw == "" {
		return 0, 0
	}
	xp, _ := strconv.Atoi(raw)
	return xp, levelOf(xp)
}

// levelOf is the XP->level curve, owned by internal/domain/discord so the
// discord-data repository (which stores the level column) and this fast path
// cannot drift apart. Kept as a local shim because every caller here holds an
// int, not the int64 the stored column uses.
func levelOf(xp int) int { return ddiscord.LevelOf(int64(xp)) }

func (m *Mem) AddXP(_ context.Context, mem Member) (int, bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mem.key()
	if !m.takeXPCooldownLocked(mem) {
		xp := m.xp[k]
		return xp, false, levelOf(xp)
	}
	before := m.xp[k]
	m.xp[k] = before + xpPerMessage
	return m.xp[k], levelOf(m.xp[k]) > levelOf(before), levelOf(m.xp[k])
}

func (m *Mem) ClaimDaily(_ context.Context, mem Member) (bool, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := mem.key()
	if m.daily[k] {
		return false, m.xp[k]
	}
	m.daily[k] = true
	m.xp[k] += dailyXP
	return true, m.xp[k]
}

func (m *Mem) Rank(_ context.Context, mem Member) (int, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	xp := m.xp[mem.key()]
	return xp, levelOf(xp)
}

type XPSeed struct {
	Member Member
	Amount int
}

// SeedXP is a test helper that sets crumbs without touching the cooldown.
func (m *Mem) SeedXP(s XPSeed) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xp[s.Member.key()] = s.Amount
}
