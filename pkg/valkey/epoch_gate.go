// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

var epochGateActivateScript = NewScript(`
local owner = redis.call('GET', KEYS[1]) or ''
if string.sub(owner, 1, string.len(ARGV[1]) + 1) ~= ARGV[1] .. ':' then return 0 end
if redis.call('GET', KEYS[3]) ~= ARGV[1] .. ':' .. ARGV[4] then return 0 end
if redis.call('HGET', KEYS[2], 'generation') ~= ARGV[2] then return 0 end
if redis.call('HGET', KEYS[2], 'enabled') == '0' then return 0 end
if (redis.call('HGET', KEYS[2], 'slot') or '0') ~= ARGV[5] then return 0 end
local state = redis.call('HGET', KEYS[2], 'state')
if state ~= 'pending' and state ~= 'receiving' and state ~= 'failed' then return 0 end
redis.call('HSET', KEYS[2], 'subscription_id', ARGV[3], 'session_id', ARGV[4], 'owner_epoch', ARGV[1], 'state', 'receiving')
redis.call('HDEL', KEYS[2], 'error')
return 1`)

var epochGateReleaseScript = NewScript(`
local owner = redis.call('GET', KEYS[1]) or ''
if string.sub(owner, 1, string.len(ARGV[1]) + 1) ~= ARGV[1] .. ':' then return 0 end
if redis.call('HGET', KEYS[2], 'generation') ~= ARGV[2] then return 0 end
local state = redis.call('HGET', KEYS[2], 'state')
if state ~= 'stopping' and state ~= 'disabled' then return 0 end
if (redis.call('HGET', KEYS[2], 'subscription_id') or '') ~= ARGV[3] then return 0 end
redis.call('HDEL', KEYS[2], 'subscription_id', 'session_id', 'owner_epoch')
if state == 'disabled' and redis.call('HGET', KEYS[2], 'enabled') ~= '0' then
  redis.call('HSET', KEYS[2], 'state', 'pending')
end
return 1`)

// EpochGate guards a record's state transitions behind an owner key holding "<epoch>:<suffix>" and a generation stamp on the record.
type EpochGate struct {
	Owner   string
	Session string
	Record  string
}

// ActivateCall builds the script call for Activate.
func (g EpochGate) ActivateCall(epoch, generation, subscriptionID, sessionID, slot string) ScriptCall {
	return ScriptCall{
		Script: epochGateActivateScript,
		Key:    g.Owner,
		Also:   []string{g.Record, g.Session},
		Args:   []string{epoch, generation, subscriptionID, sessionID, slot},
	}
}

// Activate moves Record into the receiving state, only while Owner's epoch prefix, Session, generation, enabled flag and slot all still match.
func (g EpochGate) Activate(ctx context.Context, client valkey_go.Client, epoch, generation, subscriptionID, sessionID, slot string) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{g.ActivateCall(epoch, generation, subscriptionID, sessionID, slot)})[0].ToInt64()
	return n == 1, err
}

// ReleaseCall builds the script call for Release.
func (g EpochGate) ReleaseCall(epoch, generation, subscriptionID string) ScriptCall {
	return ScriptCall{
		Script: epochGateReleaseScript,
		Key:    g.Owner,
		Also:   []string{g.Record},
		Args:   []string{epoch, generation, subscriptionID},
	}
}

// Release clears Record's subscription fields, only while Owner's epoch prefix, generation, state and subscriptionID still match.
func (g EpochGate) Release(ctx context.Context, client valkey_go.Client, epoch, generation, subscriptionID string) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{g.ReleaseCall(epoch, generation, subscriptionID)})[0].ToInt64()
	return n == 1, err
}
