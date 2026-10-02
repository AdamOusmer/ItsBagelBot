// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const channelBody = `{"data":[{"title":"Ranked grind","game_id":"33214","game_name":"Fortnite","tags":[]}]}`

func chatLine(text string) recordedRequest {
	return call("POST", "/helix/chat/messages", appAuth, `{"broadcaster_id":"`+testBroadcaster+`","message":"`+text+`","sender_id":"`+testBot+`"}`)
}

func TestChannelUpdateGetsAndSetsStreamDetails(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		script  []scriptedResponse
		want    []recordedRequest
	}{
		{
			name:    "replies with the current title",
			payload: `{"field":"title","locale":"en","user":"alice"}`,
			script:  []scriptedResponse{{status: 200, body: channelBody}},
			want:    []recordedRequest{call("GET", "/helix/channels"+"?"+"broadcaster_id="+testBroadcaster, appAuth, ""), chatLine("The current title is: Ranked grind")},
		},
		{
			name:    "replies that no tags are set",
			payload: `{"field":"tags","locale":"en","user":"alice"}`,
			script:  []scriptedResponse{{status: 200, body: channelBody}},
			want:    []recordedRequest{call("GET", "/helix/channels"+"?"+"broadcaster_id="+testBroadcaster, appAuth, ""), chatLine("No tags set.")},
		},
		{
			name:    "sets the title as the broadcaster and confirms in chat",
			payload: `{"field":"title","value":" New title ","locale":"en","user":"alice"}`,
			want: []recordedRequest{
				call("PATCH", "/helix/channels"+"?"+"broadcaster_id="+testBroadcaster, "Bearer broadcaster-token", `{"title":"New title"}`),
				chatLine("@alice updated the title to: New title"),
			},
		},
		{
			name:    "resolves the game category before setting it",
			payload: `{"field":"game","value":"fort","locale":"en","user":"alice"}`,
			script:  []scriptedResponse{{status: 200, body: `{"data":[{"id":"33214","name":"Fortnite"}]}`}},
			want: []recordedRequest{
				call("GET", "/helix/search/categories"+"?"+"first=1&query=fort", appAuth, ""),
				call("PATCH", "/helix/channels"+"?"+"broadcaster_id="+testBroadcaster, "Bearer broadcaster-token", `{"game_id":"33214"}`),
				chatLine("@alice updated the game to: Fortnite"),
			},
		},
		{
			name:    "splits and trims the tags it sets",
			payload: `{"field":"tags","value":"English,  family friendly,","locale":"en","user":"alice"}`,
			want: []recordedRequest{
				call("PATCH", "/helix/channels"+"?"+"broadcaster_id="+testBroadcaster, "Bearer broadcaster-token", `{"tags":["English","family friendly"]}`),
				chatLine("@alice updated tags to: English, family friendly"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: tt.script}

			require.NoError(t, testMessage{Type: "channel_update", Payload: tt.payload}.send(pipelineWorker(t, rt)))

			assert.Equal(t, tt.want, rt.recorded())
		})
	}
}

func TestChannelUpdateDropsRejectionsAndRetriesTransientFailures(t *testing.T) {
	tests := []struct {
		status    int
		wantRetry bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusTooManyRequests, true},
		{http.StatusBadGateway, true},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			rejection := scriptedResponse{status: tt.status, body: `{"message":"no"}`}
			rt := &scriptedTransport{responses: []scriptedResponse{rejection, rejection}}

			err := testMessage{Type: "channel_update", Payload: `{"field":"title","locale":"en","user":"alice"}`}.send(pipelineWorker(t, rt))

			assert.Equal(t, tt.wantRetry, err != nil)
		})
	}
}

func TestStreamEditorJobsConfirmInChat(t *testing.T) {
	tests := []struct {
		name   string
		msg    testMessage
		script scriptedResponse
		want   []recordedRequest
	}{
		{
			name:   "creates a marker as the broadcaster",
			msg:    testMessage{Type: "stream_marker", Payload: `{"description":"boss","locale":"en","user":"alice"}`},
			script: scriptedResponse{status: 200, body: `{"data":[{"id":"1"}]}`},
			want: []recordedRequest{
				call("POST", "/helix/streams/markers", "Bearer broadcaster-token", `{"user_id":"`+testBroadcaster+`","description":"boss"}`),
				chatLine("@alice dropped a stream marker."),
			},
		},
		{
			name:   "starts a commercial as the broadcaster",
			msg:    testMessage{Type: "commercial", Payload: `{"length":30,"locale":"en","user":"alice"}`},
			script: scriptedResponse{status: 200, body: `{"data":[{"length":30}]}`},
			want: []recordedRequest{
				call("POST", "/helix/channels/commercial", "Bearer broadcaster-token", `{"broadcaster_id":"`+testBroadcaster+`","length":30}`),
				chatLine("@alice started a 30s commercial."),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: []scriptedResponse{tt.script}}

			require.NoError(t, tt.msg.send(pipelineWorker(t, rt)))

			assert.Equal(t, tt.want, rt.recorded())
		})
	}
}
