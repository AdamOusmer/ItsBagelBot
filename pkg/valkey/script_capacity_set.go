// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var capacitySetAddScript = valkey_go.NewLuaScript(`
local added = redis.call('SADD', KEYS[2], ARGV[1])
if redis.call('SCARD', KEYS[2]) > tonumber(ARGV[4]) then
    if added == 1 then
        redis.call('SREM', KEYS[2], ARGV[1])
    end
    return 0
end
redis.call('SET', KEYS[1], ARGV[2], 'EX', ARGV[3])
redis.call('EXPIRE', KEYS[2], ARGV[3])
return 1
`)

// RunCapacitySetAdd runs the capacity-bounded set-membership script.
func RunCapacitySetAdd(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return capacitySetAddScript.Exec(ctx, client, keys, args)
}
