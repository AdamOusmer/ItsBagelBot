// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// SeedOnMiss reads a string key, seeding it with candidate on a first miss; a lost race against another writer re-reads the winning value.
func SeedOnMiss(ctx context.Context, client valkey_go.Client, at Key, candidate string) (string, error) {
	got, err := client.Do(ctx, client.B().Get().Key(at.Name).Build()).ToString()
	if err == nil {
		return got, nil
	}
	if !valkey_go.IsValkeyNil(err) {
		return "", err
	}
	setResult := client.Do(ctx, client.B().Set().Key(at.Name).Value(candidate).Nx().Ex(at.TTL).Build())
	if _, setErr := setResult.ToString(); setErr == nil {
		return candidate, nil
	} else if !valkey_go.IsValkeyNil(setErr) {
		return "", setErr
	}
	return client.Do(ctx, client.B().Get().Key(at.Name).Build()).ToString()
}
