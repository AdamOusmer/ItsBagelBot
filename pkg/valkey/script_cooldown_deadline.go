// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var cooldownDeadlineObserveScript = valkey_go.NewLuaScript(`
local clock=redis.call('TIME')
local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
local incoming=math.min(tonumber(ARGV[1]),now+tonumber(ARGV[2]))
local raw=redis.call('GET',KEYS[1])
local previous=raw and tonumber(raw) or 0
if raw and not tonumber(raw) then return redis.error_reply('invalid provider reset state') end
if previous>incoming then incoming=previous end
if incoming<=now then redis.call('DEL',KEYS[1]); return 0 end
redis.call('SET',KEYS[1],string.format('%.0f',incoming))
return 1`)

var cooldownDeadlineReadScript = valkey_go.NewLuaScript(`local raw=redis.call('GET',KEYS[1]); if not raw then return 0 end
local at=tonumber(raw); if not at or at<=0 then return redis.error_reply('invalid provider reset state') end
local clock=redis.call('TIME'); local now=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)
if at<=now then redis.call('DEL',KEYS[1]); return 0 end
return at`)

// RunCooldownDeadlineObserve runs the bounded cooldown-deadline publish script.
func RunCooldownDeadlineObserve(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return cooldownDeadlineObserveScript.Exec(ctx, client, keys, args)
}

// RunCooldownDeadlineRead runs the cooldown-deadline read-and-retire script.
func RunCooldownDeadlineRead(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return cooldownDeadlineReadScript.Exec(ctx, client, keys, args)
}
