// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var versionFoldApplyScript = valkey_go.NewLuaScript(`local cur = tonumber(redis.call('HGET', KEYS[1], 'live_ver'))
if cur and cur > tonumber(ARGV[2]) then return -1 end
local was = redis.call('HGET', KEYS[1], 'live')
if ARGV[1]=='1' then
 if was~='1' or redis.call('HEXISTS',KEYS[1],'stream_baseline_pending_ver')==1 then redis.call('HSET',KEYS[1],'stream_baseline_pending_ver',ARGV[2]) end
else redis.call('HDEL',KEYS[1],'stream_baseline_pending_ver') end
redis.call('HSET', KEYS[1], 'live', ARGV[1], 'live_ver', ARGV[2])
redis.call('EXPIRE', KEYS[1], ARGV[3], 'NX')
redis.call('EXPIRE', KEYS[1], ARGV[3], 'GT')
if was == '1' then return 1 end
return 0`)

var versionFoldDiscardPendingScript = valkey_go.NewLuaScript(`if redis.call('HGET',KEYS[1],'stream_baseline_pending_ver')==ARGV[1] then redis.call('HDEL',KEYS[1],'stream_baseline_pending_ver') end return 1`)

var versionFoldCommitPendingScript = valkey_go.NewLuaScript(`local confirmed=tonumber(redis.call('GET',KEYS[2]) or '0')
 if confirmed>tonumber(ARGV[1]) then return 0 end
 if redis.call('HGET',KEYS[1],'live')~='1' or redis.call('HGET',KEYS[1],'live_ver')~=ARGV[1] or redis.call('HGET',KEYS[1],'stream_baseline_pending_ver')~=ARGV[1] then return 0 end
 redis.call('HSET',KEYS[1],'streamctr:messages',ARGV[2],'streamctr:answered',ARGV[3],'streamctr:mod_actions',ARGV[4])
 redis.call('HDEL',KEYS[1],'stream_baseline_pending_ver') return 1`)

// RunVersionFoldApply runs the monotonic live-version fold script.
func RunVersionFoldApply(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return versionFoldApplyScript.Exec(ctx, client, keys, args)
}

// RunVersionFoldDiscardPending runs the pending-baseline discard script.
func RunVersionFoldDiscardPending(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return versionFoldDiscardPendingScript.Exec(ctx, client, keys, args)
}

// RunVersionFoldCommitPending runs the pending-baseline commit script.
func RunVersionFoldCommitPending(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return versionFoldCommitPendingScript.Exec(ctx, client, keys, args)
}
