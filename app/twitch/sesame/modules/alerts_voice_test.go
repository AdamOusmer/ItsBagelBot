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
	"go.uber.org/zap/zaptest/observer"
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

const alertNotificationID = "replay/abc+=="
const alertNotificationDigest = "52321f6ed12e5e17e61656395631563a"

type alertVoiceCase struct {
	name       string
	event      eventInput
	locale     string
	masterOff  bool
	chatOff    bool
	trial      bool
	missingID  bool
	wantSkip   speechSkipReason
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
		{name: "chat only without voice allowance", event: eventInput{"channel.subscription.message", resubJSON, ""}, masterOff: true, wantSkip: speechUnavailable, wantChat: true, wantChecks: 1},
		{name: "follow never speaks even with legacy config", event: eventInput{"channel.follow", followJSON, `{"followTts":"true"}`}, wantChat: true},
		{name: "gift never speaks even with legacy config", event: eventInput{"channel.subscription.gift", giftJSON, `{"giftTts":"true"}`}, wantChat: true},
		{name: "raid never speaks even with legacy config", event: eventInput{"channel.raid", raidJSON, `{"raidTts":"true"}`}, wantChat: true},
		{name: "first-time subscribe never speaks", event: eventInput{"channel.subscribe", subscribeJSON, ""}, wantChat: true},
		{name: "resub speech only", event: eventInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`},
			wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer said: 7 months!", "excited, energetic, celebratory", ""}},
		{name: "resub in french locale", locale: "fr", event: eventInput{"channel.subscription.message", resubJSON, `{"subEnabled":"off"}`},
			wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer a dit : 7 months!", "excited, energetic, celebratory", ""}},
		{name: "resub without message skips speech without checking allowance", wantSkip: speechEmpty, event: eventInput{"channel.subscription.message", resubEmptyMsg, ""}, wantChat: true},
		{name: "cheer strips cheermotes and skips bits amount", event: eventInput{"channel.cheer", cheerWithViewerText, `{"__rev":10,"cheerTtsVoice":"Zephyr","cheerTtsStyle":"calm"}`},
			wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"Cool laugh Viewer said: whispers {user}!", "calm, gentle, relaxed", "Zephyr"}},
		{name: "cheer with only cheermotes skips speech without checking allowance", wantSkip: speechEmpty, event: eventInput{"channel.cheer", cheerOnlyBits, ""}, wantChat: true},
		{name: "blocked cheer message drops speech completely", wantSkip: speechBlocked, blocked: `{"block_terms":"whispers"}`, event: eventInput{"channel.cheer", cheerWithViewerText, ""}, wantChat: true},
		{name: "blocked resub message drops speech completely", wantSkip: speechBlocked, blocked: `{"block_terms":"months"}`, event: eventInput{"channel.subscription.message", resubJSON, ""}, wantChat: true},
		{name: "control characters in username and message", event: eventInput{
			"channel.cheer", `{"user_name":"Cool\u0001Viewer","broadcaster_user_id":"2","bits":100,"message":"Great\tstream!"}`, "",
		}, wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer said: Great stream!", "playful, light, amused", ""}},
		{name: "offline only drops speech", wantSkip: speechOffline, event: eventInput{"channel.subscription.message", resubJSON, ""}, offline: true, wantChat: true},
		{name: "offline allowed when ttsLiveOnly is false", event: eventInput{"channel.subscription.message", resubJSON, `{"ttsLiveOnly":"false","subEnabled":"off"}`},
			offline: true, wantChecks: 1, wantSpeech: speechExpectation{"CoolViewer said: 7 months!", "excited, energetic, celebratory", ""}},
		{name: "cheer below default minimum", wantSkip: speechMinBits, event: eventInput{"channel.cheer", anonCheerWithMsg, ""}, wantChat: true},
		{name: "anonymous cheer at configured minimum skips bits and uses anonymous label", event: eventInput{"channel.cheer", anonCheerWithMsg, `{"ttsMinBits":"50"}`},
			wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"Anonymous said: hello stream!", "playful, light, amused", ""}},
		{name: "anonymous cheer localized in french", locale: "fr", event: eventInput{"channel.cheer", anonCheerWithMsg, `{"ttsMinBits":"50"}`},
			wantChat: true, wantChecks: 1, wantSpeech: speechExpectation{"Anonyme a dit : hello stream!", "playful, light, amused", ""}},
		{name: "trial stays silent", trial: true, wantSkip: speechTrial, event: eventInput{"channel.subscription.message", resubJSON, ""}, wantChat: true},
		{name: "per-kind opt out", wantSkip: speechToggleOff, event: eventInput{"channel.cheer", cheerWithViewerText, `{"cheerTts":"false"}`}, wantChat: true},
		{name: "opt out is read from a disabled row", chatOff: true, wantSkip: speechToggleOff, event: eventInput{"channel.subscription.message", resubJSON, `{"subTts":"false"}`}},
		{name: "missing identity skips speech", missingID: true, wantSkip: speechNoIdentity, event: eventInput{"channel.subscription.message", resubJSON, ""}, wantChat: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func (tc alertVoiceCase) run(t *testing.T) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core)
	voice := &capturedVoice{enabled: !tc.masterOff}
	pub := bustest.NewPublisher()
	d := engine.Deps{
		AlertVoice: voice, Live: &fakeLive{live: !tc.offline}, Pub: pub, Log: log,
		Proj: &fakeProj{
			user: projection.User{Locale: tc.locale},
			modules: []projection.ModuleView{
				{Name: "alerts", IsEnabled: !tc.chatOff, Configs: []byte(tc.event.cfg)},
				{Name: "automod", IsEnabled: true, Configs: []byte(tc.blocked)},
			},
		},
	}
	p := engine.NewPipeline(d, alertTestRegistry(d), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)
	env := lane.Envelope{
		Type: tc.event.event, Lane: "standard",
		Event: []byte(tc.event.payload), MsgID: alertNotificationID,
	}
	if tc.trial {
		env.Origin = "trial"
	}
	if tc.missingID {
		env.MsgID = ""
	}
	processAlertEnvelope(t, p, env)
	assert.Equal(t, tc.wantChat, len(pub.On("out")) == 1)
	assert.Equal(t, tc.wantChecks, voice.checks)
	tc.assertSpeech(t, voice, logs)
}

func (tc alertVoiceCase) assertSpeech(t *testing.T, v *capturedVoice, logs *observer.ObservedLogs) {
	t.Helper()
	skips := logs.FilterMessage("voice alert skipped").All()
	if tc.wantSkip == "" {
		assert.Empty(t, skips)
	} else {
		require.Len(t, skips, 1)
		fields := skips[0].ContextMap()
		assert.Equal(t, []any{string(tc.wantSkip), uint64(2)}, []any{fields["reason"], fields["broadcaster_id"]})
	}
	if tc.wantSpeech.text == "" {
		assert.Empty(t, v.jobs)
		return
	}
	require.Len(t, v.jobs, 1)
	job := v.jobs[0]
	assert.Equal(t, []any{tc.wantSpeech, "2", alertNotificationDigest},
		[]any{speechExpectation{job.Text, job.Style, job.Voice}, job.BroadcasterID, job.EventID})
	assert.WithinDuration(t, time.Now(), job.CreatedAt, time.Minute)
	assert.Equal(t, 1, logs.FilterMessage("voice job published").Len())
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
	p := engine.NewPipeline(d, alertTestRegistry(d), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)

	for i, step := range []eventInput{
		{event: "channel.subscribe", payload: subscribeJSON},
		{event: "channel.subscription.message", payload: resubJSON},
	} {
		id := "ev-" + string(rune('1'+i))
		processAlertEnvelope(t, p, lane.Envelope{
			Type: step.event, Lane: "standard", Event: []byte(step.payload), MsgID: id,
		})
	}

	assert.Len(t, pub.On("out"), 1)
	require.Len(t, voice.jobs, 1)
	assert.Equal(t, "CoolViewer said: 7 months!", voice.jobs[0].Text)
}

func processAlertEnvelope(t *testing.T, p *engine.Pipeline, env lane.Envelope) {
	t.Helper()
	require.NoError(t, p.Process(alertEnvelopeMessage(t, env)))
}

func alertEnvelopeMessage(t *testing.T, env lane.Envelope) *bus.Message {
	t.Helper()
	data, err := codec.Marshal(env)
	require.NoError(t, err)
	return bus.NewMessage(env.MsgID, data)
}

func alertTestRegistry(d engine.Deps) *engine.Registry {
	return engine.NewRegistry(d.Log, Alerts(d), AlertVoice(d), Automod(d))
}
