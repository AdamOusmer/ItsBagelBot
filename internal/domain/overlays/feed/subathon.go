// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package feed

import (
	"time"

	"ItsBagelBot/internal/domain/channelevents"
)

// SubathonRules are the seconds each channel event adds to the timer; CapSeconds 0 means the timer has no cap beyond MaxTimerSeconds.
type SubathonRules struct {
	BaseSeconds       int64 `json:"base_seconds"`
	FollowSeconds     int64 `json:"follow_seconds"`
	Tier1Seconds      int64 `json:"tier1_seconds"`
	Tier2Seconds      int64 `json:"tier2_seconds"`
	Tier3Seconds      int64 `json:"tier3_seconds"`
	PrimeSeconds      int64 `json:"prime_seconds"`
	Bits100Seconds    int64 `json:"bits100_seconds"`
	GiftSeconds       int64 `json:"gift_seconds"`
	RaidViewerSeconds int64 `json:"raid_viewer_seconds"`
	CapSeconds        int64 `json:"cap_seconds"`
}

// Subathon is the timer frame. While Running, EndAt is the deadline and RemainingSeconds is stale.
type Subathon struct {
	Running          bool       `json:"running,omitempty"`
	EndAt            *time.Time `json:"end_at,omitempty"`
	RemainingSeconds int64      `json:"remaining_seconds,omitempty"`
}

const MaxTimerSeconds = int64(10 * 365 * 24 * 3600)

func (r SubathonRules) Valid() bool {
	values := []int64{r.BaseSeconds, r.FollowSeconds, r.Tier1Seconds, r.Tier2Seconds, r.Tier3Seconds,
		r.PrimeSeconds, r.Bits100Seconds, r.GiftSeconds, r.RaidViewerSeconds, r.CapSeconds}
	for _, seconds := range values {
		if seconds < 0 || seconds > MaxTimerSeconds {
			return false
		}
	}
	return true
}

// Earned is the time one channel event adds.
func (r SubathonRules) Earned(e channelevents.Event) int64 {
	amount := min(MaxTimerSeconds, e.Amount)
	switch e.Kind {
	case channelevents.Follow:
		return r.FollowSeconds
	case channelevents.Sub, channelevents.Resub:
		return r.subscription(tierOf(e))
	case channelevents.CommunityGift:
		return r.GiftSeconds * amount
	case channelevents.Cheer:
		return r.Bits100Seconds * amount / 100
	case channelevents.Raid:
		return r.RaidViewerSeconds * amount
	}
	return 0
}

func (r SubathonRules) subscription(tier string) int64 {
	switch tier {
	case "2000":
		return r.Tier2Seconds
	case "3000":
		return r.Tier3Seconds
	case "prime":
		return r.PrimeSeconds
	}
	return r.Tier1Seconds
}

func (s Subathon) Remaining(now time.Time) int64 {
	if s.Running && s.EndAt != nil {
		return max(0, int64((s.EndAt.Sub(now)+time.Second-1)/time.Second))
	}
	return s.RemainingSeconds
}

// WithRemaining returns the timer set to seconds, clamped to the rules' cap, and re-armed when running.
func (s Subathon) WithRemaining(rules SubathonRules, seconds int64, now time.Time) Subathon {
	limit := rules.CapSeconds
	if limit == 0 {
		limit = MaxTimerSeconds
	}
	s.RemainingSeconds = min(limit, max(0, seconds))
	s.EndAt = nil
	if s.Running {
		end := now.Add(time.Duration(s.RemainingSeconds) * time.Second)
		s.EndAt = &end
	}
	return s
}

// Add moves the clock by seconds, which may be negative.
func (s Subathon) Add(rules SubathonRules, seconds int64, now time.Time) Subathon {
	return s.WithRemaining(rules, s.Remaining(now)+max(-MaxTimerSeconds, min(seconds, MaxTimerSeconds)), now)
}

func (s Subathon) SetRunning(rules SubathonRules, running bool, now time.Time) Subathon {
	remaining := s.Remaining(now)
	s.Running = running
	return s.WithRemaining(rules, remaining, now)
}

func (s Subathon) Reset(rules SubathonRules, now time.Time) Subathon {
	s.Running = false
	return s.WithRemaining(rules, rules.BaseSeconds, now)
}
