// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsmigrate

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func startTestServer(t *testing.T, domain string) (*server.Server, *nats.Conn) {
	t.Helper()
	opts := &server.Options{
		Host:            "127.0.0.1",
		Port:            -1,
		JetStream:       true,
		JetStreamDomain: domain,
		StoreDir:        t.TempDir(),
		NoLog:           true,
		NoSigs:          true,
	}
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	s.Start()
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("server did not become ready")
	}
	t.Cleanup(s.Shutdown)

	nc, err := nats.Connect(s.ClientURL())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(nc.Close)
	return s, nc
}

func jsFor(t *testing.T, nc *nats.Conn, domain string) jetstream.JetStream {
	t.Helper()
	if domain == "" {
		js, err := jetstream.New(nc)
		if err != nil {
			t.Fatalf("jetstream client: %v", err)
		}
		return js
	}
	js, err := jetstream.NewWithDomain(nc, domain)
	if err != nil {
		t.Fatalf("jetstream client: %v", err)
	}
	return js
}

func seedStream(t *testing.T, js jetstream.JetStream, name string, n int) jetstream.Stream {
	t.Helper()
	ctx := context.Background()
	stream, err := js.CreateStream(ctx, jetstream.StreamConfig{Name: name, Subjects: []string{name + ".>"}})
	if err != nil {
		t.Fatalf("create stream %s: %v", name, err)
	}
	for i := 0; i < n; i++ {
		if _, err := js.Publish(ctx, name+".msg", []byte("payload")); err != nil {
			t.Fatalf("publish to %s: %v", name, err)
		}
	}
	return stream
}

func seedConsumer(t *testing.T, stream jetstream.Stream, ack int) jetstream.Consumer {
	t.Helper()
	ctx := context.Background()
	cons, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
		Durable:   "watcher",
		AckPolicy: jetstream.AckExplicitPolicy,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	for i := 0; i < ack; i++ {
		fetchAndAck(t, cons)
	}
	return cons
}

func fetchAndAck(t *testing.T, cons jetstream.Consumer) {
	t.Helper()
	msgs, err := cons.Fetch(1, jetstream.FetchMaxWait(2*time.Second))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	for msg := range msgs.Messages() {
		if err := msg.Ack(); err != nil {
			t.Fatalf("ack: %v", err)
		}
	}
}

func testSnapshotRestoreRoundTrip(t *testing.T, domain string) {
	t.Helper()
	s, nc := startTestServer(t, domain)
	_ = s
	js := jsFor(t, nc, domain)

	stream := seedStream(t, js, "ORDERS", 5)
	seedConsumer(t, stream, 2)

	var buf bytes.Buffer
	stats, err := Snapshot(nc, "ORDERS", &buf, SnapshotOptions{Domain: domain})
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if stats.Bytes == 0 {
		t.Fatal("snapshot captured zero bytes for a non-empty stream")
	}

	ctx := context.Background()
	if err := js.DeleteStream(ctx, "ORDERS"); err != nil {
		t.Fatalf("delete stream before restore: %v", err)
	}

	restoreStats, err := Restore(nc, &buf, RestoreOptions{Domain: domain})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restoreStats.Stream != "ORDERS" {
		t.Fatalf("restore stats stream = %q, want ORDERS", restoreStats.Stream)
	}
	assertOrdersRestored(t, js)
}

func assertOrdersRestored(t *testing.T, js jetstream.JetStream) {
	t.Helper()
	ctx := context.Background()
	info, err := js.Stream(ctx, "ORDERS")
	if err != nil {
		t.Fatalf("stream info after restore: %v", err)
	}
	cfg := info.CachedInfo()
	if cfg.State.Msgs != 5 {
		t.Fatalf("restored message count = %d, want 5", cfg.State.Msgs)
	}

	cons, err := info.Consumer(ctx, "watcher")
	if err != nil {
		t.Fatalf("consumer after restore: %v", err)
	}
	consInfo, err := cons.Info(ctx)
	if err != nil {
		t.Fatalf("consumer info after restore: %v", err)
	}
	if consInfo.AckFloor.Consumer != 2 {
		t.Fatalf("restored consumer ack floor = %d, want 2", consInfo.AckFloor.Consumer)
	}
	if consInfo.NumPending != 3 {
		t.Fatalf("restored consumer num pending = %d, want 3", consInfo.NumPending)
	}
}

func TestSnapshotRestoreRoundTripPlainDomain(t *testing.T) {
	testSnapshotRestoreRoundTrip(t, "")
}

func TestSnapshotRestoreRoundTripHubDomain(t *testing.T) {
	testSnapshotRestoreRoundTrip(t, "hub")
}
