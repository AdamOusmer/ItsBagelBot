// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// HyperLogLogCount returns a HyperLogLog's approximate cardinality.
func HyperLogLogCount(ctx context.Context, client valkey_go.Client, key string) (int64, error) {
	return client.Do(ctx, client.B().Pfcount().Key(key).Build()).AsInt64()
}
