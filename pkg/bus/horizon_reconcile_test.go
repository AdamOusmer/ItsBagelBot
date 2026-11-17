// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGuardianRepairsHotStreamDriftWithoutReconnect(t *testing.T) {
	f := completionFixture(t)
	specs := []StreamSpec{TwitchIngressPremiumStream, TwitchIngressStandardStream}
	for i := range specs {
		specs[i].Replicas = 1
	}
	require.NoError(t, EnsureStreams(t.Context(), f.url, specs, zap.NewNop()))
	for _, spec := range specs {
		info, err := f.js.StreamInfo(spec.Name)
		require.NoError(t, err)
		info.Config.Retention, info.Config.MaxAge, info.Config.MaxBytes = nats.LimitsPolicy, 10*time.Second, 640<<20
		info.Config.Duplicates = info.Config.MaxAge
		_, err = f.js.UpdateStream(&info.Config)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool {
		for _, spec := range specs {
			info, err := f.js.StreamInfo(spec.Name)
			if err != nil {
				return false
			}
			got := [3]any{info.Config.Retention, info.Config.MaxAge, info.Config.MaxBytes}
			if got != [3]any{spec.Retention, spec.MaxAge, spec.MaxBytes} {
				return false
			}
		}
		return true
	}, 35*time.Second, 100*time.Millisecond)
}

func TestPullRefreshesReceiptHorizonAfterStreamChange(t *testing.T) {
	for _, age := range []time.Duration{10 * time.Second, 2 * time.Minute, 10 * time.Minute} {
		t.Run(age.String(), func(t *testing.T) {
			f := completionFixture(t)
			t.Setenv("NATS_PULL_ACK_WAIT", "300ms")
			messages := subscribe(t, openSubscriber(t, f, "worker"), hotLane)
			spec := TwitchIngressPremiumStream
			spec.MaxAge, spec.Replicas = age, 1
			require.NoError(t, EnsureStreams(t.Context(), f.url, []StreamSpec{spec}, zap.NewNop()))
			require.Eventually(t, func() bool {
				f.appendMessage(t, hotLane, "refreshed horizon")
				msg := receive(t, messages)
				retention := DeliveryEffectRetention(msg.Context())
				msg.Ack()
				return retention == max(TwitchEffectRetention(), age+300*time.Millisecond)
			}, 2*time.Second, 20*time.Millisecond)
		})
	}
}
