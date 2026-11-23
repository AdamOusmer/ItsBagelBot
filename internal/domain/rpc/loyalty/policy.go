// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package loyaltyrpc

import "time"

const (
	FreezeMaximum        = 30 * time.Minute
	FreezeDefaultMinutes = 30
)

func ValidFreezeMinutes(minutes int) bool {
	return minutes >= 1 && minutes <= int(FreezeMaximum/time.Minute)
}
