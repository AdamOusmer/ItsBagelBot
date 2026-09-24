// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/app/twitch/outgress/internal/twitch"
)

func TestMintLeaseTTLExceedsMaxHold(t *testing.T) {
	maxHold := twitch.MaxMintLeaseHold()
	if mintLeaseTTL <= maxHold {
		t.Fatalf("mintLeaseTTL (%s) must exceed the maximum possible mint lease hold (%s); "+
			"otherwise the lease can expire while the winner is still minting, letting a second "+
			"replica acquire it and redeem the same rotating refresh token concurrently",
			mintLeaseTTL, maxHold)
	}
}
