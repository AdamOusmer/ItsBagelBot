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

func TestTTSBuildersUseFileStorage(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	_, err := bus.OpenTTSJobs(ctx, nc, 1)
	require.NoError(t, err)
	_, err = bus.OpenTTSOverlay(ctx, nc, 1)
	require.NoError(t, err)

	js, err := jsapi.New(nc)
	require.NoError(t, err)
	table := []struct {
		name      string
		subjects  []string
		retention jsapi.RetentionPolicy
		maxAge    time.Duration
	}{
		{"TTS_JOBS", []string{"tts.jobs.*"}, jsapi.WorkQueuePolicy, 10 * time.Minute},
		{"OBJ_TTS_CLIPS", []string{"$O.TTS_CLIPS.C.>", "$O.TTS_CLIPS.M.>"}, jsapi.LimitsPolicy, 5 * time.Minute},
		{"OVERLAY_TTS", []string{"overlay.tts.*"}, jsapi.LimitsPolicy, 5 * time.Minute},
	}
	for _, want := range table {
		t.Run(want.name, func(t *testing.T) {
			stream, err := js.Stream(t.Context(), want.name)
			require.NoError(t, err)
			cfg := stream.CachedInfo().Config
			assert.Equal(t, jsapi.FileStorage, cfg.Storage)
			assert.Equal(t, want.retention, cfg.Retention)
			assert.Equal(t, want.maxAge, cfg.MaxAge)

			assert.ElementsMatch(t, want.subjects, cfg.Subjects)
		})
	}

	consumer, err := js.Consumer(ctx, "TTS_JOBS", "tts-worker")
	require.NoError(t, err)
	cfg := consumer.CachedInfo().Config
	assert.Equal(t, jsapi.AckExplicitPolicy, cfg.AckPolicy)
	assert.Equal(t, 3, cfg.MaxDeliver)

	assert.Empty(t, cfg.DeliverSubject)
	assert.False(t, cfg.MemoryStorage)
	assert.Equal(t, 3, bus.TTSReplicas)
}
