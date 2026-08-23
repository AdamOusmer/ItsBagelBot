// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"
)

// Tuned windows. Both are gap tolerances, not stream boundaries: a chain dies
// when the next candidate line takes longer than the window to arrive, and
// ordinary prose never touches this store at all (the module gates on shape),
// so nothing here can cost the chat hot path anything. Values are script
// arguments, so a test can shrink them instead of sleeping out real windows.
const (
	// pyramidWindow is the max gap between two lines of one pyramid. Chat
	// pyramids move at typing speed (~1-3s per line); 15s absorbs slow typists
	// and a folded-cohort detour without keeping a dead attempt alive long
	// enough for someone to "complete" a pyramid nobody was building.
	pyramidWindow = 15 * time.Second
	// streakWindow is the max gap between two single-emote messages of one
	// streak. Streaks read as hype only while they are dense; 10s keeps the
	// counter from creeping up across scattered messages.
	streakWindow = 10 * time.Second
)

// streakLadder is the counts at which a streak is announced. A milestone fires
// when the running count crosses a rung (a folded cohort may jump several), and
// after the last rung the streak stays silent — past x1000 every further line
// would be spam with no information. Crossing (not equality) so a 5-duplicate
// cohort cannot step over a rung without celebrating it.
var streakLadder = []int{5, 10, 25, 50, 100, 250, 500, 1000}

// EmotePlayUpdate is one candidate line fed to the store: emote is the exact
// repeated token (case matters — Kappa and kappa are different emotes), width
// its repetition count in the line (>= 1), copies how many identical lines the
// ingress squash folded into this envelope (>= 1 when none).
type EmotePlayUpdate struct {
	BroadcasterID uint64
	MsgID         string
	Emote         string
	Width         int
	Copies        int
}

// EmotePlayResult is what one accepted line did to the channel's chains.
type EmotePlayResult struct {
	// PyramidDone is true when this line landed the descent back at width 1;
	// Apex is the height it reached.
	PyramidDone bool
	Apex        int
	// StreakMilestone is true when the streak crossed a ladder rung; Streak is
	// that rung's count (the announced number, not necessarily the raw count —
	// a folded cohort can overshoot).
	StreakMilestone bool
	Streak          int
}

// EmotePlayStore itself is declared in deps.go alongside the other store
// interfaces; ValkeyEmotePlay below is its implementation.

// ValkeyEmotePlay is the race-safety story, which is the whole reason it lives
// in valkey rather than pod-local memory: sesame runs 3 replicas sharing one
// durable lane consumer, so consecutive lines of the same channel are routinely
// handled by different pods, and JetStream redeliveries re-run handlers after a
// nack. Every transition is therefore ONE Lua script call — read state, decide,
// write state, all inside the interpreter's single-threaded atomicity — so the
// second pod to touch a channel linearizes on top of the first pod's write.
// There is deliberately no WATCH/MULTI retry loop and no GET-then-SET: the
// script is one master round trip, which keeps the per-candidate-line cost at
// exactly one RTT and closes the inter-pod races by construction.
//
// The script is not NewLuaScriptRetryable on purpose (same reasoning as
// pkg/ratelimit): a connection failure mid-script could have applied the bump,
// and replaying it would double-count a line. The module treats an error as
// "line lost" and fails open silently.
//
// Replays of the SAME message id are absorbed in-script (the msg field): a
// redelivered envelope re-runs this handler because the pipeline's EventDedup
// deliberately does not claim plain-chat lines, and without the msg guard a
// redelivery would double-count a streak line. One guard covers both
// subsystems since they consume the same line.
type ValkeyEmotePlay struct {
	client valkey.Client
	// The windows ride every Bump as script arguments rather than living only
	// in the script source, so tests can shrink them to milliseconds instead
	// of sleeping out production values.
	pyrWin time.Duration
	stkWin time.Duration
}

func NewValkeyEmotePlay(client valkey.Client) *ValkeyEmotePlay {
	return &ValkeyEmotePlay{client: client, pyrWin: pyramidWindow, stkWin: streakWindow}
}

// emoteplayScript advances pyramid + streak state for one candidate line.
//
// KEYS[1] the channel's state hash (emoteplay:v1:<broadcaster>).
// ARGV[1] emote, [2] width, [3] copies, [4] msgid,
// [5] pyramid window ms, [6] streak window ms, [7] key ttl ms, [8] ladder csv.
//
// Returns {flags, milestone, apex}: flags bit0 (value 1) pyramid completed,
// bit1 (value 2) streak milestone; milestone the rung crossed; apex the
// completed pyramid's height. Integers only — raffle_claim.go documents why a
// RESP2 bulk starting '-' would parse as an error.
//
// Pyramid rules. State: emote, width, apex, phase (0 ascending / 1 descending).
// From any line: same width repeats are neutral no-ops (two chatters racing the
// same step, or two pods delivering near-simultaneously, must not double-step);
// width+1 ascends while phase=asc; width-1 descends, but only straight off the
// apex (phase flips there); landing the descent at 1 completes and clears.
// Anything else — different emote, a width jump, re-ascending mid-descent —
// restarts the attempt anchored AT the offending line rather than clearing:
// a troll wall should not erase the fun, it just becomes the new base. Window
// expiry behaves like a clear.
//
// Streak rules. Only single-token lines (width==1) count; a wider pure-emote
// line (someone building something else) breaks the current streak silently.
// Same-emote lines add their copies (folded duplicates each count — they were
// distinct chatters); a different emote restarts from that line's copies.
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

-- Pyramid.
if pts == nil or now - pts > pwin then
  pem, pw, pa, pp = emote, width, width, 0
elseif pem ~= emote then
  pem, pw, pa, pp = emote, width, width, 0
elseif width == pw + 1 then
  if pp == 1 then
    pem, pw, pa, pp = emote, width, width, 0
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
    pem, pw, pa, pp = emote, width, width, 0
  end
elseif width ~= pw then
  pem, pw, pa, pp = emote, width, width, 0
end
-- width == pw falls through: duplicate step, neutral.

if done_apex > 0 then
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
	return "emoteplay:v1:" + strconv.FormatUint(broadcasterID, 10)
}

// Bump advances both chains for one line in a single master round trip. Pure
// write script, so the default write-to-master routing is correct without a
// Primary pin (nothing ever reads back through a replica).
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
		// Twice the longest subsystem window, so an abandoned chain always
		// expires server-side even if no further candidate line ever arrives.
		// Nothing reads the key except this script, so the exact value only
		// bounds memory.
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
