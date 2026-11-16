// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/internal/analytics"
	"ItsBagelBot/internal/channelruntime"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/lifecycle"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/internal/valkeytest"
	pkgvalkey "ItsBagelBot/pkg/valkey"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestPublishedTimersRecordActivityAndActivationAge(t *testing.T) {
	for _, tc := range []struct {
		days int
		age  string
	}{
		{0, "d0"}, {1, "d1"}, {2, "d2_6"}, {6, "d2_6"}, {7, "d7_13"},
		{13, "d7_13"}, {14, "d14_29"}, {29, "d14_29"}, {30, "d30p"},
	} {
		t.Run(tc.age+time.Duration(tc.days).String(), func(t *testing.T) {
			vc := valkeytest.Spawn(t)
			rec := analytics.New(vc, zap.NewNop(), nil, analytics.WithoutTicker())
			created := time.Now().Add(-time.Duration(tc.days)*24*time.Hour - time.Minute)
			at := armedTimer{ref: timerRef{broadcasterID: 123, id: "timer"}, def: timerDef{Message: "served"}}
			pub := &fakePublisher{}
			store := &ValkeyTimerStore{pub: pub, log: zap.NewNop(), pipeline: &Pipeline{analytics: rec}, now: time.Now, proj: fakeReader{user: projection.User{AccountCreatedAt: created.UnixMicro()}}}
			store.fire(context.Background(), at)
			store.fire(context.Background(), at)
			pub.failErr = errors.New("publication rejected")
			at.ref.broadcasterID = 456
			store.fire(context.Background(), at)
			rec.Close()
			counts, err := pkgvalkey.HashAllInts(context.Background(), vc, catalog.CounterKey(time.Now()))
			require.NoError(t, err)
			require.Equal(t, map[string]int64{"legacy": 2, "ticks": 2}, map[string]int64{"legacy": counts["timer.fires"], "ticks": counts["timers.ticks|fired"]})
			for _, field := range []string{"channels.active", "activation.age|" + tc.age} {
				n, err := pkgvalkey.SetCard(context.Background(), vc, catalog.DistinctKey(catalog.Day, time.Now(), field))
				require.NoError(t, err)
				require.EqualValues(t, 1, n, field)
			}
			cohort, err := pkgvalkey.SetCard(context.Background(), vc, catalog.CohortKey(created, time.Now()))
			require.NoError(t, err)
			require.EqualValues(t, 1, cohort)
		})
	}
}

func TestTimerProjectionDoesNotChangeAdmissionFence(t *testing.T) {
	for _, createdAt := range []int64{0, 123} {
		t.Run(time.Duration(createdAt).String(), func(t *testing.T) {
			at := armedTimer{
				ref: timerRef{broadcasterID: 123, id: "timer"}, def: timerDef{Message: "served"},
				admission: channelruntime.Snapshot{Registered: true, AccountCreatedAt: createdAt, State: lifecycle.State{AdmissionGeneration: 7}},
			}
			pub := &fakePublisher{}
			store := &ValkeyTimerStore{pub: pub, log: zap.NewNop(), proj: fakeReader{user: projection.User{AccountCreatedAt: 456}}}
			store.fire(context.Background(), at)
			outputs := pub.snapshot()
			require.Len(t, outputs, 1)
			require.Equal(t, createdAt, outputs[0].msg.AccountCreatedAt)
			require.EqualValues(t, 7, outputs[0].msg.AdmissionGeneration)
		})
	}
}
