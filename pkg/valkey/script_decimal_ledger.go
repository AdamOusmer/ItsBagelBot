// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"

	valkey_go "github.com/valkey-io/valkey-go"
)

// Keep arithmetic in decimal strings: Redis Lua numbers cannot represent all
// signed BIGINT values. All values are checked before any mutation or receipt.
const decimalLedgerPrelude = `
local function trim(s)
 s = string.gsub(s, '^0+', '')
 if s == '' then return '0' end
 return s
end
local function cmp(a, b)
 if #a ~= #b then return #a < #b and -1 or 1 end
 if a == b then return 0 end
 return a < b and -1 or 1
end
local function add(a, delta)
 local negative = string.sub(delta, 1, 1) == '-'
 local b = negative and string.sub(delta, 2) or delta
 if not string.match(a, '^%d+$') or not string.match(b, '^%d+$') then return nil end
 a, b = trim(a), trim(b)
 if negative and cmp(a,b) < 0 then return nil end
 local out, carry, j = '', 0, #b
 for i = #a, 1, -1 do
  local x = tonumber(string.sub(a,i,i))
  local y = j > 0 and tonumber(string.sub(b,j,j)) or 0
  local digit
  if negative then
   digit = x-y-carry
   carry = digit < 0 and 1 or 0
   if digit < 0 then digit = digit+10 end
  else
   digit = x+y+carry
   carry = math.floor(digit/10)
   digit = digit%10
  end
  out = tostring(digit)..out
  j = j-1
 end
 while j > 0 do
  local digit = tonumber(string.sub(b,j,j))+carry
  carry = math.floor(digit/10)
  out = tostring(digit%10)..out
  j = j-1
 end
 if carry > 0 then out = tostring(carry)..out end
 out = trim(out)
 if cmp(out,'9223372036854775807') > 0 then return nil end
 return out
end
local function validateBoard(key)
 local members = 'ctr:board-member:v2:'..string.sub(key,#'ctr:board:v2:'+1)
 redis.call('ZCARD',key)
 redis.call('HLEN',members)
end
local function board(key, user, value)
 local members = 'ctr:board-member:v2:'..string.sub(key,#'ctr:board:v2:'+1)
 local old = redis.call('HGET',members,user)
 if old then redis.call('ZREM',key,old) end
 local member = string.rep('0',19-#value)..value..':'..user
 redis.call('ZADD',key,0,member)
 redis.call('HSET',members,user,member)
end
`

var decimalLedgerApplyScript = valkey_go.NewLuaScript(decimalLedgerPrelude + `
local seeded = redis.call('HGET', KEYS[1], 'seeded_at')
if not seeded then return 2 end
if tonumber(ARGV[2]) < tonumber(seeded) then return 1 end
if redis.call('EXISTS',KEYS[2]) == 1 then return 1 end
for k = 3, #KEYS do validateBoard(KEYS[k]) end
local totals = {}
local i = 4
while ARGV[i] do
 local old = totals[ARGV[i]] or redis.call('HGET',KEYS[1],ARGV[i]) or '0'
 local value = add(old,ARGV[i+1])
 if not value then return redis.error_reply('counter outside signed BIGINT range') end
 totals[ARGV[i]] = value
 i = i+2
end
redis.call('SET', KEYS[2], '1', 'EX', ARGV[3])
i = 4
local n = 0
while ARGV[i] do
  local total = totals[ARGV[i]]
  redis.call('HSET', KEYS[1], ARGV[i], total)
  n = n + 1
  if KEYS[2 + n] then board(KEYS[2+n],ARGV[1],total) end
  i = i + 2
end
return 0`)

var decimalLedgerSeedScript = valkey_go.NewLuaScript(decimalLedgerPrelude + `
if redis.call('HEXISTS', KEYS[1], 'seeded_at') == 1 then return 0 end
for k = 2, #KEYS do validateBoard(KEYS[k]) end
local i = 3
local n = 0
while ARGV[i] do
  redis.call('HSET', KEYS[1], ARGV[i], ARGV[i + 1])
  n = n + 1
  if KEYS[1 + n] then board(KEYS[1+n],ARGV[1],ARGV[i+1]) end
  i = i + 2
end
redis.call('HSET', KEYS[1], 'seeded_at', ARGV[2])
return 1`)

var decimalLedgerSeedBoardScript = valkey_go.NewLuaScript(decimalLedgerPrelude + `
validateBoard(KEYS[1])
local i = 1
while ARGV[i] do
 local existing = redis.call('HGET',KEYS[2],ARGV[i])
 if existing then
  redis.call('ZADD',KEYS[1],'NX',0,existing)
 else
  board(KEYS[1],ARGV[i],ARGV[i+1])
 end
 i = i+2
end
redis.call('SET',KEYS[3],'1')
return 1`)

// Read and validate every board before removing anything; a type error must
// not leave the hash and leaderboard indexes only partly deleted.
var decimalLedgerDeleteScript = valkey_go.NewLuaScript(`
local members = {}
for i = 2, #KEYS, 2 do
 members[i] = redis.call('HGET',KEYS[i+1],ARGV[1])
 redis.call('ZCARD',KEYS[i])
end
for i = 2, #KEYS, 2 do
 if members[i] then redis.call('ZREM',KEYS[i],members[i]) end
 redis.call('HDEL',KEYS[i+1],ARGV[1])
end
redis.call('DEL',KEYS[1])
return 1`)

// RunDecimalLedgerApply runs the idempotent decimal-counter batch apply script.
func RunDecimalLedgerApply(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return decimalLedgerApplyScript.Exec(ctx, client, keys, args)
}

// RunDecimalLedgerSeed runs the one-time decimal-counter seed script.
func RunDecimalLedgerSeed(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return decimalLedgerSeedScript.Exec(ctx, client, keys, args)
}

// RunDecimalLedgerSeedBoard runs the one-time leaderboard seed script.
func RunDecimalLedgerSeedBoard(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return decimalLedgerSeedBoardScript.Exec(ctx, client, keys, args)
}

// RunDecimalLedgerDelete runs the decimal-counter and leaderboard deletion script.
func RunDecimalLedgerDelete(ctx context.Context, client valkey_go.Client, keys, args []string) valkey_go.ValkeyResult {
	return decimalLedgerDeleteScript.Exec(ctx, client, keys, args)
}
