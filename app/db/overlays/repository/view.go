// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"slices"

	"ItsBagelBot/app/db/overlays/ent"
	overlaysrpc "ItsBagelBot/internal/domain/rpc/overlays"
)

func overlayOf(row *ent.PoweredOverlay) overlaysrpc.PoweredOverlay {
	return overlaysrpc.PoweredOverlay{
		UserID: row.UserID,
		Token:  row.Token,
		Config: overlaysrpc.PoweredConfig{
			EveryMinutes: row.EveryMinutes,
			ShowSeconds:  row.ShowSeconds,
			Directions:   directionsOf(row.Directions),
			Align:        overlaysrpc.Align(row.Align),
		},
		Revision: row.Revision,
	}
}

func directionBits(list []overlaysrpc.Direction) uint8 {
	var bits uint8
	for i, d := range overlaysrpc.Directions {
		if slices.Contains(list, d) {
			bits |= 1 << i
		}
	}
	return bits
}

func directionsOf(bits uint8) []overlaysrpc.Direction {
	list := make([]overlaysrpc.Direction, 0, len(overlaysrpc.Directions))
	for i, d := range overlaysrpc.Directions {
		if bits&(1<<i) != 0 {
			list = append(list, d)
		}
	}
	return list
}
