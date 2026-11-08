// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chatSettingsCall(body string) recordedRequest {
	return call("PATCH", "/helix/chat/settings?broadcaster_id="+testBroadcaster+"&"+mod, botAuth, body)
}

func settingsMessage(payload string) testMessage {
	return testMessage{Type: "chat_settings", Payload: payload}
}

func TestChatSettingsChangeOneModeAsTheBot(t *testing.T) {
	runActionCases(t, []actionCase{
		{name: "slow mode with a wait", msg: settingsMessage(`{"mode":"slow","enabled":true,"duration":45}`),
			want: []recordedRequest{chatSettingsCall(`{"slow_mode":true,"slow_mode_wait_time":45}`)}},
		{name: "slow mode defaults to thirty seconds", msg: settingsMessage(`{"mode":"slow","enabled":true}`),
			want: []recordedRequest{chatSettingsCall(`{"slow_mode":true,"slow_mode_wait_time":30}`)}},
		{name: "slow mode wait is capped", msg: settingsMessage(`{"mode":"slow","enabled":true,"duration":500}`),
			want: []recordedRequest{chatSettingsCall(`{"slow_mode":true,"slow_mode_wait_time":120}`)}},
		{name: "slow mode off sends no wait", msg: settingsMessage(`{"mode":"slow","enabled":false,"duration":45}`),
			want: []recordedRequest{chatSettingsCall(`{"slow_mode":false}`)}},
		{name: "followers only for ten minutes", msg: settingsMessage(`{"mode":"follower","enabled":true,"duration":10}`),
			want: []recordedRequest{chatSettingsCall(`{"follower_mode":true,"follower_mode_duration":10}`)}},
		{name: "followers only from any follow", msg: settingsMessage(`{"mode":"follower","enabled":true}`),
			want: []recordedRequest{chatSettingsCall(`{"follower_mode":true,"follower_mode_duration":0}`)}},
		{name: "emote only on", msg: settingsMessage(`{"mode":"emote","enabled":true}`),
			want: []recordedRequest{chatSettingsCall(`{"emote_mode":true}`)}},
		{name: "subscribers only off", msg: settingsMessage(`{"mode":"subscriber","enabled":false}`),
			want: []recordedRequest{chatSettingsCall(`{"subscriber_mode":false}`)}},
		{name: "unique chat on with a confirmation", msg: testMessage{Type: "chat_settings", Payload: `{"mode":"unique","enabled":true}`},
			want: []recordedRequest{chatSettingsCall(`{"unique_chat_mode":true}`)}},
		{name: "an unknown mode costs no call", msg: settingsMessage(`{"mode":"rainbow","enabled":true}`)},
	})
}

func TestChannelControlActions(t *testing.T) {
	runActionCases(t, []actionCase{
		{
			name: "clear chat deletes every message as the bot",
			msg:  testMessage{Type: "clear_chat", Payload: `{}`},
			want: []recordedRequest{call("DELETE", "/helix/moderation/chat?broadcaster_id="+testBroadcaster+"&"+mod, botAuth, "")},
		},
		{
			name: "snooze pushes the next ad as the broadcaster",
			msg:  testMessage{Type: "ad_snooze", Payload: `{}`},
			want: []recordedRequest{call("POST", "/helix/channels/ads/schedule/snooze?broadcaster_id="+testBroadcaster, broadcasterAuth, "")},
		},
		{
			name:   "snooze with none left is dropped",
			msg:    testMessage{Type: "ad_snooze", Payload: `{}`},
			script: []scriptedResponse{{status: 429, body: `{"message":"no snoozes"}`}},
			want:   []recordedRequest{call("POST", "/helix/channels/ads/schedule/snooze?broadcaster_id="+testBroadcaster, broadcasterAuth, "")},
		},
		{
			name:   "whisper resolves the recipient and sends from the bot",
			msg:    testMessage{Type: "whisper", To: "bob", Payload: `{"message":"hi there"}`},
			script: []scriptedResponse{bobLookup},
			want: []recordedRequest{userLookup("bob"),
				call("POST", "/helix/whispers?from_user_id="+testBot+"&to_user_id=777", botAuth, `{"message":"hi there"}`)},
		},
		{
			name: "an empty whisper costs no call",
			msg:  testMessage{Type: "whisper", To: "bob", Payload: `{"message":""}`},
		},
	})
}

func TestWhisperIsTruncatedToTheNewRecipientLimit(t *testing.T) {
	rt := &scriptedTransport{responses: []scriptedResponse{bobLookup}}

	require.NoError(t, testMessage{Type: "whisper", To: "bob", Payload: `{"message":"` + strings.Repeat("é", 600) + `"}`}.send(pipelineWorker(t, rt)))

	got := rt.recorded()
	require.Len(t, got, 2)
	assert.Equal(t, `{"message":"`+strings.Repeat("é", 500)+`"}`, got[1].Body)
}
