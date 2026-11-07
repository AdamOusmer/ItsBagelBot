// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var hashRevisionGateRuntimeScript = valkey_go.NewLuaScript(`
if redis.call('HGET',KEYS[2],'deleted')=='1' then return 0 end
local instance=redis.call('HGET',KEYS[2],'instance')
if instance and instance~=ARGV[1] then return 0 end
local revision=tonumber(redis.call('HGET',KEYS[2],'state_revision') or '0')
if tonumber(ARGV[2])<revision then return 0 end
local prior=redis.call('HGET',KEYS[1],'lifecycle')
local changed=true
if prior and prior~='null' then
 changed=cjson.decode(prior).admission_generation~=cjson.decode(ARGV[3]).admission_generation
end
if changed then redis.call('HINCRBY',KEYS[2],'epoch',1) end
redis.call('HSET',KEYS[2],'instance',ARGV[1],'state_revision',ARGV[2])
redis.call('HSET',KEYS[1],'account_created_at',ARGV[1],'lifecycle',ARGV[3],'active',ARGV[4])
redis.call('HSET',KEYS[1],'runtime_background',ARGV[5])
if redis.call('TTL',KEYS[1])<0 then redis.call('EXPIRE',KEYS[1],86400) end
return 1`)

var hashRevisionGateUserScript = valkey_go.NewLuaScript(`-- user watch admission write
local instance=redis.call('HGET',KEYS[2],'instance')
if redis.call('HGET',KEYS[2],'deleted') == '1' then return 0 end
if instance and instance ~= ARGV[7] then return 0 end
local revision=tonumber(redis.call('HGET',KEYS[2],'state_revision') or '0')
local incoming=tonumber(ARGV[8])
if incoming < revision then return 0 end
if incoming > 0 then redis.call('HSET',KEYS[2],'state_revision',ARGV[8]) end
local active=redis.call('HGET',KEYS[1],'active'); local banned=redis.call('HGET',KEYS[1],'banned')
local prior=redis.call('HGET',KEYS[1],'lifecycle')
local changed=false
if prior and prior ~= 'null' and ARGV[9] ~= 'null' then
 local old=cjson.decode(prior); local new=cjson.decode(ARGV[9])
 changed=old.admission_generation ~= new.admission_generation
end
if active ~= ARGV[1] or banned ~= ARGV[2] or changed then redis.call('HINCRBY',KEYS[2],'epoch',1) end
redis.call('HSET',KEYS[1],'active',ARGV[1],'banned',ARGV[2],'status',ARGV[3],'commands_page_hidden',ARGV[4])
redis.call('HSET',KEYS[1],'account_created_at',ARGV[7],'lifecycle',ARGV[9])
redis.call('HSET',KEYS[1],'runtime_background',ARGV[10],'leaderboard_page_hidden',ARGV[11],'creator_code',ARGV[12])
if ARGV[5] ~= '' then redis.call('HSET',KEYS[1],'locale',ARGV[5]) end
local existing=redis.call('TTL',KEYS[1]); if existing<0 or existing<tonumber(ARGV[6]) then redis.call('EXPIRE',KEYS[1],ARGV[6]) end
return 1`)

var hashRevisionGateLegacyDeleteScript = valkey_go.NewLuaScript(`-- user watch deletion
if tonumber(redis.call('HGET',KEYS[2],'instance') or '0') > 0 then return 0 end
redis.call('HINCRBY',KEYS[2],'epoch',1); redis.call('HSET',KEYS[2],'deleted','1'); redis.call('DEL',KEYS[1]); return 1`)

var hashRevisionGateModuleScript = valkey_go.NewLuaScript(`-- module revision gate
if redis.call('HGET',KEYS[2],'deleted') == '1' then return 0 end
local instance=redis.call('HGET',KEYS[2],'instance')
if ARGV[3] == 'module:loyalty:enabled' and instance and instance ~= ARGV[8] then return 0 end
local current=tonumber(redis.call('HGET',KEYS[1],ARGV[1]) or '-1')
local incoming=tonumber(ARGV[2])
if ARGV[3] == 'module:loyalty:enabled' then
 current=math.max(current,tonumber(redis.call('HGET',KEYS[2],'loyalty_revision') or '-1'))
 if current>=0 then redis.call('HSET',KEYS[2],'loyalty_revision',string.format('%.0f',current)) end
end
if incoming < current then return 0 end
if ARGV[3] == 'module:loyalty:enabled' then redis.call('HSET',KEYS[2],'loyalty_revision',ARGV[2]) end
if ARGV[3] == 'module:loyalty:enabled' and (redis.call('HGET',KEYS[1],ARGV[3]) ~= ARGV[4] or redis.call('HGET',KEYS[1],ARGV[5]) ~= ARGV[6]) then redis.call('HINCRBY',KEYS[2],'epoch',1) end
redis.call('HSET',KEYS[1],ARGV[1],ARGV[2],ARGV[3],ARGV[4],ARGV[5],ARGV[6],string.gsub(ARGV[3],':enabled$',':account_created_at'),ARGV[8])
redis.call('EXPIRE',KEYS[1],ARGV[7],'NX')
redis.call('EXPIRE',KEYS[1],ARGV[7],'GT')
return 1`)

// RunHashRevisionGateRuntime runs the lifecycle-only revision-gated hash write script.
func RunHashRevisionGateRuntime(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return hashRevisionGateRuntimeScript.Exec(ctx, client, keys, args)
}

// RunHashRevisionGateUser runs the full user revision-gated hash write script.
func RunHashRevisionGateUser(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return hashRevisionGateUserScript.Exec(ctx, client, keys, args)
}

// RunHashRevisionGateLegacyDelete runs the unstamped-delete gate script.
func RunHashRevisionGateLegacyDelete(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return hashRevisionGateLegacyDeleteScript.Exec(ctx, client, keys, args)
}

// RunHashRevisionGateModule runs the per-module revision-gated field write script.
func RunHashRevisionGateModule(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return hashRevisionGateModuleScript.Exec(ctx, client, keys, args)
}
