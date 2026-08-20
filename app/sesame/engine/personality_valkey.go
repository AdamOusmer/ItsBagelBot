// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// personalityTTL scopes the per-stream personality state (feed counter, mood).
// Streams rarely run longer, and a stale value only means a joke resets, so a
// coarse window beats tracking real stream boundaries.
const personalityTTL = 12 * time.Hour

// ValkeyPersonality is the tiny state behind the personality module:
//
//   - a monotonic per-channel fact cursor (personality:fact:<id>, no TTL) so
//     the fun-fact list plays in order instead of repeating at random;
//   - both halves of the fleet-wide feed counter: the today window
//     (personality:feed:global, TTL) and a live view of the lifetime total
//     (personality:feed:total, no TTL) that is seeded from and persisted to
//     the modules service's DB row through the injected FeedTotalPersister;
//   - the feed leaderboard as a sorted set (personality:feed:board, scored by
//     lifetime feedings per channel) so a feeding can report the channel's own
//     count and rank from the same round trip;
//   - a per-stream mood (personality:mood:<id>), first roll wins.
//
// Fact and mood are best-effort (the module falls back to stateless randomness
// on any error); Feed errors instead, which silences the feed line rather than
// reporting numbers that lost their meaning.
type ValkeyPersonality struct {
	client valkey.Client
	total  FeedTotalPersister
	log    *zap.Logger
}

func NewValkeyPersonality(client valkey.Client, total FeedTotalPersister, log *zap.Logger) *ValkeyPersonality {
	return &ValkeyPersonality{client: client, total: total, log: log}
}

func personalityKey(section string, id uint64) string {
	return "personality:" + section + ":" + strconv.FormatUint(id, 10)
}

// FactCursor bumps and returns the channel's fact cursor. The module takes it
// modulo the fact-list length, so the counter itself never needs resetting.
func (s *ValkeyPersonality) FactCursor(ctx context.Context, broadcasterID uint64) (int64, error) {
	key := personalityKey("fact", broadcasterID)
	return s.client.Do(ctx, s.client.B().Incr().Key(key).Build()).AsInt64()
}

// feedTodayKey is the today half of the fleet-wide feed counter: one bagel, fed by
// every channel at once.
const feedTodayKey = "personality:feed:global"

// feedTotalKey is the valkey live view of the permanent feed total. The DB row
// in the modules service stays the source of truth; this key exists so the
// reply path never waits on an RPC once the view is warm.
const feedTotalKey = "personality:feed:total"

// feedBoardKey is the live view of the per-channel leaderboard: a sorted set
// scored by lifetime feedings, member = broadcaster id. It answers "how often
// has this channel fed, and where does that place it" in the same call that
// records the feeding.
const feedBoardKey = "personality:feed:board"

// feedBoardTTL makes the board reconcile with the DB on its own: when it
// lapses the next feeding takes the cold path, which reseeds every channel's
// score from the permanent rows. Without it a score lost to a failed
// write-behind would stay wrong until the key was deleted by hand.
const feedBoardTTL = time.Hour

// A warm feed updates every counter in one atomic master round trip. A cold
// total or a lapsed board returns nil without touching today's count, allowing
// the caller to synchronously persist this feeding through FeedBump before
// seeding the live views. The seed script keeps the larger of an already-raced
// live value and the DB total, so concurrent cold callers never move the view
// backwards; the board seed does the same per channel with ZADD GT.
//
// Rank ties break by broadcaster id here (sorted-set order) and by "everyone
// tied shares the better rank" in the DB. A tie only ever shifts a joke line by
// one place, which is not worth a second round trip to reconcile.
var (
	personalityTTLArg = strconv.FormatInt(int64(personalityTTL.Seconds()), 10)
	feedBoardTTLArg   = strconv.FormatInt(int64(feedBoardTTL.Seconds()), 10)
	feedKeys          = []string{feedTotalKey, feedTodayKey, feedBoardKey}
	feedCounterKeys   = []string{feedTotalKey, feedTodayKey}
	feedBoardKeys     = []string{feedBoardKey}
	feedWarmScript    = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return false end
if redis.call('EXISTS', KEYS[3]) == 0 then return false end
local total = redis.call('INCR', KEYS[1])
local today = redis.call('INCR', KEYS[2])
if today == 1 then redis.call('EXPIRE', KEYS[2], ARGV[1]) end
local channel = tonumber(redis.call('ZINCRBY', KEYS[3], 1, ARGV[2]))
local rank = redis.call('ZREVRANK', KEYS[3], ARGV[2]) + 1
return {today, total, channel, rank, redis.call('ZCARD', KEYS[3])}`)
	feedSeedScript = valkey.NewLuaScript(`
local seed = tonumber(ARGV[1])
local current = redis.call('GET', KEYS[1])
if not current then
  current = seed
  redis.call('SET', KEYS[1], seed)
else
  current = tonumber(current)
  if current < seed then
    current = seed
    redis.call('SET', KEYS[1], seed)
  end
end
local today = redis.call('INCR', KEYS[2])
if today == 1 then redis.call('EXPIRE', KEYS[2], ARGV[2]) end
return {today, current}`)
	feedBoardSeedScript = valkey.NewLuaScript(`
for i = 2, #ARGV, 2 do
  redis.call('ZADD', KEYS[1], 'GT', ARGV[i], ARGV[i + 1])
end
redis.call('EXPIRE', KEYS[1], ARGV[1])
return redis.call('ZCARD', KEYS[1])`)
)

// Feed records one feeding on every counter and returns them: the fleet-wide
// lifetime total from the valkey live view (DB-seeded, persisted behind the
// reply), the valkey today window, and the feeding channel's own count and
// rank. An error on either side errors the whole call; the module then stays
// silent instead of reporting half a readout.
func (s *ValkeyPersonality) Feed(ctx context.Context, broadcasterID uint64, name string) (FeedCounts, error) {
	if s.total == nil {
		return FeedCounts{}, errors.New("personality: no feed total backend")
	}
	member := strconv.FormatUint(broadcasterID, 10)
	counts, err := decodeFeedWarm(feedWarmScript.Exec(ctx, s.client, feedKeys, []string{personalityTTLArg, member}))
	if err == nil {
		s.bumpBehind(broadcasterID, name)
		return counts, nil
	}
	if !valkey.IsValkeyNil(err) {
		return FeedCounts{}, err
	}
	return s.coldFeed(ctx, broadcasterID, name)
}

// coldFeed persists the feeding through the modules service first, then seeds
// both live views from the reply. The counts it returns come from the DB for
// the channel half (authoritative the moment it was written) and from the seed
// script for the fleet halves.
func (s *ValkeyPersonality) coldFeed(ctx context.Context, broadcasterID uint64, name string) (FeedCounts, error) {
	totals, err := s.total.FeedBump(ctx, broadcasterID, name, true)
	if err != nil {
		return FeedCounts{}, err
	}
	counts, err := decodeFeedCounts(feedSeedScript.Exec(ctx, s.client,
		feedCounterKeys, []string{strconv.FormatUint(totals.Total, 10), personalityTTLArg}))
	if err != nil {
		return FeedCounts{}, err
	}
	counts.Channel, counts.Rank = totals.Channel, totals.Rank
	counts.Ranked = s.seedBoard(ctx, totals.Board)
	return counts, nil
}

// seedBoard rebuilds the sorted-set view from the permanent rows and returns
// how many channels it holds. A seeding failure is not fatal: the numbers in
// this reply already came from the DB, and the next feeding simply takes the
// cold path again.
func (s *ValkeyPersonality) seedBoard(ctx context.Context, board []FeedBoardEntry) uint64 {
	if len(board) == 0 {
		return 0
	}
	args := make([]string, 0, 1+2*len(board))
	args = append(args, feedBoardTTLArg)
	for _, entry := range board {
		args = append(args,
			strconv.FormatUint(entry.Count, 10),
			strconv.FormatUint(entry.BroadcasterID, 10))
	}
	ranked, err := feedBoardSeedScript.Exec(ctx, s.client, feedBoardKeys, args).AsInt64()
	if err != nil || ranked < 0 {
		s.log.Debug("personality: feed board seed failed", zap.Error(err))
		return uint64(len(board))
	}
	return uint64(ranked)
}

// FeedBoard reads the leaderboard from the permanent rows rather than the live
// view: the reaction that prints it is cooldown-gated and rare, and the DB
// rows are the only place channel names live.
func (s *ValkeyPersonality) FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error) {
	if s.total == nil {
		return FeedBoard{}, errors.New("personality: no feed total backend")
	}
	return s.total.FeedBoard(ctx, broadcasterID, limit)
}

// bumpBehind persists one feeding to the modules service off the reply path,
// mirroring ValkeyReputation.Bump: a failure only lets the DB lag the view
// until the next cold seed reconciles them.
func (s *ValkeyPersonality) bumpBehind(broadcasterID uint64, name string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.total.FeedBump(ctx, broadcasterID, name, false); err != nil {
			s.log.Debug("personality: feed write-behind failed", zap.Error(err))
		}
	}()
}

// decodeFeedCounts reads the two fleet-wide counters the seed script returns.
func decodeFeedCounts(result valkey.ValkeyResult) (FeedCounts, error) {
	values, err := feedValues(result, 2)
	if err != nil {
		return FeedCounts{}, err
	}
	return FeedCounts{Today: values[0], Total: values[1]}, nil
}

// decodeFeedWarm reads the full readout the warm script returns: both
// fleet-wide counters, then the channel's count, rank and the board size.
func decodeFeedWarm(result valkey.ValkeyResult) (FeedCounts, error) {
	values, err := feedValues(result, 5)
	if err != nil {
		return FeedCounts{}, err
	}
	return FeedCounts{
		Today:   values[0],
		Total:   values[1],
		Channel: values[2],
		Rank:    values[3],
		Ranked:  values[4],
	}, nil
}

// feedValues unpacks a script's integer array, rejecting a short reply or a
// negative counter rather than reporting a number that lost its meaning.
func feedValues(result valkey.ValkeyResult, want int) ([]uint64, error) {
	values, err := result.ToArray()
	if err != nil {
		return nil, err
	}
	if len(values) != want {
		return nil, errors.New("personality: invalid feed script result")
	}
	counts := make([]uint64, 0, want)
	for _, value := range values {
		number, err := value.AsInt64()
		if err != nil {
			return nil, err
		}
		if number < 0 {
			return nil, errors.New("personality: negative feed counter")
		}
		counts = append(counts, uint64(number))
	}
	return counts, nil
}

// Mood returns the channel's mood for the current window, seeding it with
// candidate when none is set. First caller's roll wins; everyone else reads it
// back, so the mood stays consistent for the whole stream.
func (s *ValkeyPersonality) Mood(ctx context.Context, broadcasterID uint64, candidate string) (string, error) {
	key := personalityKey("mood", broadcasterID)
	got, err := s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString()
	if err == nil {
		return got, nil
	}
	if !valkey.IsValkeyNil(err) {
		return "", err
	}
	seconds := int64(personalityTTL.Seconds())
	set := s.client.Do(ctx, s.client.B().Set().Key(key).Value(candidate).Nx().ExSeconds(seconds).Build())
	if _, err := set.ToString(); err == nil {
		return candidate, nil // our roll won the window
	} else if !valkey.IsValkeyNil(err) {
		return "", err
	}
	// Lost the SET NX race: another pod seeded the mood between our GET and SET.
	return s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString()
}
