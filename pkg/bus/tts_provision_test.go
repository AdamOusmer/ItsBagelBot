// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	jsapi "github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
)

func TestTTSProvisioningSucceedsAcrossConcurrentReplicas(t *testing.T) {
	nc := bustest.JetStream(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)
	start := make(chan struct{})
	for range 3 {
		group.Go(func() error {
			<-start
			if _, err := bus.OpenTTSJobs(ctx, nc, 1); err != nil {
				return fmt.Errorf("open TTS jobs: %w", err)
			}
			_, err := bus.OpenOverlays(ctx, nc, 1)
			return err
		})
	}
	close(start)
	require.NoError(t, group.Wait())
}

func TestTTSProvisioningAdoptsExistingOverlayResources(t *testing.T) {
	ctx := t.Context()
	nc := bustest.JetStream(t)
	js, err := jsapi.NewWithDomain(nc, bus.JSDomain())
	require.NoError(t, err)
	const existingTTL = time.Minute
	stream, err := js.CreateStream(ctx, jsapi.StreamConfig{
		Name: bus.OverlaysPremiumStream, Subjects: []string{"overlays.premium.>"}, Retention: jsapi.WorkQueuePolicy, Storage: jsapi.MemoryStorage, MaxAge: existingTTL,
	})
	require.NoError(t, err)
	bucket, err := js.CreateObjectStore(ctx, jsapi.ObjectStoreConfig{Bucket: bus.OverlaysMediaBucket, TTL: existingTTL})
	require.NoError(t, err)
	_, err = bus.OpenOverlays(ctx, nc, 1)
	require.NoError(t, err)

	info, err := stream.Info(ctx)
	require.NoError(t, err)
	status, err := bucket.Status(ctx)
	require.NoError(t, err)
	require.Equal(t, []time.Duration{bus.OverlayClipTTL, bus.OverlayClipTTL}, []time.Duration{info.Config.MaxAge, status.TTL()})
}
