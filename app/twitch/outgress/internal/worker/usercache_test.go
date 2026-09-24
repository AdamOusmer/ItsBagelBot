// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import "testing"

func TestLaneWorkersShareInjectedUserIDCache(t *testing.T) {
	shared := NewUserIDCache()
	base := Config{UserIDs: shared}

	lanes := map[string]*Worker{
		"premium":  New(base),
		"standard": New(base),
		"system":   New(base),
	}
	for lane, w := range lanes {
		if w.userIDs != shared {
			t.Fatalf("%s worker did not reuse the injected login->id cache", lane)
		}
	}
}

func TestNewFallsBackToPrivateUserIDCache(t *testing.T) {
	w := New(Config{})
	if w.userIDs == nil {
		t.Fatal("worker without an injected cache must still have a usable login->id cache")
	}
}
