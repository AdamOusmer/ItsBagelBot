// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var addFreshMemberScript = valkey_go.NewLuaScript(`
local added = redis.call('SADD', KEYS[1], ARGV[1])
if added == 1 then redis.call('EXPIRE', KEYS[1], ARGV[2]) end
return added`)

// FreshMemberSet is a set whose TTL renews only when a member joins it for the first time.
type FreshMemberSet struct{ Key string }

// Add reports whether member was new to the set.
func (s FreshMemberSet) Add(ctx context.Context, client valkey_go.Client, member string, ttl time.Duration) (bool, error) {
	added, err := addFreshMemberScript.Exec(ctx, client, []string{s.Key}, []string{
		member, strconv.FormatInt(int64(ttl.Seconds()), 10),
	}).AsInt64()
	if err != nil {
		return false, err
	}
	return added > 0, nil
}
