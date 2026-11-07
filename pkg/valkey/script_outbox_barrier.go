// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var outboxBarrierAdvanceScript = valkey_go.NewLuaScript(`
local n=redis.call('XLEN',KEYS[1])
if n~=redis.call('ZCARD',KEYS[2]) then return 0 end
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local cutoff=math.min(tonumber(ARGV[1]),now-tonumber(ARGV[2]))
local first=redis.call('ZRANGE',KEYS[2],0,0,'WITHSCORES')
if #first>0 then cutoff=math.min(cutoff,tonumber(first[2])-1) end
if cutoff<=0 then return 0 end
local prior=tonumber(redis.call('GET',KEYS[3]) or '0')
if prior>cutoff then return 0 end
redis.call('SET',KEYS[3],string.format('%.0f',cutoff))
return cutoff`)

// RunOutboxBarrierAdvance runs the outbox retirement barrier script.
func RunOutboxBarrierAdvance(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return outboxBarrierAdvanceScript.Exec(ctx, client, keys, args)
}
