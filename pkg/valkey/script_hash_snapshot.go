// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var hashSnapshotSeedScript = valkey_go.NewLuaScript(`-- module hydration revision seed
if redis.call('HGET',KEYS[2],'deleted') == '1' then return 0 end
local instance=redis.call('HGET',KEYS[2],'instance')
local ttl=tonumber(ARGV[1]); local n=tonumber(ARGV[2]); local p=3
if instance then
 for i=1,n do local start=3+(i-1)*5; if ARGV[start] == 'loyalty' and instance ~= ARGV[start+4] then return 0 end end
end
local warm=tonumber(redis.call('HGET',KEYS[1],'module:loyalty:revision') or '-1')
if instance and warm>=0 and redis.call('HGET',KEYS[1],'module:loyalty:account_created_at')==instance then
 local known=tonumber(redis.call('HGET',KEYS[2],'loyalty_revision') or '-1')
 redis.call('HSET',KEYS[2],'loyalty_revision',string.format('%.0f',math.max(warm,known)))
end
for i=1,n do
 local name=ARGV[p]; local incoming=tonumber(ARGV[p+1]); local current=tonumber(redis.call('HGET',KEYS[1],'module:'..name..':revision') or '-1')
 if name == 'loyalty' then
  current=math.max(current,tonumber(redis.call('HGET',KEYS[2],'loyalty_revision') or '-1'))
  if current>=0 then redis.call('HSET',KEYS[2],'loyalty_revision',string.format('%.0f',current)) end
 end
 if incoming>=current and (name ~= 'loyalty' or not instance or instance == ARGV[p+4]) then
  if name == 'loyalty' then redis.call('HSET',KEYS[2],'loyalty_revision',ARGV[p+1]) end
  if name == 'loyalty' and (redis.call('HGET',KEYS[1],'module:loyalty:enabled') ~= ARGV[p+2] or redis.call('HGET',KEYS[1],'module:loyalty:config') ~= ARGV[p+3]) then redis.call('HINCRBY',KEYS[2],'epoch',1) end
  redis.call('HSET',KEYS[1],'module:'..name..':revision',ARGV[p+1],'module:'..name..':enabled',ARGV[p+2],'module:'..name..':config',ARGV[p+3],'module:'..name..':account_created_at',ARGV[p+4])
 end
 p=p+5
end
local missingKnownLoyalty=redis.call('HEXISTS',KEYS[2],'loyalty_revision')==1 and redis.call('HEXISTS',KEYS[1],'module:loyalty:enabled')==0
if missingKnownLoyalty then redis.call('HDEL',KEYS[1],'modules:projected') else redis.call('HSET',KEYS[1],'modules:projected','1') end
local existing=redis.call('TTL',KEYS[1]); if existing<0 or existing<ttl then redis.call('EXPIRE',KEYS[1],ttl) end
return 1`)

var hashSnapshotReadScript = valkey_go.NewLuaScriptReadOnly(`-- module-only settings snapshot
local fields=redis.call('HGETALL',KEYS[1])
local out={}
for i=1,#fields,2 do
 local name=fields[i]
 if name == 'modules:projected' or string.sub(name,1,7) == 'module:' then
  out[#out+1]=name; out[#out+1]=fields[i+1]
 end
end
return out`)

// RunHashSnapshotSeed runs the additive module-hydration snapshot seed script.
func RunHashSnapshotSeed(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return hashSnapshotSeedScript.Exec(ctx, client, keys, args)
}

// RunHashSnapshotRead runs the read-only filtered hash snapshot script.
func RunHashSnapshotRead(ctx context.Context, client valkey_go.Client, key string) valkey_go.ValkeyResult {
	return hashSnapshotReadScript.Exec(ctx, client, []string{key}, nil)
}
