// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
)

type Follows interface {
	FollowedAt(ctx context.Context, channel, viewer uint64) (time.Time, bool, error)
}

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

type drawRule func(context.Context, Rules, candidate) (draw.Verdict, error)

func (c *Checker) Verify(ctx context.Context, chk draw.Check) (draw.Verdict, error) {
	if chk.Round.Terms.Kind != draw.KindGiveaway {
		return draw.Verdict{Pass: true}, nil
	}
	viewer, err := strconv.ParseUint(string(chk.Viewer), 10, 64)
	if err != nil || viewer == 0 {
		return draw.Verdict{}, fmt.Errorf("eligibility: invalid viewer id %q", chk.Viewer)
	}
	rules := Parse(chk.Round.Terms.Rules)
	entry := candidate{viewerKey{uint64(chk.Round.Channel), viewer}, c.ports.Now()}
	for _, check := range []drawRule{c.checkFollow, c.checkAccount, c.checkRecentWin} {
		result, err := check(ctx, rules, entry)
		if err != nil || !result.Pass {
			return result, err
		}
	}
	return draw.Verdict{Pass: true}, nil
}

func (c *Checker) checkFollow(ctx context.Context, r Rules, entry candidate) (draw.Verdict, error) {
	if !r.FollowersOnly && r.MinFollowDays == 0 {
		return draw.Verdict{Pass: true}, nil
	}
	at, following, err := c.ports.Follows.FollowedAt(ctx, entry.channel, entry.viewer)
	switch {
	case err != nil:
		return draw.Verdict{}, err
	case !following:
		return draw.Verdict{Reason: string(ReasonFollow)}, nil
	default:
		return ageVerdict(entry.now, at, r.MinFollowDays, ReasonFollowAge), nil
	}
}

func (c *Checker) checkAccount(ctx context.Context, r Rules, entry candidate) (draw.Verdict, error) {
	if r.MinAccountDays == 0 {
		return draw.Verdict{Pass: true}, nil
	}
	at, found, err := c.ports.Accounts.CreatedAt(ctx, entry.viewer)
	if err != nil {
		return draw.Verdict{}, err
	}
	if !found {
		at = time.Time{}
	}
	return ageVerdict(entry.now, at, r.MinAccountDays, ReasonAccountAge), nil
}

func (c *Checker) checkRecentWin(ctx context.Context, r Rules, entry candidate) (draw.Verdict, error) {
	if r.SkipWinnersDays == 0 {
		return draw.Verdict{Pass: true}, nil
	}
	since := entry.now.Add(-time.Duration(r.SkipWinnersDays) * 24 * time.Hour)
	winners, err := c.ports.Winners.WonWithin(ctx, entry.channel, []uint64{entry.viewer}, since)
	if err != nil {
		return draw.Verdict{}, err
	}
	if slices.Contains(winners, entry.viewer) {
		return draw.Verdict{Reason: string(ReasonRecentWinner), Detail: dayDetail(r.SkipWinnersDays)}, nil
	}
	return draw.Verdict{Pass: true}, nil
}

func ageVerdict(now, at time.Time, need int, reason Reason) draw.Verdict {
	if need == 0 || meetsAge(now, at, need) {
		return draw.Verdict{Pass: true}
	}
	return draw.Verdict{Reason: string(reason), Detail: dayDetail(need)}
}

func meetsAge(now, at time.Time, days int) bool {
	return !at.IsZero() && now.Sub(at) >= time.Duration(days)*24*time.Hour
}

func dayDetail(days int) map[string]string {
	return map[string]string{"need": strconv.Itoa(days)}
}
