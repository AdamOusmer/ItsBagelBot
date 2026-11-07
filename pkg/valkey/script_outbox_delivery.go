// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var outboxDeliveryAckScript = valkey_go.NewLuaScript(`redis.call('XACK',KEYS[1],ARGV[1],ARGV[2]); redis.call('XDEL',KEYS[1],ARGV[2]); redis.call('HDEL',KEYS[2],ARGV[3]); redis.call('ZREM',KEYS[3],ARGV[2]); return 1`)

var outboxDeliveryQuarantineScript = valkey_go.NewLuaScript(`
local clock=redis.call('TIME')
local horizon=tonumber(clock[1])*1000+math.floor(tonumber(clock[2])/1000)-tonumber(ARGV[5])
redis.call('XADD',KEYS[2],'MINID','~',string.format('%.0f',math.max(horizon,0)),'*','source_id',ARGV[2],'reason',ARGV[3],'payload',ARGV[4])
redis.call('XACK',KEYS[1],ARGV[1],ARGV[2]); redis.call('XDEL',KEYS[1],ARGV[2]); redis.call('ZREM',KEYS[3],ARGV[2]); return 1`)

// RunOutboxDeliveryAck runs the delivery acknowledgement script.
func RunOutboxDeliveryAck(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return outboxDeliveryAckScript.Exec(ctx, client, keys, args)
}

// RunOutboxDeliveryQuarantine runs the delivery quarantine script.
func RunOutboxDeliveryQuarantine(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return outboxDeliveryQuarantineScript.Exec(ctx, client, keys, args)
}
