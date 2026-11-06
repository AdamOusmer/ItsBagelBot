// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// ReadString returns the key's string value, or "" when it does not exist.
func (kv KV) ReadString(ctx context.Context, name string) (string, error) {
	raw, err := kv.client.Do(ctx, kv.client.B().Get().Key(name).Build()).ToString()
	if valkey_go.IsValkeyNil(err) {
		return "", nil
	}
	return raw, err
}
