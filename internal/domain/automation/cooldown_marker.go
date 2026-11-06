// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package automation

import (
	"strconv"
	"time"
)

const (
	LongCooldownMarkerMaxTTL = MaxValkeyCooldownSeconds * time.Second
	LongCooldownMoveWindow   = 25 * time.Hour
)

// LongCooldownMarkerKey names the Valkey key for a window over one day; a moved window lives only here.
func LongCooldownMarkerKey(channel uint64, window string) string {
	return "cooldown:long:{" + strconv.FormatUint(channel, 10) + "}:" + window
}

// LongCooldownMarkerValue lets a holder delete only its own marker.
func LongCooldownMarkerValue(expiresAtMS int64) string {
	return strconv.FormatInt(expiresAtMS, 10)
}

// LongCooldownMarkerTTL is how long a marker may live while the row is still the truth; zero or less means expired.
func LongCooldownMarkerTTL(expiresAtMS int64, now time.Time) time.Duration {
	left := time.Duration(expiresAtMS-now.UnixMilli()) * time.Millisecond
	return min(left, LongCooldownMarkerMaxTTL)
}
