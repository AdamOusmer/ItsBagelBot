// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus_test

import (
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlaysPublishLandsOnTierStreamAndDurable(t *testing.T) {
	table := []struct {
		tier    bus.OverlayTier
		stream  string
		durable string
		subject string
	}{
		{bus.OverlayPremium, "OVERLAYS_PREMIUM", "overlays-premium", "overlays.premium.tts.42"},
		{bus.OverlayStandard, "OVERLAYS_STANDARD", "overlays-standard", "overlays.standard.tts.42"},
	}
	ctx := t.Context()
	nc := bustest.JetStream(t)
	overlays, err := bus.OpenOverlays(ctx, nc, 1)
	require.NoError(t, err)
	js, err := jsapi.New(nc)
	require.NoError(t, err)

	for _, want := range table {
		t.Run(string(want.tier), func(t *testing.T) {
			msg := bus.OverlayMessage{Tier: want.tier, Kind: bus.OverlayKindTTS, UserID: "42", EventID: "evt-" + string(want.tier), Value: map[string]string{"k": "v"}}
			require.NoError(t, overlays.Publish(ctx, msg))

			consumer, err := js.Consumer(ctx, want.stream, want.durable)
			require.NoError(t, err)
			cfg := consumer.CachedInfo().Config
			assert.Equal(t, jsapi.AckExplicitPolicy, cfg.AckPolicy)
			assert.Equal(t, 30*time.Second, cfg.AckWait)
			assert.Equal(t, -1, cfg.MaxDeliver)
			assert.Equal(t, -1, cfg.MaxAckPending)
			assert.Equal(t, jsapi.DeliverAllPolicy, cfg.DeliverPolicy)
			assert.Empty(t, cfg.FilterSubject)

			batch, err := consumer.Fetch(1, jsapi.FetchMaxWait(3*time.Second))
			require.NoError(t, err)
			got := <-batch.Messages()
			require.NotNil(t, got)
			assert.Equal(t, want.subject, got.Subject())
			assert.Equal(t, msg.EventID, got.Headers().Get(nats.MsgIdHdr))
			assert.JSONEq(t, `{"k":"v"}`, string(got.Data()))
			require.NoError(t, got.Ack())
		})
	}
}

func TestOverlaysPublishRejectsUnknownTier(t *testing.T) {
	overlays, err := bus.OpenOverlays(t.Context(), bustest.JetStream(t), 1)
	require.NoError(t, err)
	err = overlays.Publish(t.Context(), bus.OverlayMessage{Tier: "gold", Kind: bus.OverlayKindTTS, UserID: "42", EventID: "e", Value: 1})
	require.Error(t, err)
}

func TestOverlaysClipRoundTripsInMediaBucket(t *testing.T) {
	ctx := t.Context()
	overlays, err := bus.OpenOverlays(ctx, bustest.JetStream(t), 1)
	require.NoError(t, err)
	key := "42/evt"
	stored, err := overlays.PutClip(ctx, bus.OverlayClipData{
		OverlayClip: bus.OverlayClip{Key: key, Metadata: map[string]string{"duration_ms": "10"}}, WAV: []byte("wav"),
	})
	require.NoError(t, err)
	got, err := overlays.GetClip(ctx, bus.OverlayClip{Key: key})
	require.NoError(t, err)
	assert.Equal(t, stored.Key, got.Key)
	assert.Equal(t, "10", got.Metadata["duration_ms"])
}
