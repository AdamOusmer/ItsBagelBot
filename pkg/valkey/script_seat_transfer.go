// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var seatTransferScript = valkey_go.NewLuaScript(`
local user, to, occupants, clones, ttl = ARGV[1], ARGV[2], ARGV[3], ARGV[4], ARGV[5]
local from = redis.call('GET', KEYS[1]) or ''
local empty = 0
if from ~= '' then
    redis.call('SREM', occupants .. from, user)
    if from ~= to and redis.call('SCARD', occupants .. from) == 0 then
        empty = 1
    end
end
if to == '' then
    redis.call('DEL', KEYS[1])
else
    redis.call('SADD', occupants .. to, user)
    redis.call('EXPIRE', occupants .. to, ttl)
    redis.call('EXPIRE', clones .. to, ttl)
    redis.call('SET', KEYS[1], to, 'EX', ttl)
end
return {from, tostring(empty)}
`)

// RunSeatTransfer runs the seat-pointer transfer script.
func RunSeatTransfer(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return seatTransferScript.Exec(ctx, client, keys, args)
}
