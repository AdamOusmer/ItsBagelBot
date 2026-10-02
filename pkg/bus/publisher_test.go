// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const publishBurstSubject = "data.test.batch"

func openPublisher(t *testing.T, url string) Publisher {
	t.Helper()
	t.Setenv("NATS_JS_DOMAIN", "hub")
	t.Setenv("NATS_LEAF_URL", "")
	t.Setenv("NATS_HUB_URL", "")
	t.Setenv("NATS_HUB_PUBLISH_URL", "")
	pub, err := NewPublisher(url, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = pub.Close() })
	return pub
}

func flush(t *testing.T, pub Publisher) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return pub.Flush(ctx)
}

func publishConcurrently(t *testing.T, messages int, publish func(i int) error) {
	t.Helper()
	start := make(chan struct{})
	errs := make(chan error, messages)
	var wg sync.WaitGroup
	for i := range messages {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- publish(i)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestPublisherStoresEveryMessageOnEveryWire(t *testing.T) {
	f := newBusFixture(t)

	for _, wire := range []string{"single", "fast", "atomic"} {
		t.Run(wire, func(t *testing.T) {
			require.NoError(t, f.js.PurgeStream(BagelDataStream.Name))
			t.Setenv("NATS_PUBLISH_WIRE", wire)
			pub := openPublisher(t, f.url)
			ctx := WithPublishPartition(context.Background(), "channel-123")

			publishConcurrently(t, 64, func(i int) error {
				return PublishJSON(ctx, pub, publishBurstSubject, map[string]int{"n": i})
			})

			require.NoError(t, flush(t, pub))
			assert.EqualValues(t, 64, f.storedMessages(t, BagelDataStream.Name))
			stored, err := f.js.GetLastMsg(BagelDataStream.Name, publishBurstSubject)
			require.NoError(t, err)
			assert.NotEmpty(t, stored.Header.Get(MessageIDHeader), "every message carries the fleet identity")
			assert.Empty(t, stored.Header.Get(nats.MsgIdHdr), "the broker dedup header must stay unset")
		})
	}
}

func TestConfirmedPublishesKeepTheirIdentityWithoutBrokerDeduplication(t *testing.T) {
	f := newBusFixture(t)
	t.Setenv("NATS_PUBLISH_WIRE", "atomic")
	pub := openPublisher(t, f.url)
	confirmed := func(i int) error {
		return PublishConfirmed(context.Background(), pub, Publication{
			Subject: publishBurstSubject, ID: fmt.Sprintf("atomic-dedup-%d", i%8), Payload: []byte(`{"n":1}`),
		})
	}

	publishConcurrently(t, 16, confirmed)

	require.NoError(t, flush(t, pub))
	assert.EqualValues(t, 16, f.storedMessages(t, BagelDataStream.Name), "a repeated identity must be stored again")
	assert.Error(t, PublishConfirmed(context.Background(), pub, Publication{Subject: publishBurstSubject}), "a confirmed publish needs an ID")
}

func TestPublishRoutesEverySubjectToItsCatalogStream(t *testing.T) {
	f := newBusFixture(t)
	pub := openPublisher(t, f.url)

	for _, tc := range []struct {
		subject string
		stream  string
	}{
		{"data.users.updated", "BAGEL_DATA"},
		{"twitch.ingress.event.premium", "TWITCH_INGRESS"},
		{"twitch.ingress.event.stream", "TWITCH_INGRESS"},
		{"twitch.ingress.status.authz.revoked", "TWITCH_INGRESS"},
		{"twitch.ingress.event.standard", "TWITCH_INGRESS_STANDARD"},
		{"twitch.ingress.retry.standard", "TWITCH_INGRESS_RETRY"},
		{"twitch.outgress.premium", "TWITCH_OUTGRESS"},
		{"twitch.outgress.standard", "TWITCH_OUTGRESS"},
		{"twitch.outgress.system", "TWITCH_OUTGRESS_SYSTEM"},
		{"youtube.outgress.premium", "YOUTUBE_OUTGRESS"},
		{"youtube.ingress.event.standard", "YOUTUBE_INGRESS"},
		{"youtube.ingress.status.chat.up", "YOUTUBE_INGRESS"},
	} {
		t.Run(tc.subject, func(t *testing.T) {
			before := f.storedMessages(t, tc.stream)

			require.NoError(t, PublishRaw(context.Background(), pub, tc.subject, []byte(`{}`)))
			require.NoError(t, flush(t, pub))

			assert.Equal(t, before+1, f.storedMessages(t, tc.stream), "the subject must land in %s", tc.stream)
		})
	}
	assert.Error(t, PublishRaw(context.Background(), pub, "twitch.ingress.event.unknown", []byte(`{}`)),
		"an unclaimed ingress lane must be refused")
}

func TestPublisherConnectsThroughThePublishURLOverride(t *testing.T) {
	f := newBusFixture(t)
	t.Setenv("NATS_HUB_PUBLISH_URL", f.url)

	routed, err := NewPublisher("nats://127.0.0.1:1", zap.NewNop())
	require.NoError(t, err)
	defer routed.Close()

	require.NoError(t, PublishRaw(context.Background(), routed, publishBurstSubject, []byte(`{}`)))
	assert.NoError(t, flush(t, routed))
}

func TestPublishFailsOnceThePublisherIsClosed(t *testing.T) {
	f := newBusFixture(t)
	pub := openPublisher(t, f.url)
	require.NoError(t, pub.Close())

	assert.Error(t, PublishRaw(context.Background(), pub, publishBurstSubject, []byte(`{}`)))
}

func TestBatchWiresFallBackToPerMessagePublishingWhenTheStreamRefusesBatches(t *testing.T) {
	for _, wire := range []string{"fast", "atomic"} {
		t.Run(wire, func(t *testing.T) {
			url := privateBroker(t)
			js := jetStreamOf(t, url)
			_, err := js.AddStream(&nats.StreamConfig{Name: BagelDataStream.Name, Subjects: BagelDataStream.Subjects})
			require.NoError(t, err)
			t.Setenv("NATS_PUBLISH_WIRE", wire)
			t.Setenv("NATS_PUBLISH_ACK_WAIT", "1s")
			pub := openPublisher(t, url)

			publishConcurrently(t, 32, func(i int) error {
				return PublishJSON(context.Background(), pub, publishBurstSubject, map[string]int{"n": i})
			})

			require.NoError(t, flush(t, pub))
			info, err := js.StreamInfo(BagelDataStream.Name)
			require.NoError(t, err)
			assert.EqualValues(t, 32, info.State.Msgs)
			stored, err := js.GetLastMsg(BagelDataStream.Name, publishBurstSubject)
			require.NoError(t, err)
			assert.Empty(t, stored.Header.Get("Nats-Batch-Id"), "the replayed message must not carry batch framing")
			assert.NotEmpty(t, stored.Header.Get(MessageIDHeader), "the replayed message must keep the fleet identity")
		})
	}
}

func TestFlushReportsAFailedPublicationOnceAndRecoversOnceTheStreamExists(t *testing.T) {
	url := privateBroker(t)
	js := jetStreamOf(t, url)
	pub := openPublisher(t, url)

	require.NoError(t, PublishRaw(context.Background(), pub, publishBurstSubject, []byte(`{}`)))
	failed := flush(t, pub)
	_, err := js.AddStream(&nats.StreamConfig{Name: BagelDataStream.Name, Subjects: BagelDataStream.Subjects})
	require.NoError(t, err)
	require.NoError(t, PublishRaw(context.Background(), pub, publishBurstSubject, []byte(`{}`)))
	recovered := flush(t, pub)

	assert.Error(t, failed, "a publication with no stream behind it must fail the flush")
	assert.NoError(t, recovered, "a healthy window after a reported failure")
}

func BenchmarkPublisherThroughput(b *testing.B) {
	url := brokerURL(b)
	b.Setenv("NATS_JS_DOMAIN", "hub")
	b.Setenv("NATS_LEAF_URL", "")
	b.Setenv("NATS_HUB_URL", "")
	b.Setenv("NATS_HUB_PUBLISH_URL", "")
	if err := EnsureStreams(context.Background(), url, DataStreams, zap.NewNop()); err != nil {
		b.Fatal(err)
	}
	pub, err := NewPublisher(url, zap.NewNop())
	if err != nil {
		b.Fatal(err)
	}
	defer pub.Close()
	payload := make([]byte, 256)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.SetParallelism(256)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := PublishRaw(context.Background(), pub, publishBurstSubject, payload); err != nil {
				b.Error(err)
				return
			}
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := pub.Flush(ctx); err != nil {
		b.Error(err)
	}
	b.StopTimer()
}
