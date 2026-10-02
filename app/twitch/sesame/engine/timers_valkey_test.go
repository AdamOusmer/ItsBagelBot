// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func storeWith(live IsLiveChecker, reader *countingReader) *ValkeyTimerStore {
	return &ValkeyTimerStore{live: live, proj: reader, log: zap.NewNop()}
}

func TestRearmReadsModulesOnlyWhenItArms(t *testing.T) {
	cases := []struct {
		name      string
		live      fakeLive
		broadcast uint64
		wantReads int
	}{
		{"a live broadcaster arms every timer", fakeLive{live: true}, 42, 1},
		{"an offline broadcaster arms only offline timers", fakeLive{live: false}, 42, 1},
		{"a live-check error arms nothing", fakeLive{live: true, err: errors.New("valkey down")}, 42, 0},
		{"a zero broadcaster id arms nothing", fakeLive{live: true}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &countingReader{}

			storeWith(tc.live, reader).Rearm(context.Background(), tc.broadcast)

			assert.Equal(t, tc.wantReads, reader.moduleReads)
		})
	}
}

type fakeLive struct {
	live bool
	err  error
}

func (f fakeLive) IsLive(context.Context, uint64) (bool, error) { return f.live, f.err }

type countingReader struct {
	fakeReader
	moduleReads int
	userReads   int
}

func (r *countingReader) User(ctx context.Context, id uint64) (projection.User, error) {
	r.userReads++
	return r.fakeReader.User(ctx, id)
}

func (r *countingReader) Modules(ctx context.Context, id uint64) (map[string]projection.ModuleView, error) {
	r.moduleReads++
	return r.fakeReader.Modules(ctx, id)
}

func (r *countingReader) Module(ctx context.Context, id uint64, name string) (projection.ModuleView, bool, error) {
	r.moduleReads++
	return r.fakeReader.Module(ctx, id, name)
}
