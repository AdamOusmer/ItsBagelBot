// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var instanceFenceRestoreScript = valkey_go.NewLuaScript(`
 local prior=tonumber(redis.call('HGET',KEYS[1],'instance') or '0')
 local incoming=tonumber(ARGV[1])
 if incoming < prior then return 0 end
 if incoming == prior then
  if redis.call('HGET',KEYS[1],'deleted') == '1' then return 0 end
  return 1
 end
 redis.call('HINCRBY',KEYS[1],'epoch',1)
 redis.call('HSET',KEYS[1],'instance',ARGV[1]); redis.call('HDEL',KEYS[1],'deleted','state_revision','loyalty_revision')
 redis.call('DEL',KEYS[2],KEYS[3],KEYS[4])
 return 1`)

var instanceFenceDeleteScript = valkey_go.NewLuaScript(`
 local prior=tonumber(redis.call('HGET',KEYS[1],'instance') or '0')
 if tonumber(ARGV[1]) < prior then return 0 end
 redis.call('HINCRBY',KEYS[1],'epoch',1)
 redis.call('HSET',KEYS[1],'instance',ARGV[1],'deleted','1')
 redis.call('DEL',KEYS[2],KEYS[3],KEYS[4],KEYS[5],KEYS[7]); redis.call('ZREM',KEYS[6],ARGV[2])
 return 1`)

// RunInstanceFenceRestore runs the instance-fenced restore script.
func RunInstanceFenceRestore(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return instanceFenceRestoreScript.Exec(ctx, client, keys, args)
}

// RunInstanceFenceDelete runs the instance-fenced retirement script.
func RunInstanceFenceDelete(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return instanceFenceDeleteScript.Exec(ctx, client, keys, args)
}
