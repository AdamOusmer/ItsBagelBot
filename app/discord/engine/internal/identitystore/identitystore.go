// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package identitystore remembers which appearance the bot has already
// applied in a guild, so the engine only asks outgress to change it when it
// actually differs.
//
// This exists because of one gateway fact: GUILD_CREATE fires for EVERY guild
// on EVERY connect, not just when the bot joins a new server. Without a
// memory, one reconnect would re-apply the identity to every guild at once,
// and each premium apply uploads a ~66 KB avatar through the shared ~50 req/s
// per-token budget. A fleet-wide restart would turn into a self-inflicted
// rate-limit incident that changes nothing about how the bot looks.
//
// Not folded into internal/discordstore for the same reason as invitecache:
// discordstore is state both outgress and engine read, while this is a
// decision cache only the identity module consults.
package identitystore

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"
)

// TTL outlives any plausible gap between reconnects, so the common case (a
// restart, a rollout) finds the entry present and skips the work. It is not
// infinite on purpose: a bounded entry means a guild whose identity somehow
// drifted from what we recorded (an admin reset the nickname by hand, a
// failed apply we recorded as done) self-corrects within a week instead of
// staying wrong until someone notices.
const TTL = 7 * 24 * time.Hour

// Store is the Valkey-backed guild -> applied-appearance record.
type Store struct {
	client valkey.Client
}

// New builds the store. Like invitecache.New and linkguard.New, it does not
// nil-guard the client: engine's main.go Fatals before valkeyClient can be
// nil, and a defensive check here would hide that invariant breaking rather
// than surface it.
func New(client valkey.Client) *Store {
	return &Store{client: client}
}

func key(guildID string) string { return "discord:identity:" + guildID }

// Applied returns the fingerprint last recorded for guildID. A miss returns
// "" and false, which callers must treat as "apply it", never as "default":
// an unknown guild and a guild known to be on the default appearance need
// different actions, and only one of them is a no-op.
func (s *Store) Applied(ctx context.Context, guildID string) (string, bool) {
	raw, err := s.client.Do(ctx, s.client.B().Get().Key(key(guildID)).Build()).ToString()
	if err != nil || raw == "" {
		return "", false
	}
	return raw, true
}

// Record stores the fingerprint now applied to guildID.
//
// Callers record only AFTER a successful publish, so a publish failure is
// retried on the next trigger. Note this records that the COMMAND was
// published, not that Discord accepted it: outgress owns that half, and a
// failed REST call nacks onto the work queue and is redelivered there. Both
// halves retry, each at its own layer.
func (s *Store) Record(ctx context.Context, guildID, fingerprint string) error {
	return s.client.Do(ctx, s.client.B().Set().Key(key(guildID)).Value(fingerprint).Ex(TTL).Build()).Error()
}
