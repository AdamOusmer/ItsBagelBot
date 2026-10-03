// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package botstatus

import (
	"context"
	"testing"
	"time"
)

func TestConnectLogLoadReadsTheScoresBack(t *testing.T) {
	log := NewConnectLog(newZsetFake(t), "pod-1")
	ctx := context.Background()
	base := time.UnixMilli(1_700_000_000_000)
	for _, at := range []time.Time{base.Add(time.Second), base} {
		if err := log.Add(ctx, at); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	got, err := log.Load(ctx, base.Add(-time.Minute))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	want := []time.Time{base, base.Add(time.Second)}
	if len(got) != len(want) {
		t.Fatalf("loaded %d attempts, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("attempt %d = %s, want %s (oldest first)", i, got[i], want[i])
		}
	}
}
