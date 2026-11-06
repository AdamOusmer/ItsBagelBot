// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

const outboxAdmissionGate = `
if redis.call('HGET',KEYS[2],'deleted') == '1' then return false end
if redis.call('HGET',KEYS[1],'active') ~= '1' or redis.call('HGET',KEYS[1],'banned') == '1' or redis.call('HGET',KEYS[1],'module:loyalty:enabled') ~= '1' then return false end
if redis.call('HGET',KEYS[1],'runtime_background') == '0' then return false end
if redis.call('SISMEMBER',KEYS[4],ARGV[1]) == 1 then return false end
local session=redis.call('GET',KEYS[3]); if not session then return false end
local generation=redis.call('HGET',KEYS[2],'epoch') or '0'
local instance=redis.call('HGET',KEYS[2],'instance'); if not instance then return false end
`

var outboxAdmissionCaptureScript = valkey_go.NewLuaScript(outboxAdmissionGate + `if redis.call('HGET',KEYS[5],'active') == '1' then session=redis.call('HGET',KEYS[5],'session') or session end
return {generation,session,instance,redis.call('HGET',KEYS[1],'module:loyalty:config') or ''}`)

var outboxAdmissionEnqueueScript = valkey_go.NewLuaScript(outboxAdmissionGate + `
if generation ~= ARGV[2] or instance ~= ARGV[9] then return false end
local floor=tonumber(redis.call('GET',KEYS[9]) or '0')
if floor>0 and tonumber(ARGV[10])<=floor then return false end
if ARGV[7] == '' and session ~= ARGV[3] then return false end
if ARGV[7] ~= '' then
 if redis.call('GET',KEYS[7]) ~= ARGV[7] or redis.call('HGET',KEYS[8],'active') ~= '1' or redis.call('HGET',KEYS[8],'window') ~= ARGV[8] or redis.call('HGET',KEYS[8],'session') ~= ARGV[3] then return false end
end
local previous=redis.call('HGET',KEYS[6],ARGV[4]); if previous then
 if previous ~= ARGV[6] then return redis.error_reply('watch operation payload mismatch') end
 return 1
end
local delivery=redis.call('XADD',KEYS[5],'*','operation_id',ARGV[4],'payload',ARGV[5])
redis.call('ZADD',KEYS[10],ARGV[10],delivery)
redis.call('HSET',KEYS[6],ARGV[4],ARGV[6])
return 1`)

// RunOutboxAdmissionCapture runs the admission-snapshot capture script.
func RunOutboxAdmissionCapture(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return outboxAdmissionCaptureScript.Exec(ctx, client, keys, args)
}

// RunOutboxAdmissionEnqueue runs the admission-gated outbox enqueue script.
func RunOutboxAdmissionEnqueue(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return outboxAdmissionEnqueueScript.Exec(ctx, client, keys, args)
}
