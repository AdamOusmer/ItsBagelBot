// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package draw

import (
	"crypto/rand"
	"math/big"

	"ItsBagelBot/pkg/valkey"
)

func secureRandom(bound int64) int64 {
	n, err := rand.Int(rand.Reader, big.NewInt(bound))
	if err != nil {
		panic(err)
	}
	return n.Int64()
}

func pick(entries []valkey.ScoredMember, drawn map[ViewerID]bool, random func(int64) int64) (ViewerID, bool) {
	total := availableWeight(entries, drawn)
	if total == 0 {
		return "", false
	}
	remaining := random(total)
	for _, entry := range entries {
		id := ViewerID(entry.Member)
		if drawn[id] {
			continue
		}
		remaining -= entry.Score
		if remaining < 0 {
			return id, true
		}
	}
	return "", false
}

func availableWeight(entries []valkey.ScoredMember, drawn map[ViewerID]bool) int64 {
	var total int64
	for _, entry := range entries {
		if !drawn[ViewerID(entry.Member)] {
			total += entry.Score
		}
	}
	return total
}

func drawnViewers(slots []Slot) map[ViewerID]bool {
	drawn := make(map[ViewerID]bool)
	for _, slot := range slots {
		for _, candidate := range slot.History {
			drawn[candidate.Viewer] = true
		}
	}
	return drawn
}
