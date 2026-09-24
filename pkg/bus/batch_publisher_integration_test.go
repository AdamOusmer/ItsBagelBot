// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

func TestBatchPublisherIntegration(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "single")
	url, pub := openIntegrationPublisher(t)
	defer closeIntegrationPublisher(t, url, pub)
	const messages = 64
	publishIntegrationMessages(t, pub, messages)
	flushIntegrationPublisher(t, pub, 5*time.Second)
	assertIntegrationStream(t, url, messages)
}

func TestFastBatchPublisherIntegration(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "fast")
	url, pub := openIntegrationPublisher(t)
	defer closeIntegrationPublisher(t, url, pub)
	const messages = 64
	publishIntegrationMessages(t, pub, messages)
	flushIntegrationPublisher(t, pub, 5*time.Second)
	assertIntegrationStream(t, url, messages)
}

func TestAtomicBatchPublisherIntegration(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	url, pub := openIntegrationPublisher(t)
	defer closeIntegrationPublisher(t, url, pub)
	const messages = 64
	publishIntegrationMessages(t, pub, messages)
	flushIntegrationPublisher(t, pub, 5*time.Second)
	assertIntegrationStream(t, url, messages)
}

func TestAtomicBatchMessageIdentityIntegration(t *testing.T) {
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	url, pub := openIntegrationPublisher(t)
	defer closeIntegrationPublisher(t, url, pub)

	const messages = 8
	publishIdentifiedIntegrationMessages(t, pub, messages)
	flushIntegrationPublisher(t, pub, 5*time.Second)
	assertIntegrationStream(t, url, messages)

	publishIdentifiedIntegrationMessages(t, pub, messages)
	flushIntegrationPublisher(t, pub, 5*time.Second)
	assertIntegrationStream(t, url, messages*2)
}

func publishIdentifiedIntegrationMessages(t *testing.T, pub Publisher, messages int) {
	t.Helper()
	publishConcurrently(t, messages, func(i int) error {
		return PublishConfirmed(context.Background(), pub, Publication{
			Subject: "data.test.batch",
			ID:      fmt.Sprintf("atomic-dedup-%d", i),
			Payload: []byte(`{"n":1}`),
		})
	})
}

func publishIntegrationMessages(t *testing.T, pub Publisher, messages int) {
	t.Helper()
	publishConcurrently(t, messages, func(i int) error {
		return PublishJSON(context.Background(), pub, "data.test.batch", map[string]int{"n": i})
	})
}

func publishConcurrently(t *testing.T, messages int, publish func(i int) error) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, messages)
	var wg sync.WaitGroup
	for i := 0; i < messages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- publish(i)
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func assertIntegrationStream(t *testing.T, url string, messages uint64) {
	t.Helper()
	nc, js := openIntegrationJetStream(t, url)
	defer nc.Close()
	assertStoredMessages(t, js, messages)
	assertBatchFeatures(t, nc)
}

func openIntegrationJetStream(t *testing.T, url string) (*nats.Conn, nats.JetStreamContext) {
	t.Helper()
	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	js, err := nc.JetStream(nats.Domain("hub"), nats.MaxWait(2*time.Second))
	if err != nil {
		nc.Close()
		t.Fatal(err)
	}
	return nc, js
}

func assertStoredMessages(t *testing.T, js nats.JetStreamContext, messages uint64) {
	t.Helper()
	info, err := js.StreamInfo("BAGEL_DATA")
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != messages {
		t.Fatalf("stored %d messages, want %d", info.State.Msgs, messages)
	}
}

func assertBatchFeatures(t *testing.T, nc *nats.Conn) {
	t.Helper()
	modern, err := jsapi.NewWithDomain(nc, "hub")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := modern.Stream(context.Background(), "BAGEL_DATA")
	if err != nil {
		t.Fatal(err)
	}
	config := stream.CachedInfo().Config
	if !config.AllowAtomicPublish {
		t.Fatal("stream did not retain NATS 2.14 atomic publish flag")
	}
	if !config.AllowBatchPublish {
		t.Fatal("stream did not retain NATS 2.14 batch publish flag")
	}
}

func BenchmarkBatchPublisherIntegration(b *testing.B) {
	url, pub := openIntegrationPublisher(b)
	defer closeIntegrationPublisher(b, url, pub)
	payload := integrationPayload(256)
	configureIntegrationBenchmark(b, len(payload))
	b.ResetTimer()
	runIntegrationBenchmark(b, pub, payload)
	flushIntegrationPublisher(b, pub, 30*time.Second)
	b.StopTimer()
}

func openIntegrationPublisher(tb testing.TB) (string, Publisher) {
	tb.Helper()
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		tb.Skip("NATS_INTEGRATION_URL is not set")
	}
	tb.Setenv("NATS_JS_DOMAIN", "hub")
	tb.Setenv("NATS_LEAF_URL", "")
	tb.Setenv("NATS_HUB_URL", "")
	tb.Setenv("NATS_HUB_PUBLISH_URL", "")
	if err := EnsureStreams(context.Background(), url, DataStreams, zap.NewNop()); err != nil {
		tb.Fatal(err)
	}
	pub, err := NewPublisher(url, zap.NewNop())
	if err != nil {
		tb.Fatal(err)
	}
	return url, pub
}

func closeIntegrationPublisher(tb testing.TB, url string, pub Publisher) {
	tb.Helper()
	if err := pub.Close(); err != nil {
		tb.Error(err)
	}
	nc, err := nats.Connect(url)
	if err != nil {
		return
	}
	defer nc.Close()
	js, err := nc.JetStream(nats.Domain("hub"))
	if err != nil {
		return
	}
	_ = js.DeleteStream("TWITCH_INGRESS")
	_ = js.DeleteStream("BAGEL_DATA")
}

func flushIntegrationPublisher(tb testing.TB, pub Publisher, timeout time.Duration) {
	tb.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := pub.Flush(ctx); err != nil {
		tb.Error(err)
	}
}

func integrationPayload(size int) []byte {
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}
	return payload
}

func configureIntegrationBenchmark(b *testing.B, payloadSize int) {
	b.SetBytes(int64(payloadSize))
	b.ReportAllocs()
	b.SetParallelism(256)
}

func runIntegrationBenchmark(b *testing.B, pub Publisher, payload []byte) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := PublishRaw(context.Background(), pub, "data.test.batch", payload); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
