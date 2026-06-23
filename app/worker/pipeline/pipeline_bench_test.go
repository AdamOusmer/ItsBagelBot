package pipeline

import (
	"testing"

	"ItsBagelBot/app/worker/module"
	"ItsBagelBot/internal/domain/outgress"

	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/bytedance/sonic"
)

// benchChatBody builds a representative channel.chat.message envelope once, so the
// benchmarks measure Process, not message construction.
func benchChatBody() []byte {
	body, err := sonic.Marshal(map[string]any{
		"type":                "channel.chat.message",
		"lane":                "standard",
		"broadcaster_user_id": "123",
		"chatter_user_id":     "999",
		"text":                "hello chat how is everyone",
	})
	if err != nil {
		panic(err)
	}
	return body
}

// BenchmarkProcessNoOutput is the true hot path: a plain chat line that matches a
// core module which emits nothing. Everything per-message (envelope, context) is
// pooled, so the only remaining allocations are the JSON decoder's internals. A
// regression that drops the pooling shows up here as a jump in allocs/op.
func BenchmarkProcessNoOutput(b *testing.B) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, baseStub{name: ""})
	msg := message.NewMessage("uuid", benchChatBody()) // reused: Process only reads it

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcessChatEmit measures the emit path: a module produces one chat
// Output that is marshaled and published. Allocation here is expected (it only
// runs when a module actually emits) and is the cost the hot path above avoids.
func BenchmarkProcessChatEmit(b *testing.B) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, chatStub{baseStub{name: ""}, "pong"})
	msg := message.NewMessage("uuid", benchChatBody())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// TestProcessNoOutputAllocCeiling is a regression guard: the pooled no-output hot
// path must stay at or below a small allocation ceiling (the decoder floor). If
// the pools regress, allocs/op jumps well past this and the test fails. The
// ceiling is deliberately loose so normal decoder variance does not flake it; it
// exists to catch a structural regression (e.g. un-pooling Context/Envelope or
// re-introducing the output slice), not to assert an exact count.
func TestProcessNoOutputAllocCeiling(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, baseStub{name: ""})
	msg := message.NewMessage("uuid", benchChatBody())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	const ceiling = 12.0 // measured floor is the sonic decode; pools add ~0
	if avg > ceiling {
		t.Fatalf("no-output hot path allocates %.1f allocs/op, ceiling %.0f: pooling likely regressed", avg, ceiling)
	}
	_ = outgress.TypeChat // keep outgress import meaningful if assertions change
}

var _ = module.GetOutput // ensure module pool API stays referenced from bench file
