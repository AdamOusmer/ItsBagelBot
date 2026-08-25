// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package youtube

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// The project's YouTube quota is a hard daily budget (default 10,000 units,
// reset at midnight Pacific per Google's docs), shared by EVERYTHING that
// calls the API under the project's credentials: this service's actions at 50
// units each, and every other consumer. Unlike a rate limit there is no
// refill within the day — spending early must not let the bucket regenerate.
//
// The fleet runs three replicas against one project, so the ledger lives in
// Valkey: one counter per UTC date key, take-N-or-nothing in one Lua call so
// a denied action consumes nothing and an admitted one is charged exactly
// once. Keys expire two days after creation so yesterday's ledger cleans
// itself up.

// luaTakeBudget charges cost units against key if doing so stays within
// limit, else refuses without consuming. Returns 1 admitted / 0 refused.
//
// Deliberately NOT retried on connection failure (the same choice
// pkg/ratelimit makes): after a failed send we cannot know whether the INCRBY
// landed, and an automatic retry could double-charge the day. A dropped
// charge under-charges (safe); a doubled one over-charges real quota.
const luaTakeBudget = `
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
local cost = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
if used + cost > limit then
  return 0
end
redis.call('INCRBY', KEYS[1], cost)
redis.call('EXPIRE', KEYS[1], tonumber(ARGV[3]))
return 1
`

// Budget is the fleet-shared daily-quota ledger.
type Budget struct {
	client valkey.Client
	script *valkey.Lua
	// limit is the daily unit ceiling for THIS consumer (the project total
	// minus what every other API consumer needs).
	limit int64
	ttl   int64
	now   func() time.Time
}

func NewBudget(client valkey.Client, dailyUnitLimit int64) *Budget {
	return &Budget{
		client: client,
		script: valkey.NewLuaScript(luaTakeBudget),
		limit:  dailyUnitLimit,
		ttl:    int64((48 * time.Hour).Seconds()),
		now:    time.Now,
	}
}

// Take charges cost units for today, reporting whether they were available.
// A false return with nil error means "refused" — the worker drops the
// message rather than nacking, because no amount of redelivery frees units
// before tomorrow.
func (b *Budget) Take(ctx context.Context, cost int64) (bool, error) {
	key := b.key(b.now().UTC())

	res := b.script.Exec(
		ctx,
		b.client,
		[]string{key},
		[]string{
			strconv.FormatInt(cost, 10),
			strconv.FormatInt(b.limit, 10),
			strconv.FormatInt(b.ttl, 10),
		},
	)
	if err := res.Error(); err != nil {
		return false, err
	}

	allowed, err := res.AsInt64()
	if err != nil {
		return false, err
	}
	return allowed == 1, nil
}

// key builds today's ledger key. Day-keyed, not one key reset at midnight:
// a date rollover naturally starts a full budget with zero coordination, and
// the old key expires itself.
func (b *Budget) key(now time.Time) string {
	return "ratelimit:yt:quota:" + now.Format("2006-01-02")
}
