// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import "strconv"

// Drops members already past their time, so the index only ever holds what could still be alive, then lives as long as its longest member.
var expiringIndexAdd = NewScript(`
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', ARGV[1])
redis.call('ZADD', KEYS[1], ARGV[2], ARGV[3])
if redis.call('PEXPIRETIME', KEYS[1]) < tonumber(ARGV[2]) then
  redis.call('PEXPIREAT', KEYS[1], ARGV[2])
end
return 1`)

// ExpiringIndexEntry lists one key that expires at ExpiresAtMS under an index, so an owner's keys can be found without a keyspace scan.
type ExpiringIndexEntry struct {
	Index       string
	Member      string
	ExpiresAtMS int64
}

func (e ExpiringIndexEntry) AddCall(nowMS int64) ScriptCall {
	return ScriptCall{
		Script: expiringIndexAdd,
		Key:    e.Index,
		Args:   []string{strconv.FormatInt(nowMS, 10), strconv.FormatInt(e.ExpiresAtMS, 10), e.Member},
	}
}
