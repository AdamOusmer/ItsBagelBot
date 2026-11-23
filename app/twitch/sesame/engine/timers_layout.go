// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"cmp"
	"math"
	"math/rand/v2"
	"slices"
	"time"
)

type intervalGroup struct {
	interval int64
	slot     int64
	phase    int64
	members  []int
}

func liveLayout(timers []armedTimer) []time.Duration {
	groups := groupByInterval(timers)
	for i, g := range groups {
		g.phase = widestPhase(g.slot, groups[:i])
	}
	delays := make([]time.Duration, len(timers))
	for _, g := range groups {
		for k, i := range g.members {
			delays[i] = g.firstFire(k)
		}
	}
	return delays
}

func groupByInterval(timers []armedTimer) []*intervalGroup {
	byInterval := map[int64]*intervalGroup{}
	var groups []*intervalGroup
	for i, at := range timers {
		interval := int64(clampInterval(at.def.Interval) / time.Second)
		g, ok := byInterval[interval]
		if !ok {
			g = &intervalGroup{interval: interval}
			byInterval[interval] = g
			groups = append(groups, g)
		}
		g.members = append(g.members, i)
	}
	for _, g := range groups {
		g.slot = max(g.interval/int64(len(g.members)), 1)
		rand.Shuffle(len(g.members), func(a, b int) { g.members[a], g.members[b] = g.members[b], g.members[a] })
	}
	slices.SortFunc(groups, func(a, b *intervalGroup) int { return cmp.Compare(a.slot, b.slot) })
	return groups
}

func (g *intervalGroup) firstFire(member int) time.Duration {
	at := g.phase + int64(member)*g.slot
	if at == 0 {
		at = g.interval
	}
	return time.Duration(at) * time.Second
}

func widestPhase(slot int64, placed []*intervalGroup) int64 {
	var best []int64
	bestGap := int64(-1)
	for phase := range slot {
		gap := closestFire(phase, slot, placed)
		if gap > bestGap {
			bestGap, best = gap, best[:0]
		}
		if gap == bestGap {
			best = append(best, phase)
		}
	}
	return best[rand.IntN(len(best))]
}

func closestFire(phase, slot int64, placed []*intervalGroup) int64 {
	closest := int64(math.MaxInt64)
	for _, g := range placed {
		closest = min(closest, circularGap(phase-g.phase, gcd(slot, g.slot)))
	}
	return closest
}

func circularGap(diff, period int64) int64 {
	r := ((diff % period) + period) % period
	return min(r, period-r)
}

func gcd(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}
