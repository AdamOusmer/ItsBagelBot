// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Outcome claim codes returned by OutcomeClaim.Claim.
const (
	OutcomeClaimNone    int64 = 1
	OutcomeClaimAlready int64 = 2
	OutcomeClaimLate    int64 = 3
	OutcomeClaimOK      int64 = 4
)

var claimOutcomeScript = valkey_go.NewLuaScript(`
local r = redis.call('HGET', KEYS[1], 'result')
if not r then return 1 end
local ok, res = pcall(cjson.decode, r)
if not ok then return 1 end
local found = false
for _, w in ipairs(res.winners or {}) do
  if type(w) == 'table' and w.id == ARGV[1] then found = true end
end
if not found then return 1 end
local claims = {}
local c = redis.call('HGET', KEYS[1], 'claims')
if c then
  local ok2, list = pcall(cjson.decode, c)
  if ok2 then claims = list end
end
for _, cl in ipairs(claims) do
  if cl == ARGV[1] then return 2 end
end
if tonumber(ARGV[3]) > tonumber(res.drawn_at or 0) + tonumber(ARGV[2]) * 1000 then
  return 3
end
table.insert(claims, ARGV[1])
redis.call('HSET', KEYS[1], 'claims', cjson.encode(claims))
return 4
`)

// OutcomeClaim lets one entrant claim its place among a JSON-encoded outcome's listed winners, within a window after the outcome was drawn.
type OutcomeClaim struct{ Key string }

func (o OutcomeClaim) Claim(ctx context.Context, client valkey_go.Client, entrantID string, window time.Duration) (int64, error) {
	return claimOutcomeScript.Exec(ctx, client, []string{o.Key}, []string{
		entrantID,
		strconv.FormatInt(int64(window.Seconds()), 10),
		strconv.FormatInt(time.Now().UnixMilli(), 10),
	}).AsInt64()
}
