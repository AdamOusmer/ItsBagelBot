package bus

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	jsapi "github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"
)

// Run with NATS_INTEGRATION_URL against NATS 2.14+ (testdata/nats-2.14.conf).
// The ordinary suite skips it so CI does not need an external broker.
func TestBatchPublisherIntegration(t *testing.T) {
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		t.Skip("NATS_INTEGRATION_URL is not set")
	}
	t.Setenv("NATS_JS_DOMAIN", "hub")
	t.Setenv("NATS_LEAF_URL", "")
	t.Setenv("NATS_HUB_URL", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log := zap.NewNop()
	if err := EnsureStreams(ctx, url, DataStreams, log); err != nil {
		t.Fatal(err)
	}
	pub, err := NewPublisher(url, log)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close() //nolint:errcheck

	const messages = 64
	start := make(chan struct{})
	errs := make(chan error, messages)
	var wg sync.WaitGroup
	for i := 0; i < messages; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- PublishJSON(context.Background(), pub, "data.test.batch", map[string]int{"n": i})
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
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer flushCancel()
	if err := pub.Flush(flushCtx); err != nil {
		t.Fatal(err)
	}

	nc, err := nats.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := nc.JetStream(nats.Domain("hub"), nats.MaxWait(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	info, err := js.StreamInfo("BAGEL_DATA")
	if err != nil {
		t.Fatal(err)
	}
	if info.State.Msgs != messages {
		t.Fatalf("stored %d messages, want %d", info.State.Msgs, messages)
	}
	modern, err := jsapi.NewWithDomain(nc, "hub")
	if err != nil {
		t.Fatal(err)
	}
	stream, err := modern.Stream(context.Background(), "BAGEL_DATA")
	if err != nil {
		t.Fatal(err)
	}
	config := stream.CachedInfo().Config
	if !config.AllowAtomicPublish || !config.AllowBatchPublish {
		t.Fatal("stream did not retain NATS 2.14 batch feature flags")
	}

	_ = js.DeleteStream("TWITCH_INGRESS")
	_ = js.DeleteStream("BAGEL_DATA")
}

// BenchmarkBatchPublisherIntegration measures the exact fleet Publisher
// contract, including UUID generation, trace-header preparation, routing,
// admission and bounded official nats.go PubAck cohorts. Run it against the
// repository's NATS 2.14 test configuration:
//
//	NATS_INTEGRATION_URL=nats://127.0.0.1:14222 \
//	go test ./pkg/bus -run '^$' -bench BenchmarkBatchPublisherIntegration -benchmem
func BenchmarkBatchPublisherIntegration(b *testing.B) {
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		b.Skip("NATS_INTEGRATION_URL is not set")
	}
	b.Setenv("NATS_JS_DOMAIN", "hub")
	b.Setenv("NATS_LEAF_URL", "")
	b.Setenv("NATS_HUB_URL", "")

	ctx := context.Background()
	log := zap.NewNop()
	if err := EnsureStreams(ctx, url, DataStreams, log); err != nil {
		b.Fatal(err)
	}
	pub, err := NewPublisher(url, log)
	if err != nil {
		b.Fatal(err)
	}
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	// Keep enough calls in flight to fill the 128-message connection-local PubAck
	// cohorts; this measures saturated capacity, not the 1 ms collection timer.
	b.SetParallelism(256)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := PublishRaw(ctx, pub, "data.test.batch", payload); err != nil {
				b.Error(err)
				return
			}
		}
	})
	flushCtx, flushCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := pub.Flush(flushCtx); err != nil {
		b.Error(err)
	}
	flushCancel()
	b.StopTimer()
	if err := pub.Close(); err != nil {
		b.Error(err)
	}

	nc, err := nats.Connect(url)
	if err == nil {
		if js, jsErr := nc.JetStream(nats.Domain("hub")); jsErr == nil {
			_ = js.DeleteStream("BAGEL_DATA")
		}
		nc.Close()
	}
}
