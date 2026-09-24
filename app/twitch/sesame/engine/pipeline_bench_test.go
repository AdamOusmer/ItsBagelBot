// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

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

func silentCore() module.Module {
	b := module.NewModule("", module.KindCore)
	b.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return b.Build()
}

func gatedSilent() module.Module {
	b := module.NewModule("gated", module.KindOptIn)
	b.On(chatType, func(context.Context, *module.Context, module.Emit) error { return nil })
	return b.Build()
}

func benchViewsReader() fakeReader {
	return fakeReader{modules: projection.ModuleMap([]projection.ModuleView{
		{Name: automodModuleName, IsEnabled: true},
		{Name: "gated", IsEnabled: true},
	})}
}

func benchMsg() *bus.Message {
	return bus.NewMessage("uuid", benchChatBody())
}

func benchPipeline(tb testing.TB, reader projection.Reader, mods ...module.Module) (*Pipeline, *bus.Message) {
	tb.Helper()
	return newPipelineWith(&fakePublisher{}, reader, mods...), benchMsg()
}

func benchProcess(b *testing.B, p *Pipeline, msg *bus.Message) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.Process(msg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkProcessNoOutput(b *testing.B) {
	p, msg := benchPipeline(b, fakeReader{}, silentCore())
	benchProcess(b, p, msg)
}

func BenchmarkProcessNoOutputWithViews(b *testing.B) {
	p, msg := benchPipeline(b, benchViewsReader(), gatedSilent())
	benchProcess(b, p, msg)
}

func BenchmarkProcessChatEmit(b *testing.B) {
	p, msg := benchPipeline(b, fakeReader{}, emitModule("", module.KindCore, "pong"))
	benchProcess(b, p, msg)
}

func TestProcessNoOutputAllocCeiling(t *testing.T) {
	p, msg := benchPipeline(t, fakeReader{}, silentCore())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	if avg > allocCeiling {
		t.Fatalf("no-output hot path allocates %.1f allocs/op, ceiling %.0f: pooling likely regressed", avg, allocCeiling)
	}
}

func TestProcessWithViewsAllocCeiling(t *testing.T) {
	p, msg := benchPipeline(t, benchViewsReader(), gatedSilent())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	if avg > allocViewsCeiling {
		t.Fatalf("views-path no-output hot path allocates %.1f allocs/op, ceiling %.0f: view-map pooling likely regressed", avg, allocViewsCeiling)
	}
}

func BenchmarkProcessNoOutputWithRecording(b *testing.B) {
	n := NewNuke(NewRecentLog(), 0, zap.NewNop())
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{},
		Pub: &fakePublisher{}, Log: zap.NewNop(), Nuke: n}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), silentCore()), Config{
		OutgressPremium: premiumSubj, OutgressStandard: standardSubj,
	})
	benchProcess(b, p, benchMsg())
}

func benchAutomodConfigBlob() codec.RawMessage {
	blob, err := codec.Marshal(map[string]string{
		"level":       "strict",
		"harassment":  "on",
		"clips_only":  "off",
		"block_terms": "kappa scam, free follows, discord.gg/x, buy viewers, cheap prime",
		"allow_terms": "gg, poggers, kekw",
	})
	if err != nil {
		panic(err)
	}
	return blob
}

func benchConfiguredViewsReader() fakeReader {
	return fakeReader{modules: projection.ModuleMap([]projection.ModuleView{
		{Name: automodModuleName, IsEnabled: true, Configs: benchAutomodConfigBlob()},
		{Name: "gated", IsEnabled: true},
	})}
}

func BenchmarkProcessNoOutputWithAutomodConfig(b *testing.B) {
	p, msg := benchPipeline(b, benchConfiguredViewsReader(), gatedSilent())
	benchProcess(b, p, msg)
}

func TestProcessWithAutomodConfigAllocCeiling(t *testing.T) {
	p, msg := benchPipeline(t, benchConfiguredViewsReader(), gatedSilent())

	avg := testing.AllocsPerRun(500, func() {
		_ = p.Process(msg)
	})

	if avg > allocViewsCeiling {
		t.Fatalf("configured-automod hot path allocates %.1f allocs/op, ceiling %.0f: the config cache is missing", avg, allocViewsCeiling)
	}
}
