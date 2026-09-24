// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package invitecache

import (
	"context"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const noGuild = "\x00none"

type Cache struct {
	kv pkg_valkey.KV
}

func New(client valkey.Client) *Cache {
	return &Cache{kv: pkg_valkey.NewKV(client)}
}

func key(code string) string { return "discord:invite:" + code + ":guild" }

func (c *Cache) Get(ctx context.Context, code string) (guildID string, hit bool) {
	raw, ok := c.kv.GetString(ctx, key(code))
	if !ok {
		return "", false
	}
	if raw == noGuild {
		return "", true
	}
	return raw, true
}

func (c *Cache) Put(ctx context.Context, code, guildID string, ttl time.Duration) error {
	val := guildID
	if val == "" {
		val = noGuild
	}
	return c.kv.Set(ctx, pkg_valkey.Key{Name: key(code), TTL: ttl}, val)
}
