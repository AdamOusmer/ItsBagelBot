// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

type RaffleClaim int

const (
	ClaimOk RaffleClaim = iota
	ClaimAlready
	ClaimLate
	ClaimNone
)

const (
	claimNoneCode    = 1
	claimAlreadyCode = 2
	claimLateCode    = 3
	claimOkCode      = 4
)

var claimScript = valkey.NewLuaScript(`
local r = redis.call('HGET', KEYS[1], 'result')
if not r then return 1 end
local ok, res = pcall(cjson.decode, r)
if not ok then return 1 end
local found = false
for _, w in ipairs(res.winners or {}) do
  if w == ARGV[1] then found = true end
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

func (s *ValkeyRaffleStore) Claim(ctx context.Context, broadcasterID uint64, userID string) (RaffleClaim, error) {
	if userID == "" {
		return ClaimNone, nil
	}
	code, err := claimScript.Exec(ctx, s.client,
		[]string{raffleKey(raffleLastPrefix, broadcasterID)},
		[]string{userID,
			strconv.FormatInt(int64(raffleClaimWindow.Seconds()), 10),
			strconv.FormatInt(time.Now().UnixMilli(), 10)},
	).AsInt64()
	if err != nil {
		return ClaimNone, err
	}
	switch code {
	case claimOkCode:
		return ClaimOk, nil
	case claimAlreadyCode:
		return ClaimAlready, nil
	case claimLateCode:
		return ClaimLate, nil
	default:
		return ClaimNone, nil
	}
}
