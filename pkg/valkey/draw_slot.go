// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"strconv"

	valkey_go "github.com/valkey-io/valkey-go"

	"ItsBagelBot/pkg/codec"
)

type SlotStamp struct {
	State      string
	Generation int64
}

type ClaimIndex uint8

const (
	ClaimKeep ClaimIndex = iota
	ClaimAdd
	ClaimDrop
)

type SlotShift struct {
	Slot   string
	Expect SlotStamp
	Next   string
	Index  ClaimIndex
	Viewer string
	Ref    string
	Prior  string
}

const drawClaimIndexLua = `
local function read_claim_refs(key, viewer)
  return cjson.decode(redis.call('HGET', key, viewer) or '[]')
end

local function drop_claim_refs(key, viewer, refs, drop)
  local keep = {}
  for _, ref in ipairs(refs) do
    if not drop(ref) then keep[#keep + 1] = ref end
  end
  if #keep == #refs then return end
  if #keep == 0 then
    redis.call('HDEL', key, viewer)
  else
    redis.call('HSET', key, viewer, cjson.encode(keep))
  end
end
`

const drawStampedSlotLua = `
local function stamped_slot(key, field, state, generation)
  local raw = redis.call('HGET', key, field)
  if not raw then return nil end
  local slot = cjson.decode(raw)
  if slot.state == state and slot.generation == tonumber(generation) then return slot end
  return nil
end
`

const drawSlotUpdateLua = `
local function matches_slot(slots, args)
  local field, state, generation, prior = unpack(args, 1, 4)
  if prior ~= '' then return redis.call('HGET', slots, field) == prior end
  return stamped_slot(slots, field, state, generation) ~= nil
end

local function update_slot(round, slots, claimable, args)
  local field, _, _, _, next_slot, index, viewer, ref = unpack(args)
  if index ~= '0' then
    local refs = read_claim_refs(claimable, viewer)
    if index == '1' then
      refs[#refs + 1] = ref
      redis.call('HSET', claimable, viewer, cjson.encode(refs))
    else
      drop_claim_refs(claimable, viewer, refs, function(candidate) return candidate == ref end)
    end
    local ttl = redis.call('PTTL', round)
    if ttl >= 0 then
      redis.call('PEXPIRE', claimable, ttl, 'NX')
      redis.call('PEXPIRE', claimable, ttl, 'GT')
    end
  end
  redis.call('HSET', slots, field, next_slot)
end
`

const drawSlotLua = drawClaimIndexLua + drawStampedSlotLua + drawSlotUpdateLua

func (s SlotShift) args() []string {
	return []string{s.Slot, s.Expect.State, strconv.FormatInt(s.Expect.Generation, 10), s.Prior,
		s.Next, strconv.Itoa(int(s.Index)), s.Viewer, s.Ref}
}

type SlotBatch struct {
	RoundID, Phase, Action string
	Revision               *string
	Fields                 []FieldValue
	Wake                   DrawWake
	Shifts                 []SlotShift
}

var drawSlotBatchScript = NewScript(drawSlotLua + drawActionLua + drawWakeLua + `
local round, slots, claimable, due = unpack(KEYS)
local id, phase, action, wake, at, check_revision, revision, fields = unpack(ARGV, 1, 8)
if redis.call('HGET', round, 'phase') ~= phase then return 0 end
if check_revision == 'true' and (redis.call('HGET', round, 'revision') or '') ~= revision then return 0 end
local next_shift = 9 + 2 * tonumber(fields)
local changes = {}
for i = next_shift, #ARGV, 8 do
  local args = {unpack(ARGV, i, i + 7)}
  if not matches_slot(slots, args) then return 0 end
  changes[#changes + 1] = args
end
for _, args in ipairs(changes) do update_slot(round, slots, claimable, args) end
if fields ~= '0' then redis.call('HSET', round, unpack(ARGV, 9, next_shift - 1)) end
add_action(round, action)
wake_draw(round, due, id, wake, at)
return 1
`)

func (k RoundKeys) ShiftSlotBatch(ctx context.Context, client valkey_go.Client, b SlotBatch) (bool, error) {
	keys := []string{k.Round, k.Slots, k.Claimable, k.Due}
	var revision string
	if b.Revision != nil {
		revision = *b.Revision
	}
	args := []string{b.RoundID, b.Phase, b.Action, b.Wake.Channel, strconv.FormatInt(b.Wake.At.UnixMilli(), 10),
		strconv.FormatBool(b.Revision != nil), revision, strconv.Itoa(len(b.Fields))}
	args = appendDrawFields(args, b.Fields)
	for _, shift := range b.Shifts {
		args = append(args, shift.args()...)
	}
	n, err := drawSlotBatchScript.Exec(ctx, client, keys, args).AsInt64()
	return n == 1, err
}

type ClaimRef struct {
	Slots, Ref, Slot string
	Round, RoundID   string
	Generation       int64
}

type SlotClaim struct {
	Claimable, Viewer string
	Refs              []ClaimRef
	Due, Wake         string
}

const (
	SlotClaimed int64 = iota + 1
	SlotClaimLate
	SlotClaimGone
)

var drawClaimSlotsScript = NewScript(drawTimeLua + drawClaimIndexLua + drawStampedSlotLua + drawWakeLua + `
local claimable = KEYS[1]
local viewer = ARGV[1]
local now = now_ms()
local wake = ARGV[2]
local refs = read_claim_refs(claimable, viewer)
local indexed = {}
for _, ref in ipairs(refs) do indexed[ref] = true end

local function claim(slots, round, ref, field, generation)
  if not indexed[ref] then return 3 end
  if redis.call('HGET', round, 'phase') ~= 'settling' then return 3 end
  local slot = stamped_slot(slots, field, 'won', generation)
  if not slot then return 3 end
  if slot.viewer_id and slot.viewer_id ~= viewer then return 3 end
  if (tonumber(slot.deadline) or 0) < now then return 2 end

  slot.state = 'claimed'
  slot.claimed_at = now
  redis.call('HSET', slots, field, cjson.encode(slot))
  indexed[ref] = nil
  return 1
end

local result = {}
for i = 3, #KEYS, 2 do
  local arg = 3 + ((i - 3) / 2) * 4
  local code = claim(KEYS[i], KEYS[i + 1], ARGV[arg], ARGV[arg + 1], ARGV[arg + 2])
  result[#result + 1] = code
  if code == 1 then wake_draw(KEYS[i + 1], KEYS[2], ARGV[arg + 3], wake, now) end
end
drop_claim_refs(claimable, viewer, refs, function(ref) return not indexed[ref] end)
return result
`)

func ClaimSlots(ctx context.Context, client valkey_go.Client, c SlotClaim) ([]int64, error) {
	keys := []string{c.Claimable, drawOptionalKey(c.Due, c.Claimable)}
	args := []string{c.Viewer, c.Wake}
	for _, ref := range c.Refs {
		keys = append(keys, ref.Slots, ref.Round)
		args = append(args, ref.Ref, ref.Slot, strconv.FormatInt(ref.Generation, 10), ref.RoundID)
	}
	return drawClaimSlotsScript.Exec(ctx, client, keys, args).AsIntSlice()
}

func ReadClaimRefs(ctx context.Context, client valkey_go.Client, claimable, viewer string) ([]string, error) {
	cmd := client.B().Hget().Key(claimable).Field(viewer).Build()
	raw, found, err := readOptionalString(ctx, cmd, client)
	if err != nil || !found {
		return nil, err
	}
	var refs []string
	err = codec.Unmarshal([]byte(raw), &refs)
	return refs, err
}
