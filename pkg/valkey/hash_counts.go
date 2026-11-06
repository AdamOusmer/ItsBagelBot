// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// HashAllInts reads a hash whose fields all hold integer values.
func HashAllInts(ctx context.Context, client valkey_go.Client, key string) (map[string]int64, error) {
	return client.Do(ctx, client.B().Hgetall().Key(key).Build()).AsIntMap()
}
