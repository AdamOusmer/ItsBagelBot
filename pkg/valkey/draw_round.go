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
	Joinable, Active, Round, Entries, Labels, Slots, Claimable, Due, Lease, Refused string
}

type RoundOpen struct {
	Wake           DrawWake
	RoundID        string
	Legacy         string
	LegacyDeadline string
	Fields         []FieldValue
	TTL            time.Duration
	DueAt          time.Duration
	OpenedMs       int64
	Seen           string
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
local joinable, round, due, active, legacy, seen_round, legacy_deadline = unpack(KEYS)
local id, ttl, after, opened, seen, wake, at = unpack(ARGV, 1, 7)
local current = redis.call('GET', joinable) or ''
if current ~= seen then return 1 end
if seen ~= '' and redis.call('EXISTS', seen_round) == 1 then return 1 end
if redis.call('EXISTS', round) == 1 then return 1 end
if redis.call('EXISTS', legacy, legacy_deadline) > 0 then return 2 end
redis.call('SET', joinable, id, 'PX', ttl)
redis.call('HSET', round, unpack(ARGV, 8))
redis.call('PEXPIRE', round, ttl)
redis.call('ZADD', active, opened, id)
redis.call('PEXPIRE', active, ttl)
if wake == '' then
  redis.call('ZADD', due, now_ms() + tonumber(after), id)
else
  redis.call('ZADD', due, at, id)
  redis.call('PUBLISH', wake, id)
end
return 0
`)

func (k RoundKeys) Open(ctx context.Context, client valkey_go.Client, o RoundOpen) (int64, error) {
	seen, _, err := ReadString(ctx, client, k.Joinable)
	if err != nil {
		return 0, err
	}
	o.Seen = seen
	keys := []string{k.Joinable, k.Round, k.Due, k.Active, drawOptionalKey(o.Legacy, k.Round),
		k.joinableRound(o), drawOptionalKey(o.LegacyDeadline, k.Round)}
	args := []string{
		o.RoundID, strconv.FormatInt(o.TTL.Milliseconds(), 10), strconv.FormatInt(o.DueAt.Milliseconds(), 10),
		strconv.FormatInt(o.OpenedMs, 10), o.Seen, o.Wake.Channel, strconv.FormatInt(o.Wake.At.UnixMilli(), 10),
	}
	return drawOpenScript.Exec(ctx, client, keys, appendDrawFields(args, o.Fields)).AsInt64()
}

func drawOptionalKey(key, fallback string) string {
	if key == "" {
		return fallback
	}
	return key
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
	CheckDeadline bool
	Channel       uint64
}

type JoinResult struct {
	Open, Added bool
	Count       int64
}

var drawJoinScript = NewScript(drawTimeLua + `
local round, entries, labels = unpack(KEYS)
local member, label, weight, ttl, check_deadline, channel = unpack(ARGV)
if channel ~= '0' and redis.call('HGET', round, 'channel') ~= channel then return {0, 0, 0} end
local deadline = tonumber(redis.call('HGET', round, 'closes_at') or 0)
if check_deadline == 'true' and deadline <= now_ms() then return {0, 0, redis.call('ZCARD', entries)} end
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
	args := []string{e.Member, e.Label, strconv.FormatInt(e.Weight, 10), strconv.FormatInt(e.TTL.Milliseconds(), 10), strconv.FormatBool(e.CheckDeadline), strconv.FormatUint(e.Channel, 10)}
	reply, err := drawJoinScript.Exec(ctx, client, keys, args).AsIntSlice()
	if err != nil {
		return JoinResult{}, err
	}
	return JoinResult{Open: reply[0] == 1, Added: reply[1] == 1, Count: reply[2]}, nil
}

type Refusal struct {
	Viewer, Reason string
	TTL            time.Duration
}

var drawRefuseScript = NewScript(`
local refused = KEYS[1]
local viewer, reason, ttl = unpack(ARGV)
local first = redis.call('HSETNX', refused, viewer, reason)
redis.call('PEXPIRE', refused, ttl)
return {first, redis.call('HLEN', refused)}
`)

func (k RoundKeys) Refuse(ctx context.Context, client valkey_go.Client, r Refusal) (bool, int64, error) {
	args := []string{r.Viewer, r.Reason, strconv.FormatInt(r.TTL.Milliseconds(), 10)}
	reply, err := drawRefuseScript.Exec(ctx, client, []string{k.Refused}, args).AsIntSlice()
	if err != nil {
		return false, 0, err
	}
	return reply[0] == 1, reply[1], nil
}

type DrawWake struct {
	Channel string
	At      time.Time
}

type PhaseShift struct {
	From, To string
	Release  bool
	RoundID  string
	Fields   []FieldValue
	Action   string
	Seed     []FieldValue
	TTL      time.Duration
	Match    FieldValue
	Wake     DrawWake
}

const releaseDrawJoinableLua = `
local function release_joinable(joinable, id)
  if redis.call('GET', joinable) == id then redis.call('DEL', joinable) end
end
`

const drawActionLua = `
local function add_action(round, action)
  if action == '' then return end
  local actions = cjson.decode(redis.call('HGET', round, 'actions') or '[]')
  actions[#actions + 1] = cjson.decode(action)
  redis.call('HSET', round, 'actions', cjson.encode(actions))
end
`

const drawWakeLua = `
local function wake_draw(round, due, id, channel, at)
  if channel == '' then return end
  redis.call('HINCRBY', round, 'revision', 1)
  redis.call('ZADD', due, at, id)
  redis.call('PUBLISH', channel, id)
end
`

var drawPhaseScript = NewScript(releaseDrawJoinableLua + drawActionLua + drawWakeLua + `
local round, entries, labels, slots, joinable, due = unpack(KEYS)
local from, to, release, id, action, ttl, count, field, expected, wake, at = unpack(ARGV, 1, 11)
if redis.call('HGET', round, 'phase') ~= from then return 0 end
if field ~= '' and (redis.call('HGET', round, field) or '') ~= expected then return 0 end

local next_seed = 12 + 2 * tonumber(count)
redis.call('HSET', round, 'phase', to, unpack(ARGV, 12, next_seed - 1))
add_action(round, action)
for i = next_seed, #ARGV, 2 do redis.call('HSETNX', slots, ARGV[i], ARGV[i + 1]) end
if release == 'true' then release_joinable(joinable, id) end
for i = 1, 4 do redis.call('PEXPIRE', KEYS[i], ttl) end
redis.call('PEXPIRE', KEYS[7], ttl)
redis.call('PEXPIRE', KEYS[8], ttl)
wake_draw(round, due, id, wake, at)
return 1
`)

func (k RoundKeys) Shift(ctx context.Context, client valkey_go.Client, s PhaseShift) (bool, error) {
	keys := []string{k.Round, k.Entries, k.Labels, k.Slots, k.Joinable, drawOptionalKey(k.Due, k.Round),
		drawOptionalKey(k.Active, k.Round), drawOptionalKey(k.Refused, k.Round)}
	args := []string{
		s.From, s.To, strconv.FormatBool(s.Release), s.RoundID, s.Action,
		strconv.FormatInt(s.TTL.Milliseconds(), 10), strconv.Itoa(len(s.Fields)),
		s.Match.Field, s.Match.Value, s.Wake.Channel, strconv.FormatInt(s.Wake.At.UnixMilli(), 10),
	}
	args = appendDrawFields(appendDrawFields(args, s.Fields), s.Seed)
	n, err := drawPhaseScript.Exec(ctx, client, keys, args).AsInt64()
	return n == 1, err
}

type DrawSchedule struct {
	RoundID, Revision string
	After, TTL        time.Duration
}

var drawScheduleScript = NewScript(drawTimeLua + `
local revision = redis.call('HGET', KEYS[1], 'revision') or ''
if revision ~= ARGV[2] or redis.call('EXISTS', KEYS[1]) == 0 then return 0 end
local ttl = tonumber(ARGV[4])
if ttl > 0 then
  redis.call('PEXPIRE', KEYS[1], ttl)
  for i = 3, #KEYS do redis.call('PEXPIRE', KEYS[i], ttl) end
end
return redis.call('ZADD', KEYS[2], now_ms() + tonumber(ARGV[3]), ARGV[1])
`)

func (k RoundKeys) Schedule(ctx context.Context, client valkey_go.Client, s DrawSchedule) error {
	args := []string{s.RoundID, s.Revision, strconv.FormatInt(s.After.Milliseconds(), 10), strconv.FormatInt(s.TTL.Milliseconds(), 10)}
	keys := []string{k.Round, k.Due, drawOptionalKey(k.Entries, k.Round), drawOptionalKey(k.Labels, k.Round),
		drawOptionalKey(k.Slots, k.Round), drawOptionalKey(k.Active, k.Round), drawOptionalKey(k.Refused, k.Round)}
	return drawScheduleScript.Exec(ctx, client, keys, args).Error()
}

func (k RoundKeys) Metadata(ctx context.Context, client valkey_go.Client) (map[string]string, error) {
	return client.Do(ctx, client.B().Hgetall().Key(k.Round).Build()).AsStrMap()
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
	EntryCount           int64
	Found                bool
	Refused              int64
}

type ScoredMember struct {
	Member string
	Score  int64
}

func (k RoundKeys) View(ctx context.Context, client valkey_go.Client) (RoundRead, error) {
	return k.read(ctx, client, false)
}

func (k RoundKeys) Read(ctx context.Context, client valkey_go.Client) (RoundRead, error) {
	return k.read(ctx, client, true)
}

func (k RoundKeys) read(ctx context.Context, client valkey_go.Client, withPool bool) (RoundRead, error) {
	b := client.B()
	commands := []valkey_go.Completed{b.Hgetall().Key(k.Round).Build(), b.Hgetall().Key(k.Slots).Build(),
		b.Hlen().Key(k.Refused).Build()}
	if withPool {
		commands = append(commands, b.Zrange().Key(k.Entries).Min("0").Max("-1").Withscores().Build(),
			b.Hgetall().Key(k.Labels).Build())
	} else {
		commands = append(commands, b.Zcard().Key(k.Entries).Build())
	}
	results := client.DoMulti(ctx, commands...)
	var read RoundRead
	if err := read.decodeOverview(results); err != nil {
		return RoundRead{}, err
	}
	if withPool {
		err := read.decodePool(results[3:])
		return read, err
	}
	count, err := results[3].AsInt64()
	read.EntryCount = count
	return read, err
}

func (r *RoundRead) decodeOverview(results []valkey_go.ValkeyResult) error {
	for i, dst := range []*map[string]string{&r.Round, &r.Slots} {
		fields, err := results[i].AsStrMap()
		if err != nil {
			return err
		}
		*dst = fields
	}
	r.Found = len(r.Round) != 0
	var err error
	r.Refused, err = results[2].AsInt64()
	return err
}

func (r *RoundRead) decodePool(results []valkey_go.ValkeyResult) error {
	if err := r.readScores(results[0]); err != nil {
		return err
	}
	r.EntryCount = int64(len(r.Entries))
	var err error
	r.Labels, err = results[1].AsStrMap()
	return err
}

func (r *RoundRead) readScores(result valkey_go.ValkeyResult) error {
	scores, err := result.AsZScores()
	if err != nil {
		return err
	}
	r.Entries = make([]ScoredMember, 0, len(scores))
	for _, entry := range scores {
		r.Entries = append(r.Entries, ScoredMember{Member: entry.Member, Score: int64(entry.Score)})
	}
	return nil
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
local round, entries, labels, slots, lease, due, active, joinable, claimable, refused = unpack(KEYS)
local id = ARGV[1]
redis.call('DEL', round, entries, labels, slots, lease, refused)
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
	keys := []string{k.Round, k.Entries, k.Labels, k.Slots, k.Lease, k.Due, k.Active, k.Joinable, k.Claimable, drawOptionalKey(k.Refused, k.Round)}
	return drawTeardownScript.Exec(ctx, client, keys, []string{roundID}).Error()
}
