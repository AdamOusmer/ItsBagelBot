// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"ItsBagelBot/internal/domain/event/data"
	"ItsBagelBot/pkg/cache"
	pkg_valkey "ItsBagelBot/pkg/valkey"

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
	return pkg_valkey.IncrPlain(ctx, s.client, key)
}

// feedTodayKey is the today half of the fleet-wide feed counter: one bagel, fed by
// every channel at once.
const feedTodayKey = "personality:feed:global"

var feedCounter = pkg_valkey.ReceiptCounter{Counter: feedTodayKey, CounterTTL: personalityTTL}

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
	today, total, err := feedCounter.Bump(ctx, s.client, "personality:feed:receipt:"+receiptID, 2*personalityTTL, totals.Total)
	if err != nil {
		return FeedCounts{}, err
	}
	return FeedCounts{Today: today, Total: total}, nil
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

// Mood returns the channel's mood for the current window, seeding it with
// candidate when none is set. First caller's roll wins; everyone else reads it
// back, so the mood stays consistent for the whole stream.
func (s *ValkeyPersonality) Mood(ctx context.Context, broadcasterID uint64, candidate string) (string, error) {
	key := personalityKey("mood", broadcasterID)
	return pkg_valkey.SeedOnMiss(ctx, s.client, pkg_valkey.Key{Name: key, TTL: personalityTTL}, candidate)
}
