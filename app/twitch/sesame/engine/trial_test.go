// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/module"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func trialCommandModule(ran *int) module.Module {
	m := module.NewModule("trial", module.KindCore).Trial()
	m.Command("discord").Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		*ran++
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "trial discord"})
		return nil
	})
	return m.Build()
}

func plainBakedCommand(ran *int) module.Module {
	m := module.NewModule("", module.KindCore)
	m.Command("cmd").Everyone().Run(func(context.Context, *module.Context, string, module.Emit) error {
		*ran++
		return nil
	})
	return m.Build()
}

type trialLine struct {
	text   string
	event  string
	cohort int
	real   bool
	gen    int
}

func (l trialLine) message(t *testing.T) *bus.Message {
	t.Helper()
	fields := map[string]any{"text": l.text, "chatter_user_id": "999"}
	if l.event != "" {
		fields["type"] = l.event
	}
	if !l.real {
		fields["origin"], fields["trial_generation"] = "trial", l.gen
	}
	if l.cohort > 0 {
		return cohortMsg(t, l.cohort, l.text, map[string]any{"origin": "trial", "trial_generation": l.gen})
	}
	return envelopeMsg(t, "trial-test", fields)
}

type trialMark struct {
	origin string
	gen    uint64
}

func trialMarks(pub *fakePublisher) []trialMark {
	var marks []trialMark
	for _, c := range pub.snapshot() {
		marks = append(marks, trialMark{c.msg.Origin, c.msg.TrialGeneration})
	}
	return marks
}

func TestTrialMarksEveryLineOfABatchedReply(t *testing.T) {
	m := module.NewModule("trial", module.KindCore).Trial()
	m.Command("discord").Run(func(_ context.Context, c *module.Context, _ string, emit module.Emit) error {
		emit(&module.Output{Type: outgress.TypeChat, BroadcasterID: c.Env.BroadcasterUserID, Text: "line one\nline two"})
		return nil
	})
	pub := &fakePublisher{}

	require.NoError(t, newPipelineWith(pub, fakeReader{}, m.Build()).Process(trialLine{text: "!discord", gen: 9}.message(t)))

	require.Len(t, pub.snapshot(), 1)
	batch := pub.snapshot()[0].msg
	var items outgress.Batch
	require.NoError(t, codec.Unmarshal(batch.Payload, &items))
	require.Len(t, items.Items, 2)
	assert.Equal(t, outgress.TypeBatch, batch.Type)
	assert.Equal(t, trialMark{"trial", 9}, trialMark{batch.Origin, batch.TrialGeneration})
	for _, item := range items.Items {
		assert.Equal(t, trialMark{"trial", 9}, trialMark{item.Origin, item.TrialGeneration})
	}
}

func TestTrialOriginDecidesWhatRuns(t *testing.T) {
	cases := []struct {
		name      string
		modules   func(ran *int) []module.Module
		reader    fakeReader
		line      trialLine
		wantRan   int
		wantTexts []string
		wantMarks []trialMark
	}{
		{
			name: "a trial runs core handlers, skips opt-in modules and marks its output",
			modules: func(*int) []module.Module {
				return []module.Module{emitModule("", module.KindCore, "core proposal"), emitModule("opt", module.KindOptIn, "must not run")}
			},
			reader:    fakeReader{modules: map[string]projection.ModuleView{"opt": {Name: "opt", IsEnabled: true}}},
			line:      trialLine{text: "hello", gen: 7},
			wantTexts: []string{"core proposal"},
			wantMarks: []trialMark{{"trial", 7}},
		},
		{
			name:    "a trial never runs a mutating baked command",
			modules: func(ran *int) []module.Module { return []module.Module{plainBakedCommand(ran)} },
			line:    trialLine{text: "!cmd add foo bar", gen: 2},
		},
		{
			name:      "a trial module's command runs for trial origin and is marked",
			modules:   func(ran *int) []module.Module { return []module.Module{trialCommandModule(ran)} },
			line:      trialLine{text: "!discord", gen: 4},
			wantRan:   1,
			wantTexts: []string{"trial discord"},
			wantMarks: []trialMark{{"trial", 4}},
		},
		{
			name:    "a trial module never runs for a real channel",
			modules: func(ran *int) []module.Module { return []module.Module{trialCommandModule(ran)} },
			reader: fakeReader{
				cmd:      projection.Command{Name: "discord", Response: "the streamer's own discord", IsActive: true},
				cmdFound: true,
			},
			line:      trialLine{text: "!discord", real: true},
			wantTexts: []string{"the streamer's own discord"},
			wantMarks: []trialMark{{}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ran int
			pub := &fakePublisher{}

			require.NoError(t, newPipelineWith(pub, tc.reader, tc.modules(&ran)...).Process(tc.line.message(t)))

			assert.Equal(t, tc.wantRan, ran)
			assert.Equal(t, tc.wantTexts, pub.chatTexts(t))
			assert.Equal(t, tc.wantMarks, trialMarks(pub))
		})
	}
}

func TestTrialNeverReadsTheAccount(t *testing.T) {
	for _, text := range []string{"hello", "!ping"} {
		t.Run(text, func(t *testing.T) {
			reader := &countingChannelReader{countingReader: countingReader{fakeReader: fakeReader{user: projection.User{Locale: "fr"}}}}
			pub := &fakePublisher{}
			p := newPipelineWith(pub, reader, localePing(), emitModule("voice", module.KindDefault, "fixed"))

			require.NoError(t, p.Process(trialLine{text: text, gen: 7}.message(t)))

			assert.Zero(t, reader.userReads)
			assert.Zero(t, reader.loads)
			for _, mark := range trialMarks(pub) {
				assert.Equal(t, trialMark{"trial", 7}, mark)
			}
			if text == "!ping" {
				assert.Equal(t, "locale=", chatMessageText(t, pub.snapshot()[0].msg))
			}
		})
	}
}

func trialCounts(bumper *fakeBumper) map[string]int64 {
	counts := bumper.channelTotals()[123]
	if counts["trial_latency_ns_total"] > 0 {
		counts["trial_latency_ns_total"] = 1
	}
	return counts
}

func TestTrialCountersReachTheBumperUnderTrialNames(t *testing.T) {
	failure := errors.New("projection unavailable")
	cases := []struct {
		name    string
		modules func(ran *int) []module.Module
		reader  fakeReader
		line    trialLine
		wantErr error
		want    map[string]int64
	}{
		{
			name:    "TestTrialCountersGoToTheLoyaltyReporterUnderTrialNames",
			modules: func(ran *int) []module.Module { return []module.Module{trialCommandModule(ran)} },
			line:    trialLine{text: "!discord", gen: 4},
			want: map[string]int64{
				"trial_decoded": 1, "trial_processed": 1, "trial_answered": 1, "trial_blocked": 1,
				"trial_latency_ns_samples": 1, "trial_latency_ns_total": 1,
			},
		},
		{
			name: "TestTrialProcessedCountsEveryMessageInASquashedCohort",
			line: trialLine{text: "hello", cohort: 3, gen: 4},
			want: map[string]int64{
				"trial_decoded": 3, "trial_processed": 3, "trial_latency_ns_samples": 1, "trial_latency_ns_total": 1,
			},
		},
		{
			name:    "a failed cold cohort counts weighted failures and retries",
			modules: func(*int) []module.Module { return []module.Module{emitModule("named", module.KindDefault, "hello")} },
			reader:  fakeReader{modErr: failure},
			line:    trialLine{text: "hello", cohort: 3, gen: 4},
			wantErr: failure,
			want: map[string]int64{
				"trial_decoded": 3, "trial_failed": 3, "trial_retried": 3,
				"trial_latency_ns_samples": 1, "trial_latency_ns_total": 1,
			},
		},
		{
			name: "an unhandled trial event counts only decoded",
			line: trialLine{text: "hello", event: "unhandled", gen: 4},
			want: map[string]int64{"trial_decoded": 1},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var ran int
			bumper := &fakeBumper{}
			var mods []module.Module
			if tc.modules != nil {
				mods = tc.modules(&ran)
			}
			p := statsPipeline(bumper, tc.reader, mods...)

			err := p.Process(tc.line.message(t))
			p.Close()

			assert.ErrorIs(t, err, tc.wantErr)
			assert.Equal(t, tc.want, trialCounts(bumper))
		})
	}
}

type slowTrialBumper struct{ fakeBumper }

func (b *slowTrialBumper) BumpChannel(id uint64, name string, amount int64) {
	if name == "trial_processed" {
		time.Sleep(100 * time.Millisecond)
	}
	b.fakeBumper.BumpChannel(id, name, amount)
}

func TestTrialDurationCapturedBeforeOutcomeReporting(t *testing.T) {
	bumper := &slowTrialBumper{}
	d := Deps{Proj: fakeReader{}, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), Stats: bumper}
	p := NewPipeline(d, NewRegistry(zap.NewNop()), Config{OutgressPremium: premiumSubj, OutgressStandard: standardSubj})
	t.Cleanup(p.Close)
	require.NoError(t, p.Process(chatMsg(t, "standard", "warm codec")))

	require.NoError(t, p.Process(trialLine{text: "hello", gen: 4}.message(t)))

	totals := bumper.channelTotals()[123]
	assert.Less(t, totals["trial_latency_ns_total"], int64(75*time.Millisecond))
	assert.Equal(t, int64(1), totals["trial_processed"])
}
