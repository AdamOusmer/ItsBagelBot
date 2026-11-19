// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"testing"
	"time"

	"ItsBagelBot/internal/valkeytest"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestReputationReceiptUsesTheDeliveryHorizon(t *testing.T) {
	for _, delivery := range []bool{false, true} {
		t.Run(map[bool]string{false: "fallback", true: "hot lane"}[delivery], func(t *testing.T) {
			b := newCompletionBroker(t, 20*time.Second)
			client := valkeytest.Spawn(t)
			ctx, want := t.Context(), bus.TwitchEffectRetention()
			if delivery {
				b.publish(t)
				msg := b.receive(t)
				defer msg.Ack()
				ctx = msg.Context()
			}
			reputation := NewValkeyReputation(client, time.Hour, zap.NewNop())
			_, err := reputation.Observe(ctx, ReputationEvent{Viewer: "999", Identity: "retention"})
			require.NoError(t, err)
			ttl, err := client.Do(t.Context(), client.B().Pttl().Key("am:event:retention:999").Build()).ToInt64()
			require.NoError(t, err)
			require.InDelta(t, want.Milliseconds(), ttl, 1000)
		})
	}
}
