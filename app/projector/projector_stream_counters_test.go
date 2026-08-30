// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestIsGoLiveEdge(t *testing.T) {
	cases := []struct {
		name           string
		wasLive, live  bool
		wantGoLiveEdge bool
	}{
		{"cold key going live", false, true, true},
		{"already live re-delivery", true, true, false},
		{"going offline", true, false, false},
		{"already offline re-delivery", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.wantGoLiveEdge, isGoLiveEdge(c.wasLive, c.live))
		})
	}
}

// fakeLoyaltyReader lets snapshotCounterBaseline tests control the (value,
// ok) pair get() returns per counter name without a NATS connection.
type fakeLoyaltyReader struct {
	values map[string]int64
	ok     bool
}

func (f *fakeLoyaltyReader) get(ctx context.Context, userID, name string) (int64, bool) {
	return f.values[name], f.ok
}

// A failed loyalty read must never fall through to writing a baseline: a
// zeroed baseline written after a failure would later read back as this
// channel's entire lifetime total in the per-stream slot (see
// snapshotCounterBaseline's doc). p.store stays nil here on purpose — if the
// method reached the write path with a nil store it would panic, which is
// exactly the assertion that it did not.
func TestSnapshotCounterBaselineSkipsOnLoyaltyFailure(t *testing.T) {
	p := &Projector{loyalty: &fakeLoyaltyReader{ok: false}, log: zap.NewNop()}
	assert.NotPanics(t, func() {
		p.snapshotCounterBaseline(context.Background(), 123, zap.NewNop())
	})
}

// A nil loyalty reader (Deps.Loyalty left unset) must be a no-op, not a
// panic — see the Projector.loyalty field doc.
func TestSnapshotCounterBaselineNilLoyalty(t *testing.T) {
	p := &Projector{log: zap.NewNop()}
	assert.NotPanics(t, func() {
		p.snapshotCounterBaseline(context.Background(), 123, zap.NewNop())
	})
}
