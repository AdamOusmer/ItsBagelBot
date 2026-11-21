// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type capturedVoice struct {
	enabled bool
	jobs    []voiceplan.AlertJob
}

func (v *capturedVoice) Enabled(context.Context, string) (bool, error) { return v.enabled, nil }

func (v *capturedVoice) Publish(_ context.Context, job voiceplan.AlertJob) error {
	v.jobs = append(v.jobs, job)
	return nil
}

type alertVoiceCase struct {
	name       string
	event      eventInput
	masterOff  bool
	offline    bool
	blocked    string
	wantChat   bool
	wantSpeech speechExpectation
}

type speechExpectation struct {
	text  string
	style string
	voice string
}

func TestProcessAlertVoiceOutputs(t *testing.T) {
	const cheerWithViewerText = `{"user_name":"Cool <laugh> Viewer","broadcaster_user_id":"2","bits":100,"message":"Cheer100 <whispers> BibleThump100 {user}!"}`
	cases := []alertVoiceCase{
		{name: "chat only when master off", event: eventInput{"channel.subscribe", subscribeJSON, ""}, masterOff: true, wantChat: true},
		{name: "chat only for follow by default", event: eventInput{"channel.follow", followJSON, ""}, wantChat: true},
		{name: "speech only", event: eventInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`},
			wantSpeech: speechExpectation{"CoolViewer just subscribed! <short pause> Welcome to the crew! 7 months!", "excited, energetic, celebratory", ""}},
		{name: "chat and speech", event: eventInput{"channel.cheer", cheerWithViewerText, `{"cheerTtsVoice":"Zephyr","cheerTtsStyle":"calm","cheerTtsMessage":"{alerts:user}: {bits} <sigh>"}`},
			wantChat: true, wantSpeech: speechExpectation{"Cool laugh Viewer: 100 <sigh> whispers {user}!", "calm, gentle, relaxed", "Zephyr"}},
		{name: "blocked cheer message", blocked: `{"block_terms":"whispers"}`, event: eventInput{"channel.cheer", cheerWithViewerText, ""},
			wantChat: true, wantSpeech: speechExpectation{"Cool laugh Viewer cheered 100 bits!", "playful, light, amused", ""}},
		{name: "blocked resub message", blocked: `{"block_terms":"months"}`, event: eventInput{"channel.subscription.message", resubJSON, ""},
			wantChat: true, wantSpeech: speechExpectation{"CoolViewer just subscribed! <short pause> Welcome to the crew!", "excited, energetic, celebratory", ""}},
		{name: "control characters in template and tokens", event: eventInput{
			"channel.cheer", `{"user_name":"Cool\u0001Viewer","broadcaster_user_id":"2","bits":100,"message":"Great\tstream!"}`,
			`{"cheerTtsMessage":"Hey\n{user}\u202e!\u0000 <short pause> {bits} bits!"}`,
		}, wantChat: true, wantSpeech: speechExpectation{"Hey CoolViewer! <short pause> 100 bits! Great stream!", "playful, light, amused", ""}},
		{name: "template placeholders cannot create speech tags", event: eventInput{
			"channel.follow", `{"user_id":"7","user_login":"laugh","broadcaster_user_id":"2"}`,
			`{"followTts":"true","followTtsMessage":"Welcome <<{user}>> <short pause> today!"}`,
		}, wantChat: true, wantSpeech: speechExpectation{"Welcome laugh <short pause> today!", "warm, friendly, upbeat", ""}},
		{name: "offline only drops speech", event: eventInput{"channel.subscribe", subscribeJSON, ""}, offline: true, wantChat: true},
		{name: "offline allowed with viewer messages disabled", event: eventInput{"channel.subscription.message", resubJSON, `{"ttsLiveOnly":"false","ttsReadMessages":"false","subEnabled":"off"}`},
			offline: true, wantSpeech: speechExpectation{"CoolViewer just subscribed! <short pause> Welcome to the crew!", "excited, energetic, celebratory", ""}},
		{name: "cheer below default minimum", event: eventInput{"channel.cheer", anonCheerJSON, ""}, wantChat: true},
		{name: "cheer exactly at configured minimum", event: eventInput{"channel.cheer", anonCheerJSON, `{"ttsMinBits":"50"}`},
			wantChat: true, wantSpeech: speechExpectation{"An anonymous cheerer cheered 50 bits!", "playful, light, amused", ""}},
		{name: "per-kind opt out", event: eventInput{"channel.raid", raidJSON, `{"raidTts":"false"}`}, wantChat: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t)
		})
	}
}

func (tc alertVoiceCase) run(t *testing.T) {
	t.Helper()
	log := zap.NewNop()
	voice := &capturedVoice{enabled: !tc.masterOff}
	pub := bustest.NewPublisher()
	d := engine.Deps{
		AlertVoice: voice, Live: &fakeLive{live: !tc.offline}, Pub: pub, Log: log,
		Proj: &fakeProj{modules: []projection.ModuleView{
			{Name: "alerts", IsEnabled: true, Configs: []byte(tc.event.cfg)},
			{Name: "automod", IsEnabled: true, Configs: []byte(tc.blocked)},
		}},
	}
	p := engine.NewPipeline(d, engine.NewRegistry(log, Alerts(d), Automod(d)), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)
	data, err := codec.Marshal(lane.Envelope{
		Type: tc.event.event, Lane: "standard", BroadcasterUserID: "2",
		Event: []byte(tc.event.payload), EventID: "test-event",
	})
	require.NoError(t, err)
	require.NoError(t, p.Process(bus.NewMessage("test-event", data)))
	assert.Equal(t, tc.wantChat, len(pub.On("out")) == 1)

	if tc.wantSpeech.text == "" {
		assert.Empty(t, voice.jobs)
		return
	}
	require.Len(t, voice.jobs, 1)
	job := voice.jobs[0]
	assert.Equal(t, tc.wantSpeech, speechExpectation{job.Text, job.Style, job.Voice})
	assert.Equal(t, []string{"2", "test-event"}, []string{job.BroadcasterID, job.EventID})
	assert.WithinDuration(t, time.Now(), job.CreatedAt, time.Minute)
}
