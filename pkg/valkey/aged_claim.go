// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

var agedClaimScript = NewScript(`
if redis.call('HGET', KEYS[1], ARGV[3]) ~= ARGV[4] then return 0 end
local claimed = redis.call('HGET', KEYS[1], ARGV[5]) or ''
if claimed == ARGV[6] then return 0 end
if claimed ~= '' and tonumber(claimed) > tonumber(ARGV[1]) - tonumber(ARGV[2]) then return 0 end
redis.call('HSET', KEYS[1], ARGV[5], ARGV[1])
return 1`)

// AgedClaim claims a hash record while Guard holds Want. A claim older than MaxAge can be taken again; Done closes it for good.
type AgedClaim struct {
	Key       string
	Guard     string
	Want      string
	Field     string
	DoneValue string
	MaxAge    time.Duration
}

func (c AgedClaim) Claim(ctx context.Context, client valkey_go.Client, now time.Time) (bool, error) {
	n, err := RunScripts(ctx, client, []ScriptCall{{
		Script: agedClaimScript,
		Key:    c.Key,
		Args: []string{
			strconv.FormatInt(now.Unix(), 10),
			strconv.Itoa(int(c.MaxAge.Seconds())),
			c.Guard,
			c.Want,
			c.Field,
			c.DoneValue,
		},
	}})[0].ToInt64()
	return n == 1, err
}

func (c AgedClaim) Done(ctx context.Context, client valkey_go.Client) error {
	return HashField{Key: c.Key, Field: c.Field}.Set(ctx, client, c.DoneValue)
}
