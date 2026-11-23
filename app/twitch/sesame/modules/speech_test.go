// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/twitch/sesame/engine"
	"ItsBagelBot/internal/domain/event/lane"
	"ItsBagelBot/internal/domain/outgress"
	"ItsBagelBot/internal/domain/voiceplan"
	"ItsBagelBot/internal/projection"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const speechRewardID = "reward-1"

type speechCase struct {
	name       string
	env        lane.Envelope
	ttsRow     string
	rewardRow  string
	alertsRow  string
	blocked    string
	automodOff bool
	free       bool
	noMinutes  bool
	offline    bool
	customTTS  bool
	wantJob    speechExpectation
	wantKind   voiceplan.AlertKind
	wantChat   string
	wantStatus string
}

func TestPremiumSpeechSurfaces(t *testing.T) {
	const speakReward = `{"rewards":[{"id":"reward-1","action":"speak","voice":"Puck","style":"calm"}]}`
	chat := ttsChat("!tts hello <b>stream</b>")
	cases := []speechCase{
		{name: "tts speaks the viewer with the name prefix", env: chat, ttsRow: `{"style":"hype"}`,
			wantKind: voiceplan.SpeechChat, wantJob: speechExpectation{"Viewer said: hello b stream /b", "excited, energetic, celebratory", ""}},
		{name: "tts trims to max characters without the prefix", env: chat, ttsRow: `{"maxChars":"5","prefixName":"false","voice":"Zephyr"}`,
			wantKind: voiceplan.SpeechChat, wantJob: speechExpectation{"hello", "warm, friendly, upbeat", "Zephyr"}},
		{name: "tts is off until the module row is enabled", env: chat},
		{name: "tts off leaves a custom command with the same name", env: chat, customTTS: true, wantChat: "custom tts"},
		{name: "tts stays locked for free channels", env: chat, ttsRow: "{}", free: true},
		{name: "tts locked for free channels yields to a custom command", env: chat, ttsRow: "{}", free: true, customTTS: true, wantChat: "custom tts"},
		{name: "tts blocked text replies when asked", env: chat, ttsRow: `{"replyDenied":"true"}`, blocked: `{"block_terms":"stream"}`,
			wantChat: "That message can't be read aloud."},
		{name: "tts blocked viewer name replies when asked", env: chat, ttsRow: `{"replyDenied":"true"}`, blocked: `{"block_terms":"viewer"}`,
			wantChat: "That message can't be read aloud."},
		{name: "tts floor applies with automod disabled", env: ttsChat("!tts please kys"), ttsRow: `{"replyDenied":"true"}`, automodOff: true,
			wantChat: "That message can't be read aloud."},
		{name: "tts allow term cannot unblock the floor", env: ttsChat("!tts please kys"), ttsRow: `{"replyDenied":"true"}`, blocked: `{"allow_terms":"kys"}`,
			wantChat: "That message can't be read aloud."},
		{name: "tts banter and game violence pass", env: ttsChat("!tts headshot, you are trash, gg ez"), ttsRow: "{}", automodOff: true,
			wantKind: voiceplan.SpeechChat, wantJob: speechExpectation{"Viewer said: headshot, you are trash, gg ez", "warm, friendly, upbeat", ""}},
		{name: "tts strips links before speaking", env: ttsChat("!tts look https://example.com/x now"), ttsRow: "{}",
			wantKind: voiceplan.SpeechChat, wantJob: speechExpectation{"Viewer said: look now", "warm, friendly, upbeat", ""}},
		{name: "speak reward refunds a blocked name", env: speakRedemption("great stream"), rewardRow: speakReward, blocked: `{"block_terms":"viewer"}`,
			wantStatus: outgress.RedemptionCanceled},
		{name: "speak reward refunds floor text", env: speakRedemption("kys"), rewardRow: speakReward, automodOff: true,
			wantStatus: outgress.RedemptionCanceled},
		{name: "tts without minutes stays silent by default", env: chat, ttsRow: "{}", noMinutes: true},
		{name: "speak reward queues the redemption text and fulfills", env: speakRedemption("great stream"), rewardRow: speakReward,
			wantKind: voiceplan.SpeechReward, wantJob: speechExpectation{"Viewer said: great stream", "calm, gentle, relaxed", "Puck"},
			wantStatus: outgress.RedemptionFulfilled},
		{name: "speak reward refunds free channels", env: speakRedemption("great stream"), rewardRow: speakReward, free: true,
			wantStatus: outgress.RedemptionCanceled},
		{name: "speak reward refunds offline under live only", env: speakRedemption("great stream"), rewardRow: speakReward, offline: true,
			wantStatus: outgress.RedemptionCanceled},
		{name: "speak reward follows the alerts live only switch", env: speakRedemption("great stream"), rewardRow: speakReward, offline: true,
			alertsRow: `{"__rev":3,"ttsLiveOnly":"false"}`, wantKind: voiceplan.SpeechReward,
			wantJob: speechExpectation{"Viewer said: great stream", "calm, gentle, relaxed", "Puck"}, wantStatus: outgress.RedemptionFulfilled},
		{name: "speak reward refunds empty text", env: speakRedemption(" ?! "), rewardRow: speakReward, wantStatus: outgress.RedemptionCanceled},
		{name: "speak reward refunds without minutes", env: speakRedemption("great stream"), rewardRow: speakReward, noMinutes: true,
			wantStatus: outgress.RedemptionCanceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, tc.run)
	}
}

func ttsChat(text string) lane.Envelope {
	return lane.Envelope{
		Type: "channel.chat.message", Lane: "standard", MsgID: "chat-1", BroadcasterUserID: "2", BroadcasterUserLogin: "streamer",
		ChatterUserID: "7", ChatterUserLogin: "viewer", ChatterUserName: "Viewer", Text: text,
	}
}

func speakRedemption(input string) lane.Envelope {
	event, _ := codec.Marshal(map[string]any{
		"id": "redeem-1", "broadcaster_user_id": "2", "user_id": "7", "user_login": "viewer", "user_name": "Viewer",
		"user_input": input, "reward": map[string]any{"id": speechRewardID, "title": "Speak", "cost": 100},
	})
	return lane.Envelope{Type: engine.RedemptionAddType, Lane: "standard", MsgID: "redeem-1", EventID: "redeem-1", BroadcasterUserID: "2", Event: event}
}

func (tc speechCase) views() []projection.ModuleView {
	views := []projection.ModuleView{
		{Name: "alerts", IsEnabled: true, Configs: []byte(tc.alertsRow)},
		{Name: "automod", IsEnabled: !tc.automodOff, Configs: []byte(tc.blocked)},
		{Name: channelPointsModuleName, IsEnabled: tc.rewardRow != "", Configs: []byte(tc.rewardRow)},
	}
	if tc.ttsRow != "" {
		views = append(views, projection.ModuleView{Name: voiceplan.ChatCommand, IsEnabled: true, Configs: []byte(tc.ttsRow)})
	}
	return views
}

func (tc speechCase) user() projection.User {
	if tc.free {
		return projection.User{IsActive: true, Status: "free"}
	}
	return projection.User{IsActive: true, Status: "premium"}
}

func (tc speechCase) commands() map[string]projection.Command {
	if !tc.customTTS {
		return nil
	}
	return map[string]projection.Command{"tts": {Name: "tts", IsActive: true, Response: "custom tts", Perm: "everyone"}}
}

func (tc speechCase) run(t *testing.T) {
	voice := &capturedVoice{enabled: !tc.noMinutes}
	pub := bustest.NewPublisher()
	d := engine.Deps{
		AlertVoice: voice, Live: &fakeLive{live: !tc.offline}, Pub: pub, Log: zap.NewNop(), Cooldown: engine.NoopCooldown{},
		Proj: &fakeProj{user: tc.user(), modules: tc.views(), commands: tc.commands()},
	}
	p := engine.NewPipeline(d, engine.NewRegistry(d.Log, TTS(d), ChannelPoints(d), Automod(d)), engine.Config{OutgressStandard: "out"})
	t.Cleanup(p.Close)
	processAlertEnvelope(t, p, tc.env)
	tc.assertJob(t, voice)
	assert.Equal(t, tc.wantChat, spokenReply(t, pub.On("out")))
	assert.Equal(t, tc.wantStatus, redemptionStatus(t, pub.On("out")))
}

func (tc speechCase) assertJob(t *testing.T, voice *capturedVoice) {
	t.Helper()
	if tc.wantJob.text == "" {
		assert.Empty(t, voice.jobs)
		return
	}
	require.Len(t, voice.jobs, 1)
	job := voice.jobs[0]
	assert.Equal(t, tc.wantJob, speechExpectation{job.Text, job.Style, job.Voice})
	assert.Equal(t, [2]any{tc.wantKind, true}, [2]any{job.Kind, job.Kind.Valid()})
	assert.Regexp(t, "^[0-9a-f]{32}$", job.EventID)
}

func outgressOf(t *testing.T, outs []*bus.Message, msgType string) (outgress.Message, bool) {
	t.Helper()
	for _, raw := range outs {
		var out outgress.Message
		require.NoError(t, codec.Unmarshal(raw.Payload, &out))
		if out.Type == msgType {
			return out, true
		}
	}
	return outgress.Message{}, false
}

func spokenReply(t *testing.T, outs []*bus.Message) string {
	t.Helper()
	out, ok := outgressOf(t, outs, outgress.TypeChat)
	if !ok {
		return ""
	}
	var payload struct{ Message string }
	require.NoError(t, codec.Unmarshal(out.Payload, &payload))
	return payload.Message
}

func redemptionStatus(t *testing.T, outs []*bus.Message) string {
	t.Helper()
	out, _ := outgressOf(t, outs, outgress.TypeRedemptionUpdate)
	return out.Status
}
