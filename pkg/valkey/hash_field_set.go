// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Set writes the field's value without touching the key's TTL.
func (f HashField) Set(ctx context.Context, client valkey_go.Client, value string) error {
	return client.Do(ctx, client.B().Hset().Key(f.Key).FieldValue().FieldValue(f.Field, value).Build()).Error()
}
