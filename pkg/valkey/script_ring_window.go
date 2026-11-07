// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var ringWindowBumpScript = valkey_go.NewLuaScript(`
local key = KEYS[1]
local epoch = tonumber(ARGV[1])
local handled = ARGV[2]
local ttl = ARGV[3]
local width = tonumber(ARGV[4])

local anchor = tonumber(redis.call('HGET', key, 'a'))
if not anchor then
    anchor = epoch
    redis.call('HSET', key, 'a', anchor)
end

local delta = epoch - anchor
local slot = tostring(epoch % width)
local count = 1
local existing = redis.call('HGET', key, slot)
if existing then
    local sep1 = string.find(existing, ':')
    if sep1 and tonumber(string.sub(existing, 1, sep1 - 1)) == delta then
        local sep2 = string.find(existing, ':', sep1 + 1)
        count = tonumber(string.sub(existing, sep1 + 1, sep2 - 1)) + 1
        if handled ~= '1' then
            handled = string.sub(existing, sep2 + 1)
        end
    end
end

redis.call('HSET', key, slot, delta .. ':' .. count .. ':' .. handled)
redis.call('EXPIRE', key, ttl)
return count
`)

// RunRingWindowBump runs the fixed-width ring bucket bump script.
func RunRingWindowBump(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return ringWindowBumpScript.Exec(ctx, client, keys, args)
}
