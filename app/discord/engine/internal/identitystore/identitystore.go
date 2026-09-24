// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package identitystore

import (
	"context"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const TTL = 7 * 24 * time.Hour

type Store struct {
	kv pkg_valkey.KV
}

func New(client valkey.Client) *Store {
	return &Store{kv: pkg_valkey.NewKV(client)}
}

func key(guildID string) string { return "discord:identity:" + guildID }

func (s *Store) Applied(ctx context.Context, guildID string) (string, bool) {
	return s.kv.GetString(ctx, key(guildID))
}

func (s *Store) Record(ctx context.Context, guildID, fingerprint string) error {
	return s.kv.Set(ctx, pkg_valkey.Key{Name: key(guildID), TTL: TTL}, fingerprint)
}
