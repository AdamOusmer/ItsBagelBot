// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// SetMembers returns every member of a set.
func SetMembers(ctx context.Context, client valkey_go.Client, key string) ([]string, error) {
	return client.Do(ctx, client.B().Smembers().Key(key).Build()).AsStrSlice()
}
