package bus

import (
	"context"
	"testing"

	"github.com/nats-io/nats.go"
)

var benchPayload = make([]byte, 865)

func BenchmarkStreamForTopic(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := streamForTopic("twitch.ingress.event.premium"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGateAcquireReleaseSerial(b *testing.B) {
	pool := newRoutinePool([]int{25}, 64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if !pool.acquire(0) {
			b.Fatal("acquire failed")
		}
		pool.release(0)
	}
}

func BenchmarkGateAcquireReleaseContended(b *testing.B) {
	pool := newRoutinePool([]int{25}, 64)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if !pool.acquire(0) {
				b.Fatal("acquire failed")
			}
			pool.release(0)
		}
	})
}

func BenchmarkPublishMessageHeaderBuild(b *testing.B) {
	cmd := publishCommand{
		ctx:     context.Background(),
		topic:   "twitch.ingress.event.premium",
		msgID:   nextNUID(),
		payload: benchPayload,
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = publishMessage(cmd)
	}
}

func BenchmarkFleetMetadata(b *testing.B) {
	headers := nats.Header{
		"Bagelbot-Message-Id":     {"01ABCDEF0123456789ABCDEF01"},
		"_watermill_message_uuid": {"01ABCDEF0123456789ABCDEF01"},
		"Traceparent":             {"00-abcdef0123456789abcdef0123456789-0123456789abcdef-01"},
		"Content-Type":            {"application/json"},
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := fleetMetadata(headers); err != nil {
			b.Fatal(err)
		}
	}
}
