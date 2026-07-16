package engine

import (
	"context"
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
//   - one global feed counter (personality:feed:global) shared by every
//     channel: there is a single bagel, and everyone is feeding it;
//   - a per-stream mood (personality:mood:<id>), first roll wins.
//
// Everything is best-effort: the module falls back to stateless randomness on
// any error, so a valkey blip only dims the bit, never the message path.
type ValkeyPersonality struct {
	client valkey.Client
	log    *zap.Logger
}

func NewValkeyPersonality(client valkey.Client, log *zap.Logger) *ValkeyPersonality {
	return &ValkeyPersonality{client: client, log: log}
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

// feedKey is the single fleet-wide feed counter: one bagel, fed by every
// channel at once.
const feedKey = "personality:feed:global"

// FeedCount bumps and returns the global feed counter. The first feed of the
// window claims the TTL; later feeds leave it in place so the count reads as
// "today", not a sliding forever-window.
func (s *ValkeyPersonality) FeedCount(ctx context.Context) (int64, error) {
	n, err := s.client.Do(ctx, s.client.B().Incr().Key(feedKey).Build()).AsInt64()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		seconds := int64(personalityTTL.Seconds())
		if err := s.client.Do(ctx, s.client.B().Expire().Key(feedKey).Seconds(seconds).Build()).Error(); err != nil {
			s.log.Warn("personality: feed expire failed", zap.Error(err))
		}
	}
	return n, nil
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
