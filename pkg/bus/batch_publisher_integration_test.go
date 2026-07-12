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

// Run with NATS_INTEGRATION_URL against NATS 2.14+ (testdata/nats-2.14.conf).
// The ordinary suite skips it so CI does not need an external broker.
func TestBatchPublisherIntegration(t *testing.T) {
	url, pub, cancel := integrationPublisher(t)
	defer cancel()
	defer pub.Close() //nolint:errcheck

	const messages = 64
	publishIntegrationBurst(t, pub, messages)
	assertIntegrationStream(t, url, messages)
	deleteIntegrationStreams(url)
}

func integrationPublisher(t testing.TB) (string, Publisher, context.CancelFunc) {
	t.Helper()
	url := os.Getenv("NATS_INTEGRATION_URL")
	if url == "" {
		t.Skip("NATS_INTEGRATION_URL is not set")
	}
	t.Setenv("NATS_JS_DOMAIN", "hub")
	t.Setenv("NATS_LEAF_URL", "")
	t.Setenv("NATS_HUB_URL", "")
	ctx, cancel := context.WithCancel(context.Background())
	log := zap.NewNop()
	if err := EnsureStreams(ctx, url, DataStreams, log); err != nil {
		cancel()
		t.Fatal(err)
	}
	pub, err := NewPublisher(url, log)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	return url, pub, cancel
}

func publishIntegrationBurst(t *testing.T, pub Publisher, messages int) {
	t.Helper()
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
}

func assertIntegrationStream(t *testing.T, url string, messages uint64) {
	t.Helper()
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
	if !config.AllowAtomicPublish {
		t.Fatal("stream did not retain NATS 2.14 atomic publish flag")
	}
	if !config.AllowBatchPublish {
		t.Fatal("stream did not retain NATS 2.14 batch feature flags")
	}

	batched := 0
	for seq := uint64(1); seq <= messages; seq++ {
		msg, err := js.GetMsg("BAGEL_DATA", seq)
		if err != nil {
			t.Fatal(fmt.Errorf("get message %d: %w", seq, err))
		}
		if msg.Header.Get(batchIDHeader) != "" {
			batched++
		}
	}
	if batched < 2 {
		t.Fatalf("only %d/%d writes used a batch", batched, messages)
	}

}

// BenchmarkBatchPublisherIntegration measures the exact fleet Publisher
// contract, including UUID generation, trace-header preparation, routing,
// admission, atomic batching and PubAck reconciliation. Run it against the
// repository's NATS 2.14 test configuration:
//
//	NATS_INTEGRATION_URL=nats://127.0.0.1:14222 \
//	go test ./pkg/bus -run '^$' -bench BenchmarkBatchPublisherIntegration -benchmem
func BenchmarkBatchPublisherIntegration(b *testing.B) {
	url, pub, cancel := integrationPublisher(b)
	defer cancel()
	ctx := context.Background()
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte('a' + i%26)
	}

	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	// The fleet publisher is a microbatcher. Keep enough calls in flight to fill
	// four 128-message connection-local commits when run with the documented
	// GOMAXPROCS=2; this measures saturated capacity, not the 1 ms flush timer.
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

	deleteIntegrationStreams(url)
}

func deleteIntegrationStreams(url string) {
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
