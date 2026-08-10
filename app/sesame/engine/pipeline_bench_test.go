package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/pkg/bus"

	"github.com/bytedance/sonic"
)

// benchChatBody builds a representative channel.chat.message envelope once, so the
// benchmarks measure Process, not message construction.
func benchChatBody() []byte {
	body, err := sonic.Marshal(map[string]any{
		"type":                chatType,
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

// silentCore is a core chat handler that emits nothing: the true hot path.
func silentCore() module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return b.Build()
}

// BenchmarkProcessNoOutput is the true hot path: a plain chat line that matches a
// core handler which emits nothing. Everything per-message (envelope, context) is
// pooled, so the only remaining allocations are the JSON decoder's internals.
func BenchmarkProcessNoOutput(b *testing.B) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, silentCore())
	msg := bus.NewMessage("uuid", benchChatBody())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcessChatEmit measures the emit path: a handler produces one chat
// Output that is marshaled and published. Allocation here is expected and is the
// cost the hot path above avoids.
func BenchmarkProcessChatEmit(b *testing.B) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, emitModule("", module.KindCore, "pong"))
	msg := bus.NewMessage("uuid", benchChatBody())

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// TestProcessNoOutputAllocCeiling is a regression guard: the pooled no-output hot
// path must stay at or below a small allocation ceiling (the decoder floor). The
// ceiling is deliberately loose so normal decoder variance does not flake it; it
// exists to catch a structural regression (un-pooling Context/Envelope), not to
// assert an exact count.
func TestProcessNoOutputAllocCeiling(t *testing.T) {
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, silentCore())
	msg := bus.NewMessage("uuid", benchChatBody())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	const ceiling = 12.0
	if avg > ceiling {
		t.Fatalf("no-output hot path allocates %.1f allocs/op, ceiling %.0f: pooling likely regressed", avg, ceiling)
	}
}
