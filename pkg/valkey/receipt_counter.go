// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"errors"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var receiptCounterScript = valkey_go.NewLuaScript(`
local today = redis.call('GET', KEYS[1]) or '0'
local limit = '9223372036854775807'
local function valid(v)
  return string.match(v, '^%d+$') and (string.len(v) < 19 or (string.len(v) == 19 and v <= limit))
end
if not valid(today) or not valid(ARGV[1]) then
  return redis.error_reply('counter outside signed integer range')
end
if redis.call('EXISTS', KEYS[2]) == 1 then return {today, ARGV[1]} end
if today == limit then return redis.error_reply('counter at signed integer limit') end
redis.call('INCR', KEYS[1])
today = redis.call('GET', KEYS[1])
if today == '1' then redis.call('EXPIRE', KEYS[1], ARGV[2]) end
redis.call('SET', KEYS[2], '1', 'EX', ARGV[3])
return {today, ARGV[1]}`)

// ReceiptCounter increments a TTL'd counter at most once per receipt, replaying the prior value for a receipt already seen,
// and echoes back a caller-supplied passthrough total after validating it fits a signed 64-bit integer.
type ReceiptCounter struct {
	Counter    string
	CounterTTL time.Duration
}

// Bump returns the counter's value after this call and the validated passthrough total.
// receipt names the per-receipt dedupe marker and its TTL.
func (c ReceiptCounter) Bump(ctx context.Context, client valkey_go.Client, receipt Key, passthrough int64) (today, total int64, err error) {
	result := receiptCounterScript.Exec(ctx, client, []string{c.Counter, receipt.Name}, []string{
		strconv.FormatInt(passthrough, 10),
		strconv.FormatInt(int64(c.CounterTTL.Seconds()), 10),
		strconv.FormatInt(int64(receipt.TTL.Seconds()), 10),
	})
	values, err := result.ToArray()
	if err != nil {
		return 0, 0, err
	}
	if len(values) != 2 {
		return 0, 0, errors.New("valkey: invalid receipt counter result")
	}
	if today, err = decodeNonNegative(values[0]); err != nil {
		return 0, 0, err
	}
	if total, err = decodeNonNegative(values[1]); err != nil {
		return 0, 0, err
	}
	return today, total, nil
}

func decodeNonNegative(value valkey_go.ValkeyMessage) (int64, error) {
	count, err := value.AsInt64()
	if err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, errors.New("valkey: counter outside signed integer range")
	}
	return count, nil
}
