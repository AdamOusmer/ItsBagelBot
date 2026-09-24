// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

func Incr(ctx context.Context, client valkey_go.Client, key string, ttl time.Duration) (int64, error) {
	resps := client.DoMulti(ctx,
		client.B().Incr().Key(key).Build(),
		client.B().Expire().Key(key).Seconds(int64(ttl.Seconds())).Build(),
	)
	n, err := resps[0].AsInt64()
	if err != nil {
		return 0, err
	}
	if err := resps[1].Error(); err != nil {
		return 0, err
	}
	return n, nil
}

func GetInt(ctx context.Context, client valkey_go.Client, key string) (int64, error) {
	n, err := client.Do(ctx, client.B().Get().Key(key).Build()).AsInt64()
	if err != nil {
		if valkey_go.IsValkeyNil(err) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}
