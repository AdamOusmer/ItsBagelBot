// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Expire sets a key's TTL; a missing key is not an error.
func Expire(ctx context.Context, client valkey_go.Client, key string, ttl time.Duration) error {
	err := client.Do(ctx, client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Build()).Error()
	if valkey_go.IsValkeyNil(err) {
		return nil
	}
	return err
}

// TTLSeconds is the server's own TTL command: positive seconds left, -1 for no expiry, -2 for a missing key.
func TTLSeconds(ctx context.Context, client valkey_go.Client, key string) (int64, error) {
	return client.Do(ctx, client.B().Ttl().Key(key).Build()).AsInt64()
}

// ReadString tells a missing key from a failed read.
func ReadString(ctx context.Context, client valkey_go.Client, key string) (string, bool, error) {
	raw, err := client.Do(ctx, client.B().Get().Key(key).Build()).ToString()
	if valkey_go.IsValkeyNil(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return raw, true, nil
}

// ListFront reads a list's first element without popping it; a missing list (or element) is not an error.
func ListFront(ctx context.Context, client valkey_go.Client, key string) (string, bool, error) {
	raw, err := client.Do(ctx, client.B().Lindex().Key(key).Index(0).Build()).ToString()
	if valkey_go.IsValkeyNil(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return raw, true, nil
}

// ZRangeAll returns every member of a sorted set in score order.
func ZRangeAll(ctx context.Context, client valkey_go.Client, key string) ([]string, error) {
	return client.Do(ctx, client.B().Zrange().Key(key).Min("0").Max("-1").Build()).AsStrSlice()
}

// IncrPlain increments a key with no TTL change.
func IncrPlain(ctx context.Context, client valkey_go.Client, key string) (int64, error) {
	return client.Do(ctx, client.B().Incr().Key(key).Build()).AsInt64()
}
