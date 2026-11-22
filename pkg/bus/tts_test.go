// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus_test

import (
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverlayBuildersStreamConfig(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	_, err := bus.OpenOverlays(ctx, nc, 1)
	require.NoError(t, err)

	js, err := jsapi.New(nc)
	require.NoError(t, err)
	table := []struct {
		name      string
		subjects  []string
		retention jsapi.RetentionPolicy
		maxAge    time.Duration
		storage   jsapi.StorageType
	}{
		{"OBJ_OVERLAYS_MEDIA", []string{"$O.OVERLAYS_MEDIA.C.>", "$O.OVERLAYS_MEDIA.M.>"}, jsapi.LimitsPolicy, 5 * time.Minute, jsapi.FileStorage},
		{"OVERLAYS_PREMIUM", []string{"overlays.premium.>"}, jsapi.WorkQueuePolicy, 5 * time.Minute, jsapi.MemoryStorage},
		{"OVERLAYS_STANDARD", []string{"overlays.standard.>"}, jsapi.WorkQueuePolicy, 5 * time.Minute, jsapi.MemoryStorage},
	}
	for _, want := range table {
		t.Run(want.name, func(t *testing.T) {
			stream, err := js.Stream(t.Context(), want.name)
			require.NoError(t, err)
			cfg := stream.CachedInfo().Config
			assert.Equal(t, want.storage, cfg.Storage)
			assert.Equal(t, want.retention, cfg.Retention)
			assert.Equal(t, want.maxAge, cfg.MaxAge)
			assert.ElementsMatch(t, want.subjects, cfg.Subjects)
		})
	}
}

func TestBindOverlaysFailsUntilProvisionedThenSharesTheLanes(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	_, err := bus.BindOverlays(ctx, nc)
	require.Error(t, err)

	provisioned, err := bus.OpenOverlays(ctx, nc, 1)
	require.NoError(t, err)
	bound, err := bus.BindOverlays(ctx, nc)
	require.NoError(t, err)

	key := "42/bound"
	_, err = bound.PutClip(ctx, bus.OverlayClipData{OverlayClip: bus.OverlayClip{Key: key}, WAV: []byte("wav")})
	require.NoError(t, err)
	got, err := provisioned.GetClip(ctx, bus.OverlayClip{Key: key})
	require.NoError(t, err)
	assert.Equal(t, key, got.Key)
	require.NoError(t, bound.Publish(ctx, bus.OverlayMessage{Tier: bus.OverlayPremium, Kind: bus.OverlayKindTTS, UserID: "42", EventID: "bound", Value: 1}))
}
