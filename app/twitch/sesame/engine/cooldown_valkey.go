// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

type ValkeyCooldown struct {
	client valkey.Client
}

func NewValkeyCooldown(client valkey.Client) *ValkeyCooldown {
	return &ValkeyCooldown{client: client}
}

func (c *ValkeyCooldown) Allow(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return pkg_valkey.ClaimOnce(ctx, c.client, key, ttl)
}

func (c *ValkeyCooldown) AllowAll(ctx context.Context, windows []CooldownWindow) (bool, error) {
	return pkg_valkey.ClaimAll(ctx, c.client, windows)
}
