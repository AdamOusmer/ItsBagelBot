// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package projection

import (
	"ItsBagelBot/internal/domain/event/data"
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
)

// web/dashboard/src/lib/server/live-counters.ts reads these keys; change both together.
const (
	liveCounterPrefix     = "ctr:live:"
	liveBoardPrefix       = "ctr:board:v2:"
	liveBoardMemberPrefix = "ctr:board-member:v2:"
	liveBoardSeedFlag     = "ctr:board-seeded:v2:"
	liveSeenPrefix        = "ctr:seen:"
	liveSeededAtField     = "seeded_at"
	liveSeenTTLSeconds    = "900"
)

type LiveOutcome int

const (
	LiveApplied LiveOutcome = iota
	LiveSkipped
	LiveNeedsSeed
)

type CounterName string

type CounterValue struct {
	Name  CounterName
	Value int64
	Board bool
}

type LiveBatch struct {
	UserID   uint64
	MsgID    string
	StoredAt time.Time
	Deltas   []CounterValue
}

type BoardEntry struct {
	UserID uint64
	Value  int64
}

// Keep arithmetic in decimal strings: Redis Lua numbers cannot represent all
// signed BIGINT values. All values are checked before any mutation or receipt.
const liveDecimalLua = `
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

var applyLiveScript = valkey.NewLuaScript(liveDecimalLua + `
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

var seedLiveScript = valkey.NewLuaScript(liveDecimalLua + `
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

var seedBoardScript = valkey.NewLuaScript(liveDecimalLua + `
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
var deleteLiveCountersScript = valkey.NewLuaScript(`
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

func liveCounterKey(userID uint64) string { return liveCounterPrefix + strconv.FormatUint(userID, 10) }

func liveBoardKey(name CounterName) string { return liveBoardPrefix + string(name) }

func boardsFirst(values []CounterValue) ([]CounterValue, []string) {
	ordered := slices.Clone(values)
	slices.SortStableFunc(ordered, func(a, b CounterValue) int {
		switch {
		case a.Board == b.Board:
			return 0
		case a.Board:
			return -1
		default:
			return 1
		}
	})
	var boards []string
	for _, v := range ordered {
		if v.Board {
			boards = append(boards, liveBoardKey(v.Name))
		}
	}
	return ordered, boards
}

func counterArgs(head []string, values []CounterValue) []string {
	args := slices.Clone(head)
	for _, v := range values {
		args = append(args, string(v.Name), strconv.FormatInt(v.Value, 10))
	}
	return args
}

// Batches stored before the seed are already in the seeded value, so they are skipped rather than counted twice.
func (v *Store) ApplyLiveCounters(ctx context.Context, b LiveBatch) (LiveOutcome, error) {
	defer segment(ctx, "EVALSHA")()

	ordered, boards := boardsFirst(b.Deltas)
	keys := append([]string{liveCounterKey(b.UserID), liveSeenPrefix + b.MsgID}, boards...)
	args := counterArgs([]string{strconv.FormatUint(b.UserID, 10), strconv.FormatInt(b.StoredAt.UnixMilli(), 10), liveSeenTTLSeconds}, ordered)
	code, err := applyLiveScript.Exec(ctx, v.primary, keys, args).AsInt64()
	if err != nil {
		return LiveSkipped, err
	}
	return LiveOutcome(code), nil
}

func (v *Store) SeedLiveCounters(ctx context.Context, userID uint64, values []CounterValue, seededAt time.Time) error {
	for _, value := range values {
		if value.Value < 0 {
			return fmt.Errorf("negative live counter: %s", value.Name)
		}
	}
	defer segment(ctx, "EVALSHA")()

	ordered, boards := boardsFirst(values)
	keys := append([]string{liveCounterKey(userID)}, boards...)
	args := counterArgs([]string{strconv.FormatUint(userID, 10), strconv.FormatInt(seededAt.UnixMilli(), 10)}, ordered)
	return seedLiveScript.Exec(ctx, v.primary, keys, args).Error()
}

func (v *Store) GetLiveCounters(ctx context.Context, userID uint64, names []CounterName) (map[CounterName]int64, bool, error) {
	defer segment(ctx, "HMGET")()

	fields := []string{liveSeededAtField}
	for _, name := range names {
		fields = append(fields, string(name))
	}
	res, err := v.primary.Do(ctx, v.primary.B().Hmget().Key(liveCounterKey(userID)).Field(fields...).Build()).ToArray()
	if err != nil {
		return nil, false, err
	}
	if _, err := res[0].ToString(); err != nil {
		return nil, false, nil
	}
	values := make(map[CounterName]int64, len(names))
	for i, name := range names {
		raw, readErr := res[i+1].ToString()
		if valkey.IsValkeyNil(readErr) {
			values[name] = 0
			continue
		}
		if readErr != nil {
			return nil, true, readErr
		}
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 0 {
			return nil, true, fmt.Errorf("invalid live counter %s: %q", name, raw)
		}
		values[name] = value
	}
	return values, true, nil
}

func (v *Store) BoardSeeded(ctx context.Context, name CounterName) (bool, error) {
	n, err := v.primary.Do(ctx, v.primary.B().Exists().Key(liveBoardSeedFlag+string(name), liveBoardKey(name)).Build()).AsInt64()
	return n == 2, err
}

// The member index keeps a channel's fresher total when an older board seed arrives.
// Reversed lexicographic order preserves the previous board's descending user-ID ties.
func (v *Store) SeedBoard(ctx context.Context, name CounterName, entries []BoardEntry) error {
	defer segment(ctx, "ZADD")()

	args := make([]string, 0, len(entries)*2)
	for _, e := range entries {
		if e.Value < 0 || e.Value > data.MaxCounter {
			return fmt.Errorf("invalid board value: %d", e.Value)
		}
		args = append(args, strconv.FormatUint(e.UserID, 10), strconv.FormatInt(e.Value, 10))
	}
	return seedBoardScript.Exec(ctx, v.primary, []string{liveBoardKey(name), liveBoardMemberPrefix + string(name), liveBoardSeedFlag + string(name)}, args).Error()
}

func (v *Store) DeleteLiveCounters(ctx context.Context, userID uint64, boards []CounterName) error {
	defer segment(ctx, "DEL")()

	keys := []string{liveCounterKey(userID)}
	for _, name := range boards {
		keys = append(keys, liveBoardKey(name), liveBoardMemberPrefix+string(name))
	}
	return deleteLiveCountersScript.Exec(ctx, v.primary, keys, []string{strconv.FormatUint(userID, 10)}).Error()
}
