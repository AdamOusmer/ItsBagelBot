// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
)

type Follows interface {
	FollowedAt(ctx context.Context, channel, viewer uint64) (time.Time, bool, error)
}

var ErrFollowScope = errors.New("eligibility: missing followers scope")

type Accounts interface {
	CreatedAt(ctx context.Context, viewer uint64) (time.Time, bool, error)
}

type Winners interface {
	WonWithin(ctx context.Context, channel uint64, viewers []uint64, since time.Time) ([]uint64, error)
}

type candidate struct {
	viewerKey
	now time.Time
}

type drawRule struct {
	enabled bool
	days    int
	missing Reason
	age     Reason
	lookup  func(context.Context) (time.Time, bool, error)
}

func (c *Checker) Verify(ctx context.Context, chk draw.Check) (draw.Verdict, error) {
	if chk.Round.Terms.Kind != draw.KindGiveaway {
		return draw.Verdict{Pass: true}, nil
	}
	viewer, err := strconv.ParseUint(string(chk.Viewer), 10, 64)
	if err != nil || viewer == 0 {
		return draw.Verdict{}, fmt.Errorf("eligibility: invalid viewer id %q", chk.Viewer)
	}
	rules, err := Parse(chk.Round.Terms.Rules)
	if err != nil {
		return draw.Verdict{}, err
	}
	entry := candidate{viewerKey{uint64(chk.Round.Channel), viewer}, c.ports.Now()}
	return verifyRules(ctx, entry.now, c.drawRules(rules, entry))
}

func verifyRules(ctx context.Context, now time.Time, rules []drawRule) (draw.Verdict, error) {
	for _, rule := range rules {
		if !rule.enabled {
			continue
		}
		result, err := rule.verify(ctx, now)
		if err != nil || !result.Pass {
			return result, err
		}
	}
	return draw.Verdict{Pass: true}, nil
}

func (c *Checker) drawRules(r Rules, entry candidate) []drawRule {
	return []drawRule{
		{enabled: r.FollowersOnly || r.MinFollowDays > 0, days: r.MinFollowDays, missing: ReasonFollow, age: ReasonFollowAge,
			lookup: func(ctx context.Context) (time.Time, bool, error) {
				return c.ports.Follows.FollowedAt(ctx, entry.channel, entry.viewer)
			}},
		{enabled: r.MinAccountDays > 0, days: r.MinAccountDays, missing: ReasonAccountAge, age: ReasonAccountAge,
			lookup: func(ctx context.Context) (time.Time, bool, error) {
				return c.ports.Accounts.CreatedAt(ctx, entry.viewer)
			}},
		{enabled: r.SkipWinnersDays > 0, days: r.SkipWinnersDays, missing: ReasonRecentWinner,
			lookup: func(ctx context.Context) (time.Time, bool, error) {
				since := entry.now.Add(-daySpan(r.SkipWinnersDays))
				winners, err := c.ports.Winners.WonWithin(ctx, entry.channel, []uint64{entry.viewer}, since)
				return time.Time{}, !slices.Contains(winners, entry.viewer), err
			}},
	}
}

func (r drawRule) verify(ctx context.Context, now time.Time) (draw.Verdict, error) {
	at, found, err := r.lookup(ctx)
	switch {
	case errors.Is(err, ErrFollowScope):
		return draw.Verdict{Reason: string(ReasonScope)}, nil
	case err != nil:
		return draw.Verdict{}, err
	case !found:
		return r.failure(r.missing), nil
	case r.age != "" && !meetsAge(now, at, r.days):
		return r.failure(r.age), nil
	default:
		return draw.Verdict{Pass: true}, nil
	}
}

func (r drawRule) failure(reason Reason) draw.Verdict {
	result := draw.Verdict{Reason: string(reason)}
	if reason != ReasonFollow {
		result.Detail = map[string]string{"need": strconv.Itoa(r.days)}
	}
	return result
}

func meetsAge(now, at time.Time, days int) bool {
	return days == 0 || (!at.IsZero() && now.Sub(at) >= daySpan(days))
}

func daySpan(days int) time.Duration { return time.Duration(days) * 24 * time.Hour }
