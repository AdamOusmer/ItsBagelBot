// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"strconv"
	"time"
)

var claimLeaseScript = NewScript(`
local value = redis.call('GET', KEYS[1])
if value == ARGV[3] then return 2 end
if value then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2])
return 1`)

var finishLeaseScript = NewScript(`
if redis.call('GET', KEYS[1]) ~= ARGV[1] then return 0 end
redis.call('SET', KEYS[1], ARGV[3], 'PX', ARGV[2])
return 1`)

// Lease outcomes of MarkedLease.
const (
	LeaseHeld int64 = 0
	LeaseWon  int64 = 1
	LeaseDone int64 = 2
)

// MarkedLease is a key that moves from empty to an owner's claim to a terminal Done marker.
type MarkedLease struct {
	Key  string
	Done string
}

// ClaimCall builds the pending-claim script call: won (1), already held (0), or already Done (2).
func (l MarkedLease) ClaimCall(owner string, ttl time.Duration) ScriptCall {
	return ScriptCall{Script: claimLeaseScript, Key: l.Key, Args: []string{owner, strconv.FormatInt(ttl.Milliseconds(), 10), l.Done}}
}

// FinishCall builds the script call marking the lease Done, only while owner still holds it.
func (l MarkedLease) FinishCall(owner string, ttl time.Duration) ScriptCall {
	return ScriptCall{Script: finishLeaseScript, Key: l.Key, Args: []string{owner, strconv.FormatInt(ttl.Milliseconds(), 10), l.Done}}
}
