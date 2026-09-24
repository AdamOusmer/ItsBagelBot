// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/cache"

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
//     reported from the modules service through FeedTotalPersister;
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
	return cache.UserKey("personality:"+section+":", id)
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

// Only today's transient count is cached. Lifetime totals come from the
// synchronous database write on every feeding.
var feedTodayScript = valkey.NewLuaScript(`
local today = redis.call('GET', KEYS[1]) or '0'
local limit = '9223372036854775807'
local function valid(v)
  return string.match(v, '^%d+$') and (string.len(v) < 19 or (string.len(v) == 19 and v <= limit))
end
if not valid(today) or not valid(ARGV[1]) then
  return redis.error_reply('feed counter outside signed integer range')
end
if redis.call('EXISTS', KEYS[2]) == 1 then return {today, ARGV[1]} end
if today == limit then return redis.error_reply('feed counter at signed integer limit') end
redis.call('INCR', KEYS[1])
today = redis.call('GET', KEYS[1])
if today == '1' then redis.call('EXPIRE', KEYS[1], ARGV[2]) end
redis.call('SET', KEYS[2], '1', 'EX', ARGV[3])
return {today, ARGV[1]}`)

// Feed commits the permanent counters before updating the transient today
// window. An uncertain RPC result can be retried with the same event identity.
func (s *ValkeyPersonality) Feed(ctx context.Context, broadcasterID uint64, name, eventID string) (FeedCounts, error) {
	if s.total == nil {
		return FeedCounts{}, errors.New("personality: no feed total backend")
	}
	if eventID == "" {
		return FeedCounts{}, errors.New("personality: feed event identity missing")
	}
	identity := fmt.Sprintf("%d:%s", broadcasterID, eventID)
	digest := sha256.Sum256([]byte(identity))
	receiptID := hex.EncodeToString(digest[:])
	totals, err := s.total.FeedBump(ctx, broadcasterID, name, receiptID)
	if err != nil {
		return FeedCounts{}, err
	}
	if totals.Total < 0 || totals.Total > data.MaxCounter {
		return FeedCounts{}, errors.New("personality: feed total outside signed integer range")
	}
	return decodeFeedCounts(feedTodayScript.Exec(ctx, s.client,
		[]string{feedTodayKey, "personality:feed:receipt:" + receiptID},
		[]string{strconv.FormatInt(totals.Total, 10), strconv.FormatInt(int64(personalityTTL.Seconds()), 10), strconv.FormatInt(int64((2 * personalityTTL).Seconds()), 10)}))
}

// FeedBoard reads the leaderboard from the permanent rows: the commands that
// print it are cooldown-gated and rare, and the DB rows are the only place
// channel names live.
func (s *ValkeyPersonality) FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error) {
	if s.total == nil {
		return FeedBoard{}, errors.New("personality: no feed total backend")
	}
	return s.total.FeedBoard(ctx, broadcasterID, limit)
}

// decodeFeedCounts reads the two fleet-wide counters a feed script returns,
// rejecting a short reply or a negative counter rather than reporting a number
// that lost its meaning.
func decodeFeedCounts(result valkey.ValkeyResult) (FeedCounts, error) {
	values, err := result.ToArray()
	if err != nil {
		return FeedCounts{}, err
	}
	if len(values) != 2 {
		return FeedCounts{}, errors.New("personality: invalid feed script result")
	}
	today, err := decodeFeedCounter(values[0])
	if err != nil {
		return FeedCounts{}, err
	}
	total, err := decodeFeedCounter(values[1])
	if err != nil {
		return FeedCounts{}, err
	}
	return FeedCounts{Today: today, Total: total}, nil
}

func decodeFeedCounter(value valkey.ValkeyMessage) (int64, error) {
	count, err := value.AsInt64()
	if err != nil {
		return 0, err
	}
	if count < 0 {
		return 0, errors.New("personality: feed counter outside signed integer range")
	}
	return count, nil
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
