// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"strconv"
	"time"

	"ItsBagelBot/pkg/cache"

	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const personalityTTL = 12 * time.Hour

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

func (s *ValkeyPersonality) FactCursor(ctx context.Context, broadcasterID uint64) (int64, error) {
	key := personalityKey("fact", broadcasterID)
	return s.client.Do(ctx, s.client.B().Incr().Key(key).Build()).AsInt64()
}

const feedTodayKey = "personality:feed:global"

const feedTotalKey = "personality:feed:total"

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

func (s *ValkeyPersonality) Feed(ctx context.Context, broadcasterID uint64, name string) (FeedCounts, error) {
	if s.total == nil {
		return FeedCounts{}, errors.New("personality: no feed total backend")
	}
	counts, err := decodeFeedCounts(feedWarmScript.Exec(ctx, s.client, feedKeys, feedWarmArgs))
	if err == nil {
		s.bumpBehind(broadcasterID, name)
		return counts, nil
	}
	if !valkey.IsValkeyNil(err) {
		return FeedCounts{}, err
	}

	totals, err := s.total.FeedBump(ctx, broadcasterID, name)
	if err != nil {
		return FeedCounts{}, err
	}
	return decodeFeedCounts(feedSeedScript.Exec(ctx, s.client,
		feedKeys, []string{strconv.FormatUint(totals.Total, 10), personalityTTLArg}))
}

func (s *ValkeyPersonality) FeedBoard(ctx context.Context, broadcasterID uint64, limit int) (FeedBoard, error) {
	if s.total == nil {
		return FeedBoard{}, errors.New("personality: no feed total backend")
	}
	return s.total.FeedBoard(ctx, broadcasterID, limit)
}

func (s *ValkeyPersonality) bumpBehind(broadcasterID uint64, name string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.total.FeedBump(ctx, broadcasterID, name); err != nil {
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
		return candidate, nil
	} else if !valkey.IsValkeyNil(err) {
		return "", err
	}
	return s.client.Do(ctx, s.client.B().Get().Key(key).Build()).ToString()
}
