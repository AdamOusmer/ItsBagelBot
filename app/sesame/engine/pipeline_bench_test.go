// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// benchChatBody builds a representative channel.chat.message envelope once, so the
// benchmarks measure Process, not message construction.
func benchChatBody() []byte {
	body, err := codec.Marshal(map[string]any{
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

// gatedSilent is a name-gated (KindOptIn) chat handler that emits nothing. It
// forces NeedsModuleViews(chatType) — the production shape once automod is
// wired — so every line pays the projection read plus a ModuleView-map build.
func gatedSilent() module.Module {
	b := module.NewModule("gated", module.KindOptIn)
	b.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return b.Build()
}

// benchViewsReader carries the minimum row set of a real broadcaster: the
// automod toggle plus the gated module's own enable row.
func benchViewsReader() fakeReader {
	return fakeReader{modules: []projection.ModuleView{
		{Name: automodModuleName, IsEnabled: true},
		{Name: "gated", IsEnabled: true},
	}}
}

// benchMsg wraps the representative chat envelope in one bus message: the
// input every case below feeds Process.
func benchMsg() *bus.Message {
	return bus.NewMessage("uuid", benchChatBody())
}

// benchPipeline wires a pipeline over pub's publisher, reader and mods, paired
// with its input message, so each benchmark (and alloc-ceiling test) states
// only what differs from the others.
func benchPipeline(tb testing.TB, reader projection.Reader, mods ...module.Module) (*Pipeline, *bus.Message) {
	tb.Helper()
	return newPipelineWith(&fakePublisher{}, reader, mods...), benchMsg()
}

// benchProcess is the standard measurement loop: report allocs, start the
// clock, process msg until the benchmark is done.
func benchProcess(b *testing.B, p *Pipeline, msg *bus.Message) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkProcessNoOutput is the true hot path: a plain chat line that matches a
// core handler which emits nothing. Everything per-message (envelope, context) is
// pooled, so the only remaining allocations are the JSON decoder's internals.
func BenchmarkProcessNoOutput(b *testing.B) {
	p, msg := benchPipeline(b, fakeReader{}, silentCore())
	benchProcess(b, p, msg)
}

// BenchmarkProcessNoOutputWithViews is the automod-wired shape of the hot path:
// same silent chat line, but NeedsModuleViews(chat) is true so Process fetches
// rows and builds the ModuleView map. It measures the pooled-map reuse; before
// pooling this rebuilt a fresh map on every line.
func BenchmarkProcessNoOutputWithViews(b *testing.B) {
	p, msg := benchPipeline(b, benchViewsReader(), gatedSilent())
	benchProcess(b, p, msg)
}

// BenchmarkProcessChatEmit measures the emit path: a handler produces one chat
// Output that is marshaled and published. Allocation here is expected and is the
// cost the hot path above avoids.
func BenchmarkProcessChatEmit(b *testing.B) {
	p, msg := benchPipeline(b, fakeReader{}, emitModule("", module.KindCore, "pong"))
	benchProcess(b, p, msg)
}

// TestProcessNoOutputAllocCeiling is a regression guard: the pooled no-output hot
// path must stay at or below a small allocation ceiling (the decoder floor). The
// ceiling is deliberately loose so normal decoder variance does not flake it; it
// exists to catch a structural regression (un-pooling Context/Envelope), not to
// assert an exact count.
func TestProcessNoOutputAllocCeiling(t *testing.T) {
	p, msg := benchPipeline(t, fakeReader{}, silentCore())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	if avg > allocCeiling {
		t.Fatalf("no-output hot path allocates %.1f allocs/op, ceiling %.0f: pooling likely regressed", avg, allocCeiling)
	}
}

// TestProcessWithViewsAllocCeiling guards the automod-wired shape of the hot
// path: the ModuleView map must come from the pool (cleared on release), so
// only the projection read's own rows may allocate above the decoder floor.
// A jump means the map is being rebuilt per line or retained past its message.
func TestProcessWithViewsAllocCeiling(t *testing.T) {
	p, msg := benchPipeline(t, benchViewsReader(), gatedSilent())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	if avg > allocViewsCeiling {
		t.Fatalf("views-path no-output hot path allocates %.1f allocs/op, ceiling %.0f: view-map pooling likely regressed", avg, allocViewsCeiling)
	}
}

// BenchmarkProcessNoOutputWithRecording is the hot path with the nuke sweep
// memory armed (Deps.Nuke set): every plain chat line additionally parses and
// buffers into the sweep memory. Production's ValkeyRecent flushes that
// buffer off-path every 50ms; this measures only the on-path stage against
// the in-memory double. Measured on M1 Pro (2026-08-24): 767 -> 813 ns/op
// (+6%), 687 -> 703 B/op (+2.3%), allocs 12 -> 12.
func BenchmarkProcessNoOutputWithRecording(b *testing.B) {
	n := NewNuke(NewRecentLog(), 0, zap.NewNop())
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: &fakePublisher{}, Log: zap.NewNop(), Nuke: n}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), silentCore()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj,
	})
	benchProcess(b, p, benchMsg())
}
