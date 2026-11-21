// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	automationrpc "ItsBagelBot/internal/domain/rpc/automation"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type speechSink struct {
	enabled bool
	jobs    []voiceplan.AlertJob
}

func (s *speechSink) Enabled(context.Context, string) (bool, error) { return s.enabled, nil }

func (s *speechSink) Publish(_ context.Context, job voiceplan.AlertJob) error {
	s.jobs = append(s.jobs, job)
	return nil
}

type speakCase struct {
	name      string
	premium   bool
	noMinutes bool
	text      string
	want      speakOutcome
}

type speakOutcome struct {
	status string
	detail string
	text   string
}

func TestRecipeSpeakStaysPremiumOnPublicRecipes(t *testing.T) {
	cases := []speakCase{
		{name: "premium channel speaks the expanded text", premium: true, text: "hello {user} <3", want: speakOutcome{automationrpc.StatusOK, "", "hello alice 3"}},
		{name: "free channel is limited", text: "hello", want: speakOutcome{automationrpc.StatusLimited, "premium_required", ""}},
		{name: "no minutes is limited", premium: true, noMinutes: true, text: "hello", want: speakOutcome{automationrpc.StatusLimited, "voice_unavailable", ""}},
		{name: "nothing to say is skipped", premium: true, text: "<> !!", want: speakOutcome{automationrpc.StatusSkipped, "speech_blocked", ""}},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc speakCase) run(t *testing.T) {
	reader := &premiumReader{fakeReader: fakeReader{modules: map[string]projection.ModuleView{AutomationsModuleName: on()}}}
	reader.premium.Store(tc.premium)
	sink := &speechSink{enabled: !tc.noMinutes}
	runs := &autoRunLog{}
	recipe := withActions(command("!say", map[string]any{}), []automation.Action{act("speak", obj{"text": tc.text, "style": "calm", "voice": "Puck"})})
	hook := &AutomationHook{}
	d := Deps{
		Proj: reader, Live: liveAlways{}, Cooldown: NoopCooldown{}, Pub: &fakePublisher{}, Log: zap.NewNop(), AlertVoice: sink,
		Automations: AutomationDeps{Source: &autoSource{list: []automation.Automation{recipe}}, Runs: runs},
	}
	p := NewPipeline(d, NewRegistry(zap.NewNop(), autoModule(hook)), Config{OutgressStandard: standardSubj})
	hook.Wire(p)
	process(t, p, chatEnv("!say", ""))

	records := runs.records()
	require.Len(t, records, 1)
	require.Len(t, records[0].Actions, 1)
	got := speakOutcome{status: records[0].Actions[0].Status, detail: records[0].Actions[0].Detail}
	if len(sink.jobs) == 1 {
		got.text = sink.jobs[0].Text
		assert.Equal(t, [3]any{voiceplan.SpeechRecipe, "Puck", "calm, gentle, relaxed"}, [3]any{sink.jobs[0].Kind, sink.jobs[0].Voice, sink.jobs[0].Style})
	}
	assert.Equal(t, tc.want, got)
	assert.LessOrEqual(t, len(sink.jobs), 1)
}
