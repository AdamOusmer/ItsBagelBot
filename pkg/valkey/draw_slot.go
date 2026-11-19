// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package valkey

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	valkey_go "github.com/valkey-io/valkey-go"
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
	Phase  string
	Slot   string
	Expect SlotStamp
	Next   string
	Index  ClaimIndex
	Viewer string
	Ref    string
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

var drawSlotShiftScript = NewScript(drawClaimIndexLua + drawStampedSlotLua + `
local round, slots, claimable = unpack(KEYS)
local phase, field, state, generation, next, index, viewer, ref = unpack(ARGV)
if redis.call('HGET', round, 'phase') ~= phase then return 0 end
if not stamped_slot(slots, field, state, generation) then return 0 end

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
redis.call('HSET', slots, field, next)
return 1
`)

func (k RoundKeys) ShiftSlots(ctx context.Context, client valkey_go.Client, shifts []SlotShift) ([]bool, error) {
	calls := make([]ScriptCall, 0, len(shifts))
	for _, s := range shifts {
		calls = append(calls, ScriptCall{
			Script: drawSlotShiftScript,
			Key:    k.Round,
			Also:   []string{k.Slots, k.Claimable},
			Args: []string{
				s.Phase, s.Slot, s.Expect.State, strconv.FormatInt(s.Expect.Generation, 10),
				s.Next, strconv.Itoa(int(s.Index)), s.Viewer, s.Ref,
			},
		})
	}
	applied := make([]bool, 0, len(shifts))
	for _, result := range RunScripts(ctx, client, calls) {
		n, err := result.AsInt64()
		if err != nil {
			return nil, err
		}
		applied = append(applied, n == 1)
	}
	return applied, nil
}

type ClaimRef struct {
	Slots, Ref, Slot string
	Generation       int64
}

type SlotClaim struct {
	Claimable, Viewer string
	Refs              []ClaimRef
	Now               time.Time
}

const (
	SlotClaimed int64 = iota + 1
	SlotClaimLate
	SlotClaimGone
)

var drawClaimSlotsScript = NewScript(drawTimeLua + drawClaimIndexLua + drawStampedSlotLua + `
local claimable = KEYS[1]
local viewer = ARGV[1]
local now = now_ms()
local refs = read_claim_refs(claimable, viewer)
local indexed = {}
for _, ref in ipairs(refs) do indexed[ref] = true end

local function claim(slots, ref, field, generation)
  if not indexed[ref] then return 3 end
  local slot = stamped_slot(slots, field, 'won', generation)
  if not slot then return 3 end
  if (tonumber(slot.deadline) or 0) < now then return 2 end

  slot.state = 'claimed'
  slot.claimed_at = now
  redis.call('HSET', slots, field, cjson.encode(slot))
  indexed[ref] = nil
  return 1
end

local result = {}
for i = 2, #KEYS do
  local arg = 2 + (i - 2) * 3
  result[#result + 1] = claim(KEYS[i], ARGV[arg], ARGV[arg + 1], ARGV[arg + 2])
end
drop_claim_refs(claimable, viewer, refs, function(ref) return not indexed[ref] end)
return result
`)

func ClaimSlots(ctx context.Context, client valkey_go.Client, c SlotClaim) ([]int64, error) {
	keys := make([]string, 1, 1+len(c.Refs))
	args := make([]string, 1, 1+3*len(c.Refs))
	keys[0], args[0] = c.Claimable, c.Viewer
	for _, ref := range c.Refs {
		keys = append(keys, ref.Slots)
		args = append(args, ref.Ref, ref.Slot, strconv.FormatInt(ref.Generation, 10))
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
	err = json.Unmarshal([]byte(raw), &refs)
	return refs, err
}
