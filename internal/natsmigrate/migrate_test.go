// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsmigrate_test

import (
	"bytes"
	"testing"
	"time"

	"ItsBagelBot/internal/natsmigrate"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	streamName = "ORDERS"
	published  = 5
	acked      = 2
)

func startJetStream(t *testing.T, domain string) (*nats.Conn, jetstream.JetStream) {
	t.Helper()
	s, err := server.NewServer(&server.Options{
		Host: "127.0.0.1", Port: -1, JetStream: true, JetStreamDomain: domain,
		StoreDir: t.TempDir(), NoLog: true, NoSigs: true,
	})
	require.NoError(t, err)
	s.Start()
	t.Cleanup(s.Shutdown)
	require.True(t, s.ReadyForConnections(5*time.Second), "server did not become ready")

	nc, err := nats.Connect(s.ClientURL())
	require.NoError(t, err)
	t.Cleanup(nc.Close)

	if domain == "" {
		js, err := jetstream.New(nc)
		require.NoError(t, err)
		return nc, js
	}
	js, err := jetstream.NewWithDomain(nc, domain)
	require.NoError(t, err)
	return nc, js
}

func seedOrders(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	ctx := t.Context()
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: streamName, Subjects: []string{streamName + ".>"}})
	require.NoError(t, err)
	for range published {
		_, err := js.Publish(ctx, streamName+".msg", []byte("payload"))
		require.NoError(t, err)
	}
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{Durable: "watcher", AckPolicy: jetstream.AckExplicitPolicy})
	require.NoError(t, err)
	for range acked {
		msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
		require.NoError(t, err)
		for msg := range msgs.Messages() {
			require.NoError(t, msg.Ack())
		}
	}
}

func TestSnapshotRestoreRoundTrip(t *testing.T) {
	for _, domain := range []string{"", "hub"} {
		t.Run("domain="+domain, func(t *testing.T) {
			nc, js := startJetStream(t, domain)
			seedOrders(t, js)

			var buf bytes.Buffer
			stats, err := natsmigrate.Snapshot(nc, streamName, &buf, natsmigrate.SnapshotOptions{Domain: domain})
			require.NoError(t, err)
			assert.NotZero(t, stats.Bytes, "a non-empty stream snapshots to bytes")
			require.NoError(t, js.DeleteStream(t.Context(), streamName))

			restored, err := natsmigrate.Restore(nc, &buf, natsmigrate.RestoreOptions{Domain: domain})
			require.NoError(t, err)
			assert.Equal(t, streamName, restored.Stream)

			stream, err := js.Stream(t.Context(), streamName)
			require.NoError(t, err)
			assert.EqualValues(t, published, stream.CachedInfo().State.Msgs)
			cons, err := stream.Consumer(t.Context(), "watcher")
			require.NoError(t, err)
			info, err := cons.Info(t.Context())
			require.NoError(t, err)
			assert.EqualValues(t, acked, info.AckFloor.Consumer)
			assert.EqualValues(t, published-acked, info.NumPending)
		})
	}
}

func TestSnapshotAndRestoreRefuseBadInput(t *testing.T) {
	nc, js := startJetStream(t, "")
	seedOrders(t, js)

	_, snapErr := natsmigrate.Snapshot(nc, "MISSING", &bytes.Buffer{}, natsmigrate.SnapshotOptions{})
	_, restoreErr := natsmigrate.Restore(nc, bytes.NewBufferString("not a snapshot"), natsmigrate.RestoreOptions{})

	assert.Error(t, snapErr, "an unknown stream cannot be snapshotted")
	assert.Error(t, restoreErr, "a stream without the snapshot header cannot be restored")
}
