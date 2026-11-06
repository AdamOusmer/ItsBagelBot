// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// HSetIfAbsent sets a hash field only when it was unset, and reports whether it did.
func HSetIfAbsent(ctx context.Context, client valkey_go.Client, field HashField, value string) (bool, error) {
	added, err := client.Do(ctx, client.B().Hsetnx().Key(field.Key).Field(field.Field).Value(value).Build()).AsInt64()
	return added > 0, err
}

// HValues reads every value in a hash; a missing hash is empty, not an error.
func HValues(ctx context.Context, client valkey_go.Client, key string) ([]string, error) {
	vals, err := client.Do(ctx, client.B().Hvals().Key(key).Build()).AsStrSlice()
	if valkey_go.IsValkeyNil(err) {
		return nil, nil
	}
	return vals, err
}

// ReadHashInt tells a missing field from a failed read.
func ReadHashInt(ctx context.Context, client valkey_go.Client, key, field string) (int64, bool, error) {
	v, err := client.Do(ctx, client.B().Hget().Key(key).Field(field).Build()).AsInt64()
	if valkey_go.IsValkeyNil(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return v, true, nil
}
