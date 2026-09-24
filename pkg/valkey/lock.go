// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const claimValue = "1"

// A plain DEL would free a lock another replica took after our TTL lapsed.
const releaseIfOwner = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`

type OwnerLock struct {
	client valkey_go.Client
	key    string
	owner  string
}

// owner must be unique to this holder, or one replica can release another's lock.
func NewOwnerLock(client valkey_go.Client, key, owner string) OwnerLock {
	return OwnerLock{client: client, key: key, owner: owner}
}

func (l OwnerLock) Acquire(ctx context.Context, ttl time.Duration) (bool, error) {
	cmd := l.client.B().Set().Key(l.key).Value(l.owner).Nx().PxMilliseconds(ttl.Milliseconds()).Build()
	return wonClaim(l.client.Do(ctx, cmd))
}

func (l OwnerLock) Release(ctx context.Context) error {
	cmd := l.client.B().Eval().Script(releaseIfOwner).Numkeys(1).Key(l.key).Arg(l.owner).Build()
	return l.client.Do(ctx, cmd).Error()
}

func ClaimOnce(ctx context.Context, client valkey_go.Client, key string, ttl time.Duration) (bool, error) {
	cmd := client.B().Set().Key(key).Value(claimValue).Nx().Ex(ttl).Build()
	return wonClaim(client.Do(ctx, cmd))
}

func wonClaim(res valkey_go.ValkeyResult) (bool, error) {
	str, err := res.ToString()
	if err != nil {
		if valkey_go.IsValkeyNil(err) {
			return false, nil
		}
		return false, err
	}
	return str == "OK", nil
}
