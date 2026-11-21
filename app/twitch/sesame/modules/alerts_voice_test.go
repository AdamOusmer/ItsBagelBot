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
	checks  int
	jobs    []voiceplan.AlertJob
}

func (v *capturedVoice) Enabled(context.Context, string) (bool, error) {
	v.checks++
	return v.enabled, nil
}

func (v *capturedVoice) Publish(_ context.Context, job voiceplan.AlertJob) error {
	v.jobs = append(v.jobs, job)
	return nil
}

type alertVoiceCase struct {
	name       string
	event      eventInput
	locale     string
	masterOff  bool
	offline    bool
	blocked    string
	wantChat   bool
	wantChecks int
	wantSpeech speechExpectation
}

type speechExpectation struct {
	text  string
	style string
	voice string
}

func TestProcessAlertVoiceOutputs(t *testing.T) {
	const cheerWithViewerText = `{"user_name":"Cool <laugh> Viewer","broadcaster_user_id":"2","bits":100,"message":"Cheer100, <whispers> BibleThump100! {user}!"}`
	const anonCheerWithMsg = `{"is_anonymous":true,"broadcaster_user_id":"2","bits":50,"message":"Cheer50 hello stream!"}`
	const cheerOnlyBits = `{"user_name":"CoolViewer","broadcaster_user_id":"2","bits":500,"message":"Cheer250! BibleThump250..."}`
	const resubEmptyMsg = `{"user_id":"7","user_name":"CoolViewer","user_login":"coolviewer","broadcaster_user_id":"2","tier":"1000","message":{"text":"   "}}`
	cases := []alertVoiceCase{
		{name: "chat only when master off", event: eventInput{"channel.subscription.message", resubJSON, ""}, masterOff: true, wantChat: true, wantChecks: 1},
		{name: "follow never speaks even with legacy config", event: eventInput{"channel.follow", followJSON, `{"followTts":"true"}`}, wantChat: true},
		{name: "gift never speaks even with legacy config", event: eventInput{"channel.subscription.gift", giftJSON, `{"giftTts":"true"}`}, wantChat: true},
		{name: "raid never speaks even with legacy config", event: eventInput{"channel.raid", raidJSON, `{"raidTts":"true"}`}, wantChat: true},
		{name: "first-time subscribe never speaks", event: eventInput{"channel.subscribe", subscribeJSON, ""}, wantChat: true},
		{name: "resub speech only", event: eventInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`},
			wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer said: 7 months!", "excited, energetic, celebratory", ""}},
		{name: "resub in french locale", locale: "fr", event: eventInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`},
			wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer a dit : 7 months!", "excited, energetic, celebratory", ""}},
		{name: "resub without message skips speech without checking allowance", event: eventInput{"channel.subscription.message", resubEmptyMsg, ""}, wantChat: true},
		{name: "cheer strips cheermotes and skips bits amount", event: eventInput{"channel.cheer", cheerWithViewerText, `{"cheerTtsVoice":"Zephyr","cheerTtsStyle":"calm"}`},
			wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"Cool laugh Viewer said: whispers {user}!", "calm, gentle, relaxed", "Zephyr"}},
		{name: "cheer with only cheermotes skips speech without checking allowance", event: eventInput{"channel.cheer", cheerOnlyBits, ""}, wantChat: true},
		{name: "blocked cheer message drops speech completely", blocked: `{"block_terms":"whispers"}`, event: eventInput{"channel.cheer", cheerWithViewerText, ""}, wantChat: true},
		{name: "blocked resub message drops speech completely", blocked: `{"block_terms":"months"}`, event: eventInput{"channel.subscription.message", resubJSON, ""}, wantChat: true},
		{name: "control characters in username and message", event: eventInput{
			"channel.cheer", `{"user_name":"Cool\u0001Viewer","broadcaster_user_id":"2","bits":100,"message":"Great\tstream!"}`, "",
		}, wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer said: Great stream!", "playful, light, amused", ""}},
		{name: "offline only drops speech", event: eventInput{"channel.subscription.message", resubJSON, ""}, offline: true, wantChat: true},
		{name: "offline allowed when ttsLiveOnly is false", event: eventInput{"channel.subscription.message", resubJSON, `{"ttsLiveOnly":"false","subEnabled":"off"}`},
			offline: true, wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer said: 7 months!", "excited, energetic, celebratory", ""}},
		{name: "cheer below default minimum", event: eventInput{"channel.cheer", anonCheerWithMsg, ""}, wantChat: true},
		{name: "anonymous cheer at configured minimum skips bits and uses anonymous label", event: eventInput{"channel.cheer", anonCheerWithMsg, `{"ttsMinBits":"50"}`},
			wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"Anonymous said: hello stream!", "playful, light, amused", ""}},
		{name: "anonymous cheer localized in french", locale: "fr", event: eventInput{"channel.cheer", anonCheerWithMsg, `{"ttsMinBits":"50"}`},
			wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"Anonyme a dit : hello stream!", "playful, light, amused", ""}},
		{name: "per-kind opt out", event: eventInput{"channel.cheer", cheerWithViewerText, `{"cheerTts":"false"}`}, wantChat: true},
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
		Proj: &fakeProj{
			user: projection.User{Locale: tc.locale},
			modules: []projection.ModuleView{
				{Name: "alerts", IsEnabled: true, Configs: []byte(tc.event.cfg)},
				{Name: "automod", IsEnabled: true, Configs: []byte(tc.blocked)},
			},
		},
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
	assert.Equal(t, tc.wantChecks, voice.checks)

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

func TestResubVoiceSpeaksEvenAfterSubscribeDedupesChat(t *testing.T) {
	log := zap.NewNop()
	voice := &capturedVoice{enabled: true}
	pub := bustest.NewPublisher()
	cd := &fakeCooldown{allow: []bool{true, false}}
	d := engine.Deps{
		AlertVoice: voice, Live: &fakeLive{live: true}, Pub: pub, Log: log, Cooldown: cd,
		Proj: &fakeProj{modules: []projection.ModuleView{{Name: "alerts", IsEnabled: true}}},
	}
	p := engine.NewPipeline(d, engine.NewRegistry(log, Alerts(d)), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)

	for i, step := range []eventInput{
		{event: "channel.subscribe", payload: subscribeJSON},
		{event: "channel.subscription.message", payload: resubJSON},
	} {
		id := "ev-" + string(rune('1'+i))
		data, err := codec.Marshal(lane.Envelope{
			Type: step.event, Lane: "standard", BroadcasterUserID: "2", Event: []byte(step.payload), EventID: id,
		})
		require.NoError(t, err)
		require.NoError(t, p.Process(bus.NewMessage(id, data)))
	}

	assert.Len(t, pub.On("out"), 1)
	require.Len(t, voice.jobs, 1)
	assert.Equal(t, "CoolViewer said: 7 months!", voice.jobs[0].Text)
}
