// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"math"

	"ItsBagelBot/internal/domain/overlays"
)

type extent int

type dims struct {
	w extent
	h extent
}

type millis float64

func (e extent) within(limit extent) bool {
	return e >= 1 && e <= limit
}

func (d dims) check(lim overlays.AssetLimit) error {
	if !d.w.within(extent(lim.MaxWidth)) || !d.h.within(extent(lim.MaxHeight)) {
		return fail(CodeDimensions)
	}
	if lim.MaxPixels > 0 && int64(d.w)*int64(d.h) > lim.MaxPixels {
		return fail(CodeDimensions)
	}
	return nil
}

func (m millis) check(lim overlays.AssetLimit) (int, error) {
	if math.IsNaN(float64(m)) || m <= 0 {
		return 0, fail(CodeCorrupt)
	}
	if lim.MaxDurationMS > 0 && float64(m) > float64(lim.MaxDurationMS) {
		return 0, fail(CodeDuration)
	}
	return int(math.Ceil(float64(m))), nil
}
