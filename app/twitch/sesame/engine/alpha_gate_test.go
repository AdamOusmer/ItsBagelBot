// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAlphaModuleNeverTakesAnEffectLeaseForANonAlphaChannel(t *testing.T) {
	cases := []struct {
		name     string
		alpha    bool
		wantRuns int
	}{
		{"alpha channel runs and holds a lease", true, 1},
		{"non-alpha channel is skipped before any lease", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newRecordingStore()
			runs := 0
			m := module.NewModule("", module.KindCore).ProtectReplay().Alpha()
			m.On(chatType, func(context.Context, *module.Context, module.Emit) error { runs++; return nil })
			deps := Deps{
				Proj: fakeReader{user: projection.User{Alpha: tc.alpha}}, Live: liveAlways{}, Cooldown: NoopCooldown{},
				Pub: &fakePublisher{}, Log: zap.NewNop(), Dedup: NewEventDedup(store, "", time.Hour, nil),
			}
			p := NewPipeline(deps, NewRegistry(zap.NewNop(), m.Build()), Config{})
			t.Cleanup(p.Close)
			msg := commandMsg(t, "alpha-gate", "hello")

			require.NoError(t, p.Process(msg))
			awaitCompletion(t, msg, true)

			require.Equal(t, tc.wantRuns, runs)
			require.Equal(t, tc.wantRuns, len(store.keys()))
		})
	}
}
