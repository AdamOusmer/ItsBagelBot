// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"

	"github.com/valkey-io/valkey-go"
)

const (
	durableKeyPrefix   = "automation:durable:"
	durableReplicasKey = durableKeyPrefix + "replicas"
	durableWakeChannel = durableKeyPrefix + "wake"
	durableKeyTTL      = time.Hour
	durableLapsedLimit = 32
)

const durableOwnerState = `
local function state(id, owner)
  local current = redis.call('HGET', KEYS[2], id)
  if current == owner then
    return 1
  end
  if current or redis.call('EXISTS', KEYS[4]) == 1 then
    return -1
  end
  return 0
end
`

var saveDurableScript = valkey.NewLuaScript(durableOwnerState + `
local standing = state(ARGV[1], ARGV[2])
if standing == -1 or (standing == 1 and ARGV[4] == '1') then
  return -1
end
if standing == 0 and ARGV[5] ~= '' then
  redis.call('HINCRBY', KEYS[3], ARGV[5], 1)
end
redis.call('HSET', KEYS[1], ARGV[1], ARGV[3])
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2])
for i = 1, 3 do
  redis.call('PEXPIRE', KEYS[i], ARGV[6])
end
if ARGV[5] ~= '' then
  redis.call('PEXPIRE', KEYS[5], ARGV[6])
  redis.call('PEXPIRE', KEYS[6], ARGV[6])
end
return 1
`)

var standingDurableScript = valkey.NewLuaScript(durableOwnerState + `
return state(ARGV[1], ARGV[2])
`)

var finishDurableScript = valkey.NewLuaScript(durableOwnerState + `
local standing = state(ARGV[1], ARGV[2])
if standing ~= 1 then
  return standing
end
redis.call('HDEL', KEYS[1], ARGV[1])
redis.call('HDEL', KEYS[2], ARGV[1])
if ARGV[3] ~= '' and redis.call('HINCRBY', KEYS[3], ARGV[3], -1) <= 0 then
  redis.call('HDEL', KEYS[3], ARGV[3])
end
return 1
`)

var claimDurableScript = valkey.NewLuaScript(`
local current = redis.call('HGET', KEYS[2], ARGV[1])
if current == ARGV[3] then
  return {2, ''}
end
if current ~= ARGV[2] then
  return {0, ''}
end
redis.call('HSET', KEYS[2], ARGV[1], ARGV[3])
redis.call('SET', KEYS[4], '1', 'PX', ARGV[4])
return {1, redis.call('HGET', KEYS[1], ARGV[1])}
`)

var renewDurableScript = valkey.NewLuaScript(`
local t = redis.call('TIME')
return redis.call('ZADD', KEYS[1], t[1] * 1000 + math.floor(t[2] / 1000) + tonumber(ARGV[2]), ARGV[1])
`)

var lapsedDurableScript = valkey.NewLuaScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local lapsed = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', now, 'LIMIT', 0, ARGV[1])
local upcoming = redis.call('ZRANGEBYSCORE', KEYS[1], '(' .. now, '+inf', 'WITHSCORES', 'LIMIT', 0, 1)
local due = -1
if #upcoming > 0 then
  due = tonumber(upcoming[2]) - now
end
return {lapsed, due}
`)

// ValkeyDurableRuns keeps per-channel records under one hash tag; only the replica lease index and each replica's
// owned set are shared, so a sweep touches dead replicas and never walks channels.
type ValkeyDurableRuns struct {
	client valkey.Client
	pubsub valkey.Client
}

func NewValkeyDurableRuns(client valkey.Client) *ValkeyDurableRuns {
	return &ValkeyDurableRuns{client: pkg_valkey.Primary(client), pubsub: client}
}

func durableChannelBase(broadcasterID uint64) string {
	return durableKeyPrefix + "{" + strconv.FormatUint(broadcasterID, 10) + "}:"
}

func durableSeatsKey(broadcasterID uint64) string {
	return durableChannelBase(broadcasterID) + "seats"
}

func durableRecordKeys(ref DurableRef) []string {
	base := durableChannelBase(ref.BroadcasterID)
	return []string{base + "records", base + "owners", base + "seats", base + "taken:" + ref.ID}
}

type durableOwner string

type durableMember string

type durableHandover struct {
	from durableOwner
	to   durableOwner
}

func durableOwnedKey(owner durableOwner) string {
	return durableKeyPrefix + "owned:{" + string(owner) + "}"
}

func memberOf(ref DurableRef) durableMember {
	return durableMember(strconv.FormatUint(ref.BroadcasterID, 10) + ":" + ref.ID)
}

func memberStrings(members []durableMember) []string {
	out := make([]string, len(members))
	for i, member := range members {
		out[i] = string(member)
	}
	return out
}

func parseDurableMember(member durableMember) (DurableRef, bool) {
	raw, id, ok := strings.Cut(string(member), ":")
	broadcasterID, err := strconv.ParseUint(raw, 10, 64)
	if !ok || err != nil {
		return DurableRef{}, false
	}
	return DurableRef{BroadcasterID: broadcasterID, ID: id}, true
}

func durableTTLMillis() string {
	return strconv.FormatInt(durableKeyTTL.Milliseconds(), 10)
}

func (s *ValkeyDurableRuns) own(ctx context.Context, owner durableOwner, members ...durableMember) error {
	key := durableOwnedKey(owner)
	results := s.client.DoMulti(ctx,
		s.client.B().Sadd().Key(key).Member(memberStrings(members)...).Build(),
		s.client.B().Pexpire().Key(key).Milliseconds(durableKeyTTL.Milliseconds()).Build(),
	)
	return results[0].Error()
}

func (s *ValkeyDurableRuns) disown(ctx context.Context, owner durableOwner, members ...durableMember) error {
	return s.client.Do(ctx, s.client.B().Srem().Key(durableOwnedKey(owner)).Member(memberStrings(members)...).Build()).Error()
}

// The owned entry goes first: a crash between the writes leaves a stale entry a sweep drops, never an unreachable record.
func (s *ValkeyDurableRuns) Save(ctx context.Context, owner string, save DurableSave) (DurableStanding, error) {
	if err := s.own(ctx, durableOwner(owner), memberOf(save.Ref)); err != nil {
		return DurableMissing, err
	}
	fresh := ""
	if save.Fresh {
		fresh = "1"
	}
	queueKeys := runQueueKeys(save.Seat)
	keys := append(durableRecordKeys(save.Ref), queueKeys[0], queueKeys[1])
	args := []string{save.Ref.ID, owner, string(save.Body), fresh, save.Seat.Ticket, durableTTLMillis()}
	standing, err := saveDurableScript.Exec(ctx, s.client, keys, args).AsInt64()
	return DurableStanding(standing), err
}

func (s *ValkeyDurableRuns) Standing(ctx context.Context, owner string, ref DurableRef) (DurableStanding, error) {
	standing, err := standingDurableScript.Exec(ctx, s.client, durableRecordKeys(ref), []string{ref.ID, owner}).AsInt64()
	return DurableStanding(standing), err
}

func (s *ValkeyDurableRuns) Finish(ctx context.Context, owner string, ref DurableRef, ticket string) (DurableStanding, error) {
	standing, err := finishDurableScript.Exec(ctx, s.client, durableRecordKeys(ref), []string{ref.ID, owner, ticket}).AsInt64()
	if err != nil {
		return DurableMissing, err
	}
	return DurableStanding(standing), s.disown(ctx, durableOwner(owner), memberOf(ref))
}

func (s *ValkeyDurableRuns) Renew(ctx context.Context, owner string, lease time.Duration) error {
	args := []string{owner, strconv.FormatInt(lease.Milliseconds(), 10)}
	return renewDurableScript.Exec(ctx, s.client, []string{durableReplicasKey}, args).Error()
}

func (s *ValkeyDurableRuns) Release(ctx context.Context, owner string) error {
	results := s.client.DoMulti(ctx,
		s.client.B().Zadd().Key(durableReplicasKey).ScoreMember().ScoreMember(0, owner).Build(),
		s.client.B().Publish().Channel(durableWakeChannel).Message(owner).Build(),
	)
	return results[0].Error()
}

func (s *ValkeyDurableRuns) Lapsed(ctx context.Context) ([]string, time.Duration, error) {
	reply, err := lapsedDurableScript.Exec(ctx, s.client, []string{durableReplicasKey}, []string{strconv.Itoa(durableLapsedLimit)}).ToArray()
	if err != nil {
		return nil, 0, err
	}
	lapsed, err := reply[0].AsStrSlice()
	if err != nil {
		return nil, 0, err
	}
	due, err := reply[1].AsInt64()
	return lapsed, time.Duration(due) * time.Millisecond, err
}

func (s *ValkeyDurableRuns) Claim(ctx context.Context, from, to string, limit int) ([]DurableClaim, bool, error) {
	handover := durableHandover{from: durableOwner(from), to: durableOwner(to)}
	raw, err := s.client.Do(ctx, s.client.B().Srandmember().Key(durableOwnedKey(handover.from)).Count(int64(limit)).Build()).AsStrSlice()
	if err != nil {
		return nil, false, err
	}
	if len(raw) == 0 {
		return nil, true, s.client.Do(ctx, s.client.B().Zrem().Key(durableReplicasKey).Member(from).Build()).Error()
	}
	members := make([]durableMember, len(raw))
	for i, member := range raw {
		members[i] = durableMember(member)
	}
	if err := s.own(ctx, handover.to, members...); err != nil {
		return nil, false, err
	}
	claims := make([]DurableClaim, 0, len(members))
	for _, member := range members {
		claim, claimed, err := s.claimOne(ctx, handover, member)
		if err != nil {
			return claims, false, err
		}
		if claimed {
			claims = append(claims, claim)
		}
	}
	return claims, false, nil
}

const (
	durableClaimGone = 0
	durableClaimWon  = 1
)

func (s *ValkeyDurableRuns) claimOne(ctx context.Context, handover durableHandover, member durableMember) (DurableClaim, bool, error) {
	ref, ok := parseDurableMember(member)
	if !ok {
		return DurableClaim{}, false, s.disown(ctx, handover.from, member)
	}
	args := []string{ref.ID, string(handover.from), string(handover.to), durableTTLMillis()}
	reply, err := claimDurableScript.Exec(ctx, s.client, durableRecordKeys(ref), args).ToArray()
	if err != nil {
		return DurableClaim{}, false, err
	}
	status, err := reply[0].AsInt64()
	if err != nil {
		return DurableClaim{}, false, err
	}
	if status == durableClaimGone {
		_ = s.disown(ctx, handover.to, member)
	}
	if err := s.disown(ctx, handover.from, member); err != nil {
		return DurableClaim{}, false, err
	}
	body, err := reply[1].ToString()
	return DurableClaim{Ref: ref, Body: []byte(body)}, status == durableClaimWon, err
}

func (s *ValkeyDurableRuns) Wakes(ctx context.Context, wake func()) error {
	subscribe := s.pubsub.B().Subscribe().Channel(durableWakeChannel).Build()
	return s.pubsub.Receive(ctx, subscribe, func(valkey.PubSubMessage) { wake() })
}
