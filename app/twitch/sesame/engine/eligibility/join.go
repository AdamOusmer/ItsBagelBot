// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package eligibility

import (
	"context"
	"slices"
	"strconv"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine/draw"
	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/invalidate"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"
	"ItsBagelBot/pkg/cache"
)

const snapshotTTL = 30 * time.Second

type Standings interface {
	Snapshot(ctx context.Context, channel, viewer uint64) (loyaltyrpc.BalanceSnapshot, error)
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
	snapshots *cache.Keyed[viewerKey, loyaltyrpc.BalanceSnapshot]
}

type viewerKey struct{ channel, viewer uint64 }

func (k viewerKey) String() string {
	return strconv.FormatUint(k.channel, 10) + ":" + strconv.FormatUint(k.viewer, 10)
}

func NewChecker(p Ports) *Checker {
	if p.Now == nil {
		p.Now = time.Now
	}
	return &Checker{ports: p, snapshots: cache.NewKeyed[viewerKey, loyaltyrpc.BalanceSnapshot](
		cache.DefaultCapacity, snapshotTTL+time.Second, viewerKey.String)}
}

func (c *Checker) WatchFreezes(feed cache.Feed) {
	feed.Watch(invalidate.ScopeLoyalty, func(e cache.Event) {
		if slices.Contains(e.Keys, invalidate.LoyaltyFreezeMarker) {
			c.snapshots.InvalidateWhere(func(k viewerKey) bool { return k.channel == e.ID })
		}
	})
	feed.WatchGap(c.snapshots.Clear)
}

func (c *Checker) Close() { c.snapshots.Close() }

type Joiner struct {
	Channel, Viewer uint64
	Kind            draw.Kind
	Role            module.Role
	Subscriber      bool
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
	snapshot, err := c.snapshot(ctx, viewerKey{j.Channel, j.Viewer})
	if err != nil {
		return Verdict{}, err
	}
	return checkStandings(rules, result, snapshot, c.ports.Now()), nil
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

func (c *Checker) snapshot(ctx context.Context, key viewerKey) (loyaltyrpc.BalanceSnapshot, error) {
	return c.snapshots.GetOrLoadTTL(ctx, key, func(ctx context.Context) (loyaltyrpc.BalanceSnapshot, time.Duration, error) {
		balance, err := c.ports.Standings.Snapshot(ctx, key.channel, key.viewer)
		return balance, snapshotLifetime(balance, c.ports.Now()), err
	})
}

func snapshotLifetime(balance loyaltyrpc.BalanceSnapshot, now time.Time) time.Duration {
	if balance.FreezeID == "" || balance.ExpiresAt <= now.UnixMilli() {
		return cache.SkipStoreTTL
	}
	return min(snapshotTTL, time.UnixMilli(balance.ExpiresAt).Sub(now))
}

func checkStandings(r Rules, pass Verdict, s loyaltyrpc.BalanceSnapshot, asOf time.Time) Verdict {
	if s.FreezeID != "" {
		asOf = time.UnixMilli(s.FrozenAsOf).UTC()
	}
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
