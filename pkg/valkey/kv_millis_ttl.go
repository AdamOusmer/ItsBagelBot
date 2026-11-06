// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"
)

// SetMillis writes value with a millisecond-precision TTL, replacing any existing value.
func (kv KV) SetMillis(ctx context.Context, name, value string, ttl time.Duration) error {
	return kv.client.Do(ctx, kv.client.B().Set().Key(name).Value(value).Px(ttl).Build()).Error()
}
