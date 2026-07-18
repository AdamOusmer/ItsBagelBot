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
//   - both halves of the global feed counter: the today window
//     (personality:feed:global, TTL) and a live view of the lifetime total
//     (personality:feed:total, no TTL) that is seeded from and persisted to
//     the modules service's DB row through the injected FeedTotalBumper;
//   - a per-stream mood (personality:mood:<id>), first roll wins.
//
// Fact and mood are best-effort (the module falls back to stateless randomness
// on any error); Feed errors instead, which silences the feed line rather than
// reporting numbers that lost their meaning.
type ValkeyPersonality struct {
	client valkey.Client
	total  FeedTotalBumper
	log    *zap.Logger
}

func NewValkeyPersonality(client valkey.Client, total FeedTotalBumper, log *zap.Logger) *ValkeyPersonality {
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

// A warm feed updates both counters in one atomic master round trip. A cold
// total returns nil without touching today's count, allowing the caller to
// synchronously persist this feeding through FeedBump before seeding the live
// view. The seed script keeps the larger of an already-raced live value and
// the DB total, so concurrent cold callers never move the view backwards.
var (
	personalityTTLArg = strconv.FormatInt(int64(personalityTTL.Seconds()), 10)
	feedKeys          = []string{feedTotalKey, feedTodayKey}
	feedWarmArgs      = []string{personalityTTLArg}
	feedWarmScript    = valkey.NewLuaScript(`
if redis.call('EXISTS', KEYS[1]) == 0 then return false end
local total = redis.call('INCR', KEYS[1])
local today = redis.call('INCR', KEYS[2])
if today == 1 then redis.call('EXPIRE', KEYS[2], ARGV[1]) end
return {today, total}`)
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
)

// Feed records one feeding on both counters and returns them: the lifetime
// total from the valkey live view (DB-seeded, persisted behind the reply) and
// the valkey today window. An error on either side errors the whole call; the
// module then stays silent instead of reporting half a readout.
func (s *ValkeyPersonality) Feed(ctx context.Context) (FeedCounts, error) {
	if s.total == nil {
		return FeedCounts{}, errors.New("personality: no feed total backend")
	}
	counts, err := decodeFeedCounts(feedWarmScript.Exec(ctx, s.client, feedKeys, feedWarmArgs))
	if err == nil {
		s.bumpBehind()
		return counts, nil
	}
	if !valkey.IsValkeyNil(err) {
		return FeedCounts{}, err
	}

	dbTotal, err := s.total.FeedBump(ctx)
	if err != nil {
		return FeedCounts{}, err
	}
	return decodeFeedCounts(feedSeedScript.Exec(ctx, s.client,
		feedKeys, []string{strconv.FormatUint(dbTotal, 10), personalityTTLArg}))
}

// bumpBehind persists one feeding to the modules service off the reply path,
// mirroring ValkeyReputation.Bump: a failure only lets the DB lag the view
// until the next cold seed reconciles them.
func (s *ValkeyPersonality) bumpBehind() {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.total.FeedBump(ctx); err != nil {
			s.log.Debug("personality: feed write-behind failed", zap.Error(err))
		}
	}()
}

func decodeFeedCounts(result valkey.ValkeyResult) (FeedCounts, error) {
	values, err := result.ToArray()
	if err != nil {
		return FeedCounts{}, err
	}
	if len(values) != 2 {
		return FeedCounts{}, errors.New("personality: invalid feed script result")
	}
	today, err := values[0].AsInt64()
	if err != nil {
		return FeedCounts{}, err
	}
	total, err := values[1].AsInt64()
	if err != nil {
		return FeedCounts{}, err
	}
	if today < 0 || total < 0 {
		return FeedCounts{}, errors.New("personality: negative feed counter")
	}
	return FeedCounts{Today: uint64(today), Total: uint64(total)}, nil
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
