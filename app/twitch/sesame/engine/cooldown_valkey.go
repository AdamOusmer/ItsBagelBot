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

func (c *ValkeyCooldown) TimeLeft(ctx context.Context, key string) (time.Duration, error) {
	ms, err := c.client.Do(ctx, c.client.B().Pttl().Key(key).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	return max(time.Duration(ms)*time.Millisecond, 0), nil
}
