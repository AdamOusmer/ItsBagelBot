// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkeygo "github.com/valkey-io/valkey-go"
)

var effectLeases = NewScript(`
local results = {}
for i, key in ipairs(KEYS) do
  local mode = ARGV[3]
  local owner = ARGV[i+3] or ARGV[1]
  local value
  if mode == 'acquire' then
    value = redis.call('SET', key, owner, 'PX', ARGV[2], 'NX', 'GET')
  else
    value = redis.call('GET', key)
  end
  if mode == 'acquire' and not value then
    results[i] = 0
  elseif mode == 'complete' and (not value or value == owner) then
    redis.call('SET', key, '1', 'PX', ARGV[2])
    results[i] = 1
  elseif value == '1' then
    results[i] = 1
  elseif value ~= owner then
    results[i] = 2
  elseif mode == 'acquire' then
    results[i] = 0
  elseif mode == 'release' then
    redis.call('DEL', key)
    results[i] = 0
  else
    redis.call('PEXPIRE', key, ARGV[2])
    results[i] = 0
  end
end
return results
`)

type EffectLeases struct {
	Keys   []string
	Owner  string
	Owners []string
	TTL    time.Duration
	Mode   string
}

func UpdateEffectLeases(ctx context.Context, client valkeygo.Client, batch EffectLeases) ([]int64, error) {
	if len(batch.Keys) == 0 {
		return nil, nil
	}
	args := []string{batch.Owner, strconv.FormatInt(max(1, batch.TTL.Milliseconds()), 10), batch.Mode}
	args = append(args, batch.Owners...)
	return effectLeases.Exec(ctx, Primary(client), batch.Keys, args).AsIntSlice()
}
