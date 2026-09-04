// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kv

import "testing"

func TestNewWithNilClientReturnsNilStore(t *testing.T) {
	// A nil Valkey client (unreachable at boot) must return a nil LiveStore
	// rather than a store wrapping a nil client that panics on first use --
	// callers already nil-check before every use, matching the pre-split
	// newValkeyDiscordLive.
	if s := New(nil); s != nil {
		t.Fatalf("New(nil) = %v, want nil", s)
	}
}
