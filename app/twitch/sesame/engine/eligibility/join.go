// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

import (
	"context"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"
)

const snapshotTTL = 30 * time.Second

type Standings interface {
	Snapshot(context.Context, StandingLookup) (loyaltyrpc.BalanceSnapshot, error)
}

type StandingLookup struct {
	Channel, Viewer uint64
	SnapshotID      string
}

type Cutoff struct {
	ID       string
	OpenedAt time.Time
}

type Ports struct {
	Standings Standings
	Follows   Follows
	Accounts  Accounts
	Winners   Winners
	Now       func() time.Time
}

type Checker struct {
	ports     Ports
	snapshots *cache.Keyed[StandingLookup, loyaltyrpc.BalanceSnapshot]
}

type viewerKey struct{ channel, viewer uint64 }

func (k StandingLookup) String() string {
	return strconv.FormatUint(k.Channel, 10) + ":" + strconv.FormatUint(k.Viewer, 10) + ":" + k.SnapshotID
}

func NewChecker(p Ports) *Checker {
	if p.Now == nil {
		p.Now = time.Now
	}
	return &Checker{ports: p, snapshots: cache.NewKeyed[StandingLookup, loyaltyrpc.BalanceSnapshot](
		cache.DefaultCapacity, snapshotTTL+time.Second, StandingLookup.String)}
}

func (c *Checker) Close() { c.snapshots.Close() }

type Joiner struct {
	Channel, Viewer uint64
	Kind            draw.Kind
	Role            module.Role
	Subscriber      bool
	Cutoff          Cutoff
}

type Verdict struct {
	Pass      bool
	Reason    Reason
	Need, Had int64
	AsOf      time.Time
	Weight    int64
}

func (c *Checker) Join(ctx context.Context, rules Rules, j Joiner) (Verdict, error) {
	if j.Kind == draw.KindRaffle {
		return Verdict{Pass: true, Weight: draw.Pins.EntryWeight}, nil
	}
	if reason := roleRefusal(rules, j); reason != "" {
		return Verdict{Reason: reason}, nil
	}
	result := Verdict{Pass: true, Weight: joinWeight(rules, j.Subscriber)}
	if !rules.NeedsStandings() {
		return result, nil
	}
	snapshot, err := c.snapshot(ctx, j)
	if err != nil {
		return Verdict{}, err
	}
	return checkStandings(rules, result, snapshot, joinAsOf(j.Cutoff, c.ports.Now())), nil
}

func roleRefusal(r Rules, j Joiner) Reason {
	switch {
	case j.Viewer == j.Channel || j.Role == module.RoleBroadcaster:
		return ReasonBroadcaster
	case r.ExcludeMods && j.Role.Allows(module.RoleModerator):
		return ReasonMod
	case !j.Role.Allows(module.ParsePerm(string(r.Role))):
		return ReasonRole
	default:
		return ""
	}
}

func joinWeight(r Rules, subscriber bool) int64 {
	if r.SubLuckPercent > 0 && subscriber {
		return int64(r.SubLuckPercent)
	}
	return draw.Pins.EntryWeight
}

func (c *Checker) snapshot(ctx context.Context, j Joiner) (loyaltyrpc.BalanceSnapshot, error) {
	req := StandingLookup{Channel: j.Channel, Viewer: j.Viewer, SnapshotID: j.Cutoff.ID}
	expiresAt := j.Cutoff.OpenedAt.Add(draw.Pins.GiveawayMaxEntryWindow)
	if req.SnapshotID == "" || !c.ports.Now().Before(expiresAt) {
		return c.ports.Standings.Snapshot(ctx, req)
	}
	return c.snapshots.GetOrLoadTTL(ctx, req, func(ctx context.Context) (loyaltyrpc.BalanceSnapshot, time.Duration, error) {
		balance, err := c.ports.Standings.Snapshot(ctx, req)
		left := expiresAt.Sub(c.ports.Now())
		if left <= 0 {
			return balance, cache.SkipStoreTTL, err
		}
		return balance, min(snapshotTTL, left), err
	})
}

func joinAsOf(cutoff Cutoff, now time.Time) time.Time {
	if cutoff.ID != "" {
		return cutoff.OpenedAt
	}
	return now
}

func checkStandings(r Rules, pass Verdict, s loyaltyrpc.BalanceSnapshot, asOf time.Time) Verdict {
	switch {
	case r.MinPoints > 0 && s.Points < r.MinPoints:
		return Verdict{Reason: ReasonPoints, Need: r.MinPoints, Had: s.Points, AsOf: asOf}
	case s.WatchSeconds < r.MinWatchSeconds:
		return Verdict{Reason: ReasonWatch, Need: int64(r.MinWatchSeconds), Had: int64(s.WatchSeconds), AsOf: asOf}
	default:
		pass.AsOf = asOf
		return pass
	}
}
