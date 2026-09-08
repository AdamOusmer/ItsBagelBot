// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"
)

// TestWithinCopiesTheWiring guards the property every caller relies on: the
// override is a copy, so a service can hold one wiring value and hand a
// different budget to a single verb without the next verb inheriting it.
func TestWithinCopiesTheWiring(t *testing.T) {
	base := RPCWiring{Queue: "svc-rpc"}
	slow := base.Within(30 * time.Second)

	got := map[string]time.Duration{"base": base.timeout(), "slow": slow.timeout()}
	want := map[string]time.Duration{"base": DefaultRPCTimeout, "slow": 30 * time.Second}
	for name, w := range want {
		if got[name] != w {
			t.Fatalf("timeouts = %v, want %v", got, want)
		}
	}
	if slow.Queue != base.Queue {
		t.Fatalf("Within dropped Queue: %q, want %q", slow.Queue, base.Queue)
	}
}
