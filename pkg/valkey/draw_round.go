// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"
	"strings"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
)

type RoundKeys struct {
	Joinable, Active, Round, Entries, Labels, Slots, Claimable, Due, Lease string
}

type RoundOpen struct {
	RoundID  string
	Legacy   string
	Fields   []FieldValue
	TTL      time.Duration
	DueAt    time.Duration
	OpenedMs int64
	Seen     string
}

const (
	RoundOpened int64 = iota
	RoundJoinableBusy
	RoundLegacyBusy
)

const drawTimeLua = `
local function now_ms()
  local clock = redis.call('TIME')
  return clock[1] * 1000 + math.floor(clock[2] / 1000)
end
`

var drawOpenScript = NewScript(drawTimeLua + `
local joinable, round, due, active, legacy, seen_round = unpack(KEYS)
local id, ttl, after, opened, seen, check_legacy = unpack(ARGV, 1, 6)
local current = redis.call('GET', joinable) or ''
if current ~= seen then return 1 end
if seen ~= '' and redis.call('EXISTS', seen_round) == 1 then return 1 end
if check_legacy == 'true' and redis.call('EXISTS', legacy) == 1 then return 2 end
redis.call('SET', joinable, id, 'PX', ttl)
redis.call('HSET', round, unpack(ARGV, 7))
redis.call('PEXPIRE', round, ttl)
redis.call('ZADD', active, opened, id)
redis.call('PEXPIRE', active, ttl)
redis.call('ZADD', due, now_ms() + tonumber(after), id)
return 0
`)

func (k RoundKeys) Open(ctx context.Context, client valkey_go.Client, o RoundOpen) (int64, error) {
	seen, _, err := ReadString(ctx, client, k.Joinable)
	if err != nil {
		return 0, err
	}
	o.Seen = seen
	legacy := o.Legacy
	if legacy == "" {
		legacy = k.Round
	}
	keys := []string{k.Joinable, k.Round, k.Due, k.Active, legacy, k.joinableRound(o)}
	args := []string{
		o.RoundID, strconv.FormatInt(o.TTL.Milliseconds(), 10), strconv.FormatInt(o.DueAt.Milliseconds(), 10),
		strconv.FormatInt(o.OpenedMs, 10), o.Seen, strconv.FormatBool(o.Legacy != ""),
	}
	return drawOpenScript.Exec(ctx, client, keys, appendDrawFields(args, o.Fields)).AsInt64()
}

func (k RoundKeys) joinableRound(o RoundOpen) string {
	if o.Seen == "" {
		return k.Round
	}
	return strings.TrimSuffix(k.Round, o.RoundID) + o.Seen
}

type RoundEntrant struct {
	Member, Label string
	Weight        int64
	TTL           time.Duration
}

type JoinResult struct {
	Open, Added bool
	Count       int64
}

var drawJoinScript = NewScript(`
local round, entries, labels = unpack(KEYS)
local member, label, weight, ttl = unpack(ARGV)
if redis.call('HGET', round, 'phase') ~= 'open' then
  return {0, 0, redis.call('ZCARD', entries)}
end
local added = redis.call('ZADD', entries, 'NX', weight, member)
redis.call('HSET', labels, member, label)
redis.call('PEXPIRE', entries, ttl)
redis.call('PEXPIRE', labels, ttl)
return {1, added, redis.call('ZCARD', entries)}
`)

func (k RoundKeys) Join(ctx context.Context, client valkey_go.Client, e RoundEntrant) (JoinResult, error) {
	keys := []string{k.Round, k.Entries, k.Labels}
	args := []string{e.Member, e.Label, strconv.FormatInt(e.Weight, 10), strconv.FormatInt(e.TTL.Milliseconds(), 10)}
	reply, err := drawJoinScript.Exec(ctx, client, keys, args).AsIntSlice()
	if err != nil {
		return JoinResult{}, err
	}
	return JoinResult{Open: reply[0] == 1, Added: reply[1] == 1, Count: reply[2]}, nil
}

type PhaseShift struct {
	From, To string
	Release  bool
	RoundID  string
	Fields   []FieldValue
	Action   string
	Seed     []FieldValue
	TTL      time.Duration
}

const releaseDrawJoinableLua = `
local function release_joinable(joinable, id)
  if redis.call('GET', joinable) == id then redis.call('DEL', joinable) end
end
`

var drawPhaseScript = NewScript(releaseDrawJoinableLua + `
local round, entries, labels, slots, joinable = unpack(KEYS)
local from, to, release, id, action, ttl, count = unpack(ARGV, 1, 7)
if redis.call('HGET', round, 'phase') ~= from then return 0 end

local next_seed = 8 + 2 * tonumber(count)
local fields = {}
for i = 8, next_seed - 1 do fields[#fields + 1] = ARGV[i] end
fields[#fields + 1] = 'phase'
fields[#fields + 1] = to
if action ~= '' then
  local actions = cjson.decode(redis.call('HGET', round, 'actions') or '[]')
  actions[#actions + 1] = cjson.decode(action)
  fields[#fields + 1] = 'actions'
  fields[#fields + 1] = cjson.encode(actions)
end
redis.call('HSET', round, unpack(fields))
for i = next_seed, #ARGV, 2 do redis.call('HSETNX', slots, ARGV[i], ARGV[i + 1]) end
if release == 'true' then release_joinable(joinable, id) end
for i = 1, 4 do redis.call('PEXPIRE', KEYS[i], ttl) end
return 1
`)

func (k RoundKeys) Shift(ctx context.Context, client valkey_go.Client, s PhaseShift) (bool, error) {
	keys := []string{k.Round, k.Entries, k.Labels, k.Slots, k.Joinable}
	args := []string{
		s.From, s.To, strconv.FormatBool(s.Release), s.RoundID, s.Action,
		strconv.FormatInt(s.TTL.Milliseconds(), 10), strconv.Itoa(len(s.Fields)),
	}
	args = appendDrawFields(appendDrawFields(args, s.Fields), s.Seed)
	n, err := drawPhaseScript.Exec(ctx, client, keys, args).AsInt64()
	return n == 1, err
}

func appendDrawFields(args []string, fields []FieldValue) []string {
	for _, field := range fields {
		args = append(args, field.Field, field.Value)
	}
	return args
}

type RoundRead struct {
	Round, Slots, Labels map[string]string
	Entries              []ScoredMember
	Found                bool
}

type ScoredMember struct {
	Member string
	Score  int64
}

func (k RoundKeys) Read(ctx context.Context, client valkey_go.Client) (RoundRead, error) {
	b := client.B()
	results := client.DoMulti(ctx,
		b.Hgetall().Key(k.Round).Build(),
		b.Hgetall().Key(k.Slots).Build(),
		b.Hgetall().Key(k.Labels).Build(),
		b.Zrange().Key(k.Entries).Min("0").Max("-1").Withscores().Build(),
	)
	var read RoundRead
	for i, dst := range []*map[string]string{&read.Round, &read.Slots, &read.Labels} {
		fields, err := results[i].AsStrMap()
		if err != nil {
			return RoundRead{}, err
		}
		*dst = fields
	}
	scores, err := results[3].AsZScores()
	if err != nil {
		return RoundRead{}, err
	}
	read.Found = len(read.Round) != 0
	read.Entries = make([]ScoredMember, 0, len(scores))
	for _, entry := range scores {
		read.Entries = append(read.Entries, ScoredMember{Member: entry.Member, Score: int64(entry.Score)})
	}
	return read, nil
}

func ActiveRounds(ctx context.Context, client valkey_go.Client, active string) ([]string, error) {
	cmd := client.B().Zrevrange().Key(active).Start(0).Stop(-1).Build()
	return client.Do(ctx, cmd).AsStrSlice()
}

func ServerNow(ctx context.Context, client valkey_go.Client) (time.Time, error) {
	clock, err := client.Do(ctx, client.B().Time().Build()).AsIntSlice()
	if err != nil {
		return time.Time{}, err
	}
	return time.Unix(clock[0], clock[1]*int64(time.Microsecond)).UTC(), nil
}

var drawTeardownScript = NewScript(releaseDrawJoinableLua + drawClaimIndexLua + `
local round, entries, labels, slots, lease, due, active, joinable, claimable = unpack(KEYS)
local id = ARGV[1]
redis.call('DEL', round, entries, labels, slots, lease)
redis.call('ZREM', due, id)
redis.call('ZREM', active, id)
release_joinable(joinable, id)

local prefix = id .. ':'
local indexed = redis.call('HGETALL', claimable)
for i = 1, #indexed, 2 do
  local refs = cjson.decode(indexed[i + 1])
  drop_claim_refs(claimable, indexed[i], refs, function(ref)
    return string.sub(ref, 1, #prefix) == prefix
  end)
end
return 1
`)

func (k RoundKeys) Teardown(ctx context.Context, client valkey_go.Client, roundID string) error {
	keys := []string{k.Round, k.Entries, k.Labels, k.Slots, k.Lease, k.Due, k.Active, k.Joinable, k.Claimable}
	return drawTeardownScript.Exec(ctx, client, keys, []string{roundID}).Error()
}
