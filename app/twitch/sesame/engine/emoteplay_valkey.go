// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
)

const (
	pyramidWindow = 15 * time.Second
	streakWindow  = 10 * time.Second
)

var streakLadder = []int{5, 10, 25, 50, 100, 250, 500, 1000}

type EmotePlayUpdate struct {
	BroadcasterID uint64
	MsgID         string
	Emote         string
	Width         int
	Copies        int
}

type EmotePlayResult struct {
	PyramidDone     bool
	Apex            int
	StreakMilestone bool
	Streak          int
}

type ValkeyEmotePlay struct {
	client valkey.Client
	pyrWin time.Duration
	stkWin time.Duration
}

func NewValkeyEmotePlay(client valkey.Client) *ValkeyEmotePlay {
	return &ValkeyEmotePlay{client: client, pyrWin: pyramidWindow, stkWin: streakWindow}
}

var emoteplayScript = valkey.NewLuaScript(`
local t = redis.call('TIME')
local now = t[1] * 1000 + math.floor(t[2] / 1000)
local emote, width, copies = ARGV[1], tonumber(ARGV[2]), tonumber(ARGV[3])
local pwin, swin = tonumber(ARGV[5]), tonumber(ARGV[6])

local st = redis.call('HMGET', KEYS[1],
  'pem', 'pw', 'pa', 'pp', 'pts',
  'sem', 'sn', 'sts',
  'msg')
local pem, pw, pa, pp, pts = st[1], tonumber(st[2]), tonumber(st[3]), tonumber(st[4]), tonumber(st[5])
local sem, sn, sts = st[6], tonumber(st[7]), tonumber(st[8])

-- Replay absorption runs before anything else, on its own unconditional
-- field: neither subsystem's state may own this guard, because a completion
-- clears the pyramid fields and would let a redelivery slip past a
-- subsystem-owned guard and double-count into the streak.
if ARGV[4] ~= '' and st[9] == ARGV[4] then
  redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[7]))
  return {0, 0, 0}
end

local flags, milestone, done_apex = 0, 0, 0

-- Pyramid. A valid attempt always starts at width 1, then rises one line at a
-- time before it may descend. In particular, a descending fragment such as
-- 3,2,1 or a partial 2,3,2,1 is not a pyramid.
if pts == nil or now - pts > pwin then
  if width == 1 then
    pem, pw, pa, pp = emote, 1, 1, 0
  else
    pem, pw, pa, pp = nil, nil, nil, nil
  end
elseif pem ~= emote then
  if width == 1 then
    pem, pw, pa, pp = emote, 1, 1, 0
  else
    pem, pw, pa, pp = nil, nil, nil, nil
  end
elseif width == pw + 1 then
  if pp == 1 then
    pem, pw, pa, pp = nil, nil, nil, nil
  else
    pw, pa = width, width
  end
elseif width == pw - 1 then
  -- Descending, either already under way or turning straight off the apex.
  -- One branch for both so the landing-at-1 check can never be skipped: an
  -- apex-2 attempt turns at its own top and must complete there too.
  if pp == 1 or pw == pa then
    pw = width
    pp = 1
    if pw <= 1 then
      flags = flags + 1
      done_apex = pa
      pem, pw, pa, pp, pts = nil, nil, nil, nil, nil
    end
  else
    if width == 1 then
      pem, pw, pa, pp = emote, 1, 1, 0
    else
      pem, pw, pa, pp = nil, nil, nil, nil
    end
  end
elseif width ~= pw then
  if width == 1 then
    pem, pw, pa, pp = emote, 1, 1, 0
  else
    pem, pw, pa, pp = nil, nil, nil, nil
  end
end
-- width == pw falls through: duplicate step, neutral.

if done_apex > 0 or pem == nil then
  redis.call('HDEL', KEYS[1], 'pem', 'pw', 'pa', 'pp', 'pts')
else
  redis.call('HMSET', KEYS[1], 'pem', pem, 'pw', pw, 'pa', pa, 'pp', pp, 'pts', now)
end

-- Streak.
if width == 1 then
  if sts == nil or now - sts > swin then
    sem, sn = nil, nil
  end
  local prev = sn
  if sem == emote and sn then
    sn = sn + copies
  else
    sem, sn, prev = emote, copies, 0
  end
  for v in string.gmatch(ARGV[8], '%d+') do
    local rung = tonumber(v)
    if prev < rung and rung <= sn and (milestone == 0 or rung < milestone) then
      milestone = rung
    end
  end
  if milestone > 0 then flags = flags + 2 end
  redis.call('HMSET', KEYS[1], 'sem', sem, 'sn', sn, 'sts', now)
else
  redis.call('HDEL', KEYS[1], 'sem', 'sn', 'sts')
end

-- The replay marker is written unconditionally last, so it records exactly the
-- line this call consumed no matter what either subsystem did above.
redis.call('HSET', KEYS[1], 'msg', ARGV[4])
redis.call('PEXPIRE', KEYS[1], tonumber(ARGV[7]))
return {flags, milestone, done_apex}`)

func emoteplayKey(broadcasterID uint64) string {
	return cache.UserKey("emoteplay:v1:", broadcasterID)
}

func (s *ValkeyEmotePlay) Bump(ctx context.Context, u EmotePlayUpdate) (EmotePlayResult, error) {
	ladder := make([]string, len(streakLadder))
	for i, r := range streakLadder {
		ladder[i] = strconv.Itoa(r)
	}
	args := []string{
		u.Emote,
		strconv.Itoa(u.Width),
		strconv.Itoa(u.Copies),
		u.MsgID,
		strconv.FormatInt(s.pyrWin.Milliseconds(), 10),
		strconv.FormatInt(s.stkWin.Milliseconds(), 10),
		strconv.FormatInt((2 * s.pyrWin).Milliseconds(), 10),
		strings.Join(ladder, ","),
	}
	values, err := emoteplayScript.Exec(ctx, s.client,
		[]string{emoteplayKey(u.BroadcasterID)}, args).ToArray()
	if err != nil {
		return EmotePlayResult{}, err
	}
	if len(values) != 3 {
		return EmotePlayResult{}, errors.New("emoteplay: invalid script result")
	}
	flags, err := values[0].AsInt64()
	if err != nil {
		return EmotePlayResult{}, err
	}
	milestone, err := values[1].AsInt64()
	if err != nil {
		return EmotePlayResult{}, err
	}
	apex, err := values[2].AsInt64()
	if err != nil {
		return EmotePlayResult{}, err
	}
	return EmotePlayResult{
		PyramidDone:     flags&1 != 0,
		Apex:            int(apex),
		StreakMilestone: flags&2 != 0,
		Streak:          int(milestone),
	}, nil
}
