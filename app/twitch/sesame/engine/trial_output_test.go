package engine

import (
	"context"
	"testing"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestMarkTrialOutputMarksBatchChildren(t *testing.T) {
	batch, err := codec.Marshal(outgress.Batch{ID: "batch-1", Items: []outgress.Message{{Type: outgress.TypeChat, BroadcasterID: "42"}, {Type: outgress.TypeClip, BroadcasterID: "42"}}})
	if err != nil {
		t.Fatal(err)
	}
	output := outgress.Message{Type: outgress.TypeBatch, BroadcasterID: "42", Payload: batch}
	markTrialOutput(&output, 9)
	if output.Origin != "trial" || output.TrialGeneration != 9 {
		t.Fatalf("outer provenance: %+v", output)
	}
	var got outgress.Batch
	if err := codec.Unmarshal(output.Payload, &got); err != nil {
		t.Fatal(err)
	}
	for _, item := range got.Items {
		if item.Origin != "trial" || item.TrialGeneration != 9 {
			t.Fatalf("child provenance: %+v", item)
		}
	}
}

func TestTrialRunsCoreStageButDisablesOptInAndMarksOutput(t *testing.T) {
	pub := &fakePublisher{}
	reader := fakeReader{modules: map[string]projection.ModuleView{"opt": {Name: "opt", IsEnabled: true}}}
	p := newPipelineWith(pub, reader,
		emitModule("", module.KindCore, "core proposal"),
		emitModule("opt", module.KindOptIn, "must not run"))
	body, err := codec.Marshal(map[string]any{
		"type": chatType, "lane": "standard", "broadcaster_user_id": "123",
		"chatter_user_id": "999", "text": "hello", "origin": "trial", "trial_generation": 7,
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("trial-1", body)))
	require.Len(t, pub.got, 1)
	require.Equal(t, "trial", pub.got[0].msg.Origin)
	require.Equal(t, uint64(7), pub.got[0].msg.TrialGeneration)
}

func TestTrialSkipsMutatingBakedCommand(t *testing.T) {
	called := false
	m := module.NewModule("", module.KindCore)
	m.Command("cmd").Everyone().Run(func(context.Context, *module.Context, string, module.Emit) error { called = true; return nil })
	p := newPipelineWith(&fakePublisher{}, fakeReader{}, m.Build())
	body, err := codec.Marshal(map[string]any{
		"type": chatType, "lane": "standard", "broadcaster_user_id": "123",
		"chatter_user_id": "999", "text": "!cmd add foo bar", "origin": "trial", "trial_generation": 2,
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("trial-2", body)))
	require.False(t, called)
}

func trialCommandModule(called *int) module.Module {
	m := module.NewModule("trial", module.KindCore).Trial()
	m.Command("discord").Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		*called++
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "trial discord"})
		return nil
	})
	return m.Build()
}

func chatEnvelope(t *testing.T, text string, trial bool) *bus.Message {
	t.Helper()
	fields := map[string]any{
		"type": chatType, "lane": "standard", "broadcaster_user_id": "123",
		"chatter_user_id": "999", "text": text,
	}
	if trial {
		fields["origin"] = "trial"
		fields["trial_generation"] = 4
	}
	body, err := codec.Marshal(fields)
	require.NoError(t, err)
	return bus.NewMessage("trial-module", body)
}

func TestTrialModuleCommandRunsForTrialOriginAndIsMarked(t *testing.T) {
	called := 0
	pub := &fakePublisher{}
	p := newPipelineWith(pub, fakeReader{}, trialCommandModule(&called))
	require.NoError(t, p.Process(chatEnvelope(t, "!discord", true)))
	require.Equal(t, 1, called)
	require.Len(t, pub.got, 1)
	require.Equal(t, "trial", pub.got[0].msg.Origin)
	require.Equal(t, uint64(4), pub.got[0].msg.TrialGeneration)
}

func TestTrialModuleNeverRunsForARealChannel(t *testing.T) {
	called := 0
	pub := &fakePublisher{}
	reader := fakeReader{cmd: projection.Command{Name: "discord", Response: "the streamer's own discord", IsActive: true}, cmdFound: true}
	p := newPipelineWith(pub, reader, trialCommandModule(&called))
	require.NoError(t, p.Process(chatEnvelope(t, "!discord", false)))
	require.Zero(t, called)
	require.Len(t, pub.got, 1)
	require.Empty(t, pub.got[0].msg.Origin)
}

type recordingBumper struct{ got map[string]int64 }

func (b *recordingBumper) BumpBot(string, int64) {}
func (b *recordingBumper) BumpChannel(id uint64, name string, delta int64) {
	if id == 123 {
		b.got[name] += delta
	}
}

func TestTrialCountersGoToTheLoyaltyReporterUnderTrialNames(t *testing.T) {
	called := 0
	bumps := &recordingBumper{got: map[string]int64{}}
	reg := NewRegistry(zap.NewNop(), trialCommandModule(&called))
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Stats: bumps}
	p := NewPipeline(d, reg, Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	require.NoError(t, p.Process(chatEnvelope(t, "!discord", true)))
	for _, name := range []string{"trial_decoded", "trial_processed", "trial_answered", "trial_blocked", "trial_latency_samples"} {
		require.Equal(t, int64(1), bumps.got[name], name)
	}
	require.Zero(t, bumps.got["messages_processed"], "trial traffic must not count as the channel's own")
}
