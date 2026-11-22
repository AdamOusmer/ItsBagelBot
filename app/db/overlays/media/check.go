// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package media

import (
	"math"

	"ItsBagelBot/internal/domain/overlays"
)

func inRange(value, limit int) bool {
	return value >= 1 && value <= limit
}

func checkFrame(lim overlays.AssetLimit, w, h int) error {
	if !inRange(w, lim.MaxWidth) || !inRange(h, lim.MaxHeight) {
		return fail(CodeDimensions)
	}
	if lim.MaxPixels > 0 && int64(w)*int64(h) > lim.MaxPixels {
		return fail(CodeDimensions)
	}
	return nil
}

func checkDuration(lim overlays.AssetLimit, ms float64) (int, error) {
	if math.IsNaN(ms) || ms <= 0 {
		return 0, fail(CodeCorrupt)
	}
	if lim.MaxDurationMS > 0 && ms > float64(lim.MaxDurationMS) {
		return 0, fail(CodeDuration)
	}
	return int(math.Ceil(ms)), nil
}
