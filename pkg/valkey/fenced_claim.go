// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var fencedClaimScript = valkey_go.NewLuaScript(`
local previous=redis.call('GET',KEYS[2])
if previous~=ARGV[1] then redis.call('DEL',KEYS[1]) end
redis.call('SET',KEYS[2],ARGV[1],'EX',172800)
return redis.call('SET',KEYS[1],'1','NX','EX',ARGV[2])`)

// FencedClaim is a claim key gated by a companion fence: a fence change clears any existing claim before the claim attempt runs.
type FencedClaim struct {
	Claim string
	Fence string
}

// Arm claims the key for ttl, first clearing it if fence changed since the last Arm. It reports whether the claim was newly taken.
func (c FencedClaim) Arm(ctx context.Context, client valkey_go.Client, fence string, ttl time.Duration) (bool, error) {
	return wonClaim(fencedClaimScript.Exec(ctx, client, []string{c.Claim, c.Fence}, []string{
		fence, strconv.FormatInt(int64(ttl.Seconds()), 10),
	}))
}
