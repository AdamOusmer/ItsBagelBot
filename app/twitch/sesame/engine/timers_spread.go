// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"slices"
	"time"

	pkg_valkey "ItsBagelBot/pkg/valkey"
)

func (s *ValkeyTimerStore) spreadDelay(ctx context.Context, at armedTimer, delay time.Duration) time.Duration {
	peers := peerScheduleKeys(at.channel, at.ref)
	if len(peers) == 0 {
		return delay
	}
	remaining, err := pkg_valkey.PTTLs(ctx, s.client, peers)
	if err != nil {
		return delay
	}
	return nextFreeSlot(delay, remaining, channelGap(at.channel))
}

func peerScheduleKeys(timers []timerDef, self timerRef) []string {
	var keys []string
	for _, td := range timers {
		if isPeer(td, self.id) {
			keys = append(keys, timerRef{broadcasterID: self.broadcasterID, id: td.ID}.scheduleKey())
		}
	}
	return keys
}

func isPeer(td timerDef, selfID string) bool {
	return td.Enabled && td.ID != "" && td.ID != selfID
}

func channelGap(timers []timerDef) time.Duration {
	var count int64
	shortest := time.Duration(0)
	for _, td := range timers {
		if !td.Enabled {
			continue
		}
		count++
		shortest = shorterInterval(shortest, clampInterval(td.Interval))
	}
	if count == 0 {
		return 0
	}
	return shortest / time.Duration(count)
}

func shorterInterval(current, candidate time.Duration) time.Duration {
	if current == 0 {
		return candidate
	}
	return min(current, candidate)
}

func nextFreeSlot(delay time.Duration, peers []time.Duration, gap time.Duration) time.Duration {
	slices.Sort(peers)
	for _, peer := range peers {
		if peer > delay-gap && peer < delay+gap {
			delay = peer + gap
		}
	}
	return delay
}
