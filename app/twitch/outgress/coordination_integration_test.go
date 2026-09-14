// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"context"
	"os"
	"testing"
	"time"

	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/ratelimit"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/nats-io/nuid"
	"github.com/stretchr/testify/require"
)

// Uses only a explicitly supplied local three-node fixture, never production.
func TestSendingCoordinationQuorumIntegration(t *testing.T) {
	url := os.Getenv("OUTGRESS_COORDINATION_TEST_URL")
	if url == "" {
		t.Skip("set OUTGRESS_COORDINATION_TEST_URL to a local R3 NATS fixture")
	}
	nc, err := nats.Connect(url, nats.UserInfo("outgress_bus", "test-password"))
	require.NoError(t, err)
	defer nc.Close()
	js, err := jetstream.NewWithDomain(nc, "hub")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	probe := nuid.Next()
	for _, bucket := range []string{"outgress_rate", "outgress_batch", "outgress_pause"} {
		kv, err := bus.CoordinationBucket(ctx, js, bucket, time.Minute)
		require.NoError(t, err)
		revision, err := kv.Create(ctx, probe, []byte("1"))
		require.NoError(t, err)
		_, err = kv.Update(ctx, probe, []byte("2"), revision)
		require.NoError(t, err)
		entry, err := kv.Get(ctx, probe)
		require.NoError(t, err)
		require.Equal(t, "2", string(entry.Value()))
	}
	kv, err := js.KeyValue(ctx, "outgress_rate")
	require.NoError(t, err)
	req := ratelimit.NewSpec(2, .001).ForKey(probe)
	for range 2 {
		ok, err := ratelimit.NewJetStreamManager(kv).Allow(ctx, req)
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := ratelimit.NewJetStreamManager(kv).Allow(ctx, req)
	require.NoError(t, err)
	require.False(t, ok)
}
