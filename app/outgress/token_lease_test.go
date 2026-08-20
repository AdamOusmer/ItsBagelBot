// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"testing"

	"ItsBagelBot/app/outgress/internal/twitch"
)

// TestMintLeaseTTLExceedsMaxHold pins the core safety invariant this file's
// mintLeaseTTL doc describes: the lease TTL must exceed the maximum time a
// mint can legally hold it, computed from the very constants involved
// (twitch.MaxMintLeaseHold = tokenClientTimeout + persistTimeout), not from
// a copy of their values re-typed here. A future edit to mintLeaseTTL,
// tokenClientTimeout, or persistTimeout that reopens the gap this file's
// history describes (the lease expiring while the winner still holds it,
// letting a second replica redeem the same rotating refresh token) fails
// this test instead of silently shipping.
func TestMintLeaseTTLExceedsMaxHold(t *testing.T) {
	maxHold := twitch.MaxMintLeaseHold()
	if mintLeaseTTL <= maxHold {
		t.Fatalf("mintLeaseTTL (%s) must exceed the maximum possible mint lease hold (%s); "+
			"otherwise the lease can expire while the winner is still minting, letting a second "+
			"replica acquire it and redeem the same rotating refresh token concurrently",
			mintLeaseTTL, maxHold)
	}
}
