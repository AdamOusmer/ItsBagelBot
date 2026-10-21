// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

const claimValue = "1"

// A plain DEL would free a lock another replica took after our TTL lapsed.
const releaseIfOwner = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('del',KEYS[1]) else return 0 end`

// A plain PEXPIRE would extend a lock another replica took after our TTL lapsed.
const renewIfOwner = `if redis.call('get',KEYS[1])==ARGV[1] then return redis.call('pexpire',KEYS[1],ARGV[2]) else return 0 end`

var (
	renewIfOwnerScript = valkey_go.NewLuaScript(renewIfOwner)
	releaseIfOwnerCall = NewScript(releaseIfOwner)
)

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

func (l OwnerLock) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	n, err := renewIfOwnerScript.Exec(ctx, l.client, []string{l.key}, []string{l.owner, strconv.FormatInt(ttl.Milliseconds(), 10)}).AsInt64()
	return n == 1, err
}

func (l OwnerLock) Release(ctx context.Context) error {
	return RunScripts(ctx, l.client, []ScriptCall{l.ReleaseCall()})[0].Error()
}

func (l OwnerLock) ReleaseCall() ScriptCall {
	return ScriptCall{Script: releaseIfOwnerCall, Key: l.key, Args: []string{l.owner}}
}

func ClaimOnce(ctx context.Context, client valkey_go.Client, key string, ttl time.Duration) (bool, error) {
	cmd := client.B().Set().Key(key).Value(claimValue).Nx().PxMilliseconds(ttl.Milliseconds()).Build()
	return wonClaim(client.Do(ctx, cmd))
}

type Claim struct {
	Key string
	TTL time.Duration
}

var claimAllScript = valkey_go.NewLuaScript(`
if redis.call('EXISTS', unpack(KEYS)) > 0 then return 0 end
for i, key in ipairs(KEYS) do
  redis.call('SET', key, '1', 'PX', ARGV[i])
end
return 1`)

// ClaimAll wins every claim or none of them.
func ClaimAll(ctx context.Context, client valkey_go.Client, claims []Claim) (bool, error) {
	keys := make([]string, len(claims))
	ttls := make([]string, len(claims))
	for i, c := range claims {
		keys[i] = c.Key
		ttls[i] = strconv.FormatInt(c.TTL.Milliseconds(), 10)
	}
	n, err := claimAllScript.Exec(ctx, client, keys, ttls).AsInt64()
	return n == 1, err
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
