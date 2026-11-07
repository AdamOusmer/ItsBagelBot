// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// UnlinkAll unlinks every key in one call; an empty list is a no-op.
func UnlinkAll(ctx context.Context, client valkey_go.Client, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	return client.Do(ctx, client.B().Unlink().Key(keys...).Build()).Error()
}
