// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

var reputationEvent = NewScript(`
if #KEYS == 2 then
  local previous = redis.call('GET', KEYS[2])
  if previous then return tonumber(previous) end
end
local score = tonumber(redis.call('GET', KEYS[1]) or '0')
redis.call('INCR', KEYS[1])
redis.call('PEXPIRE', KEYS[1], ARGV[1])
if #KEYS == 2 then redis.call('SET', KEYS[2], tostring(score), 'PX', ARGV[2]) end
return score
`)

type ReputationEvent struct {
	Account   string
	Receipt   string
	TTL       time.Duration
	ReplayTTL time.Duration
}

func ObserveReputation(ctx context.Context, client valkeygo.Client, event ReputationEvent) (int64, error) {
	keys := []string{event.Account}
	if event.Receipt != "" {
		keys = append(keys, event.Receipt)
	}
	args := []string{strconv.FormatInt(max(1, event.TTL.Milliseconds()), 10), strconv.FormatInt(max(1, event.ReplayTTL.Milliseconds()), 10)}
	return reputationEvent.Exec(ctx, Primary(client), keys, args).AsInt64()
}
