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

type fakeLoyaltyReader struct {
	values map[string]int64
	ok     bool
}

func (f *fakeLoyaltyReader) get(ctx context.Context, userID, name string) (int64, bool) {
	return f.values[name], f.ok
}

func TestSnapshotCounterBaselineSkipsOnLoyaltyFailure(t *testing.T) {
	p := &Projector{loyalty: &fakeLoyaltyReader{ok: false}, log: zap.NewNop()}
	assert.NotPanics(t, func() {
		p.snapshotCounterBaseline(context.Background(), 123, zap.NewNop())
	})
}

func TestSnapshotCounterBaselineNilLoyalty(t *testing.T) {
	p := &Projector{log: zap.NewNop()}
	assert.NotPanics(t, func() {
		p.snapshotCounterBaseline(context.Background(), 123, zap.NewNop())
	})
}
