// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	appAuth = "Bearer app-token"
	botAuth = "Bearer bot-token"

	sentReply = `{"data":[{"message_id":"abc-123","is_sent":true}]}`
	usersBody = `{"data":[{"id":"12826","login":"somelogin"}]}`
)

func call(method, target, auth, body string) recordedRequest {
	path, query, _ := strings.Cut(target, "?")
	return recordedRequest{Method: method, Path: path, Query: query, Auth: auth, Body: body}
}

type actionCase struct {
	name   string
	msg    testMessage
	script []scriptedResponse
	want   []recordedRequest
}

const mod = "moderator_id=" + testBot

func runActionCases(t *testing.T, tests []actionCase) {
	t.Helper()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: tt.script}

			require.NoError(t, tt.msg.send(pipelineWorker(t, rt)))

			assert.Equal(t, tt.want, rt.recorded())
		})
	}
}

func TestChatActionsPostAsTheApp(t *testing.T) {
	runActionCases(t, []actionCase{
		{
			name: "chat posts as the app with the bot as sender",
			msg:  testMessage{Type: "chat", Payload: `{"broadcaster_id":"44322889","message":"hello"}`},
			want: []recordedRequest{call("POST", "/helix/chat/messages", appAuth, `{"broadcaster_id":"44322889","message":"hello","sender_id":"987654"}`)},
		},
		{
			name: "chat honours an explicit sender",
			msg:  testMessage{Type: "chat", Sender: "555", Payload: `{"message":"hello"}`},
			want: []recordedRequest{call("POST", "/helix/chat/messages", appAuth, `{"message":"hello","sender_id":"555"}`)},
		},
		{
			name: "announce posts as the app and defaults the color",
			msg:  testMessage{Type: "announce", Payload: `{"message":"gm"}`},
			want: []recordedRequest{call("POST", "/helix/chat/announcements?broadcaster_id=44322889&"+mod, appAuth, `{"message":"gm","color":"primary"}`)},
		},
		{
			name: "announce keeps an explicit color",
			msg:  testMessage{Type: "announce", Color: "blue", Payload: `{"message":"gm"}`},
			want: []recordedRequest{call("POST", "/helix/chat/announcements?broadcaster_id=44322889&"+mod, appAuth, `{"message":"gm","color":"blue"}`)},
		},
		{
			name:   "shoutout resolves the target login then posts as the app",
			msg:    testMessage{Type: "shoutout", To: "somelogin"},
			script: []scriptedResponse{{status: 200, body: usersBody}},
			want: []recordedRequest{
				call("GET", "/helix/users?login=somelogin", appAuth, ""),
				call("POST", "/helix/chat/shoutouts?from_broadcaster_id=44322889&to_broadcaster_id=12826&"+mod, appAuth, ""),
			},
		},
		{
			name:   "shoutout escapes every id it puts in the query",
			msg:    testMessage{Type: "shoutout", Broadcaster: "a b", Sender: "e?f", To: "somelogin"},
			script: []scriptedResponse{{status: 200, body: `{"data":[{"id":"c&d","login":"somelogin"}]}`}},
			want: []recordedRequest{
				call("GET", "/helix/users?login=somelogin", appAuth, ""),
				call("POST", "/helix/chat/shoutouts?from_broadcaster_id=a+b&to_broadcaster_id=c%26d&moderator_id=e%3Ff", appAuth, ""),
			},
		},
		{
			name:   "pin sends the chat line then pins it without limiting the pin lifetime",
			msg:    testMessage{Type: "pin", Payload: `{"message":"rules"}`},
			script: []scriptedResponse{{status: 200, body: sentReply}},
			want: []recordedRequest{
				call("POST", "/helix/chat/messages", appAuth, `{"message":"rules","sender_id":"987654"}`),
				call("PUT", "/helix/chat/pins?broadcaster_id=44322889&"+mod+"&message_id=abc-123", appAuth, ""),
			},
		},
		{
			name:   "pin escapes the moderator and message ids",
			msg:    testMessage{Type: "pin", Sender: "c&d", Payload: `{"message":"rules"}`},
			script: []scriptedResponse{{status: 200, body: `{"data":[{"message_id":"e?f","is_sent":true}]}`}},
			want: []recordedRequest{
				call("POST", "/helix/chat/messages", appAuth, `{"message":"rules","sender_id":"c&d"}`),
				call("PUT", "/helix/chat/pins?broadcaster_id=44322889&moderator_id=c%26d&message_id=e%3Ff", appAuth, ""),
			},
		},
	})
}

func TestModerationActionsCallTwitchAsTheBot(t *testing.T) {
	runActionCases(t, []actionCase{
		{
			name: "delete removes the message as the bot",
			msg:  testMessage{Type: "delete", MsgID: "abc-123"},
			want: []recordedRequest{call("DELETE", "/helix/moderation/chat?broadcaster_id=44322889&"+mod+"&message_id=abc-123", botAuth, "")},
		},
		{
			name: "delete escapes every id it puts in the query",
			msg:  testMessage{Type: "delete", Broadcaster: "a b", Sender: "c&d", MsgID: "e?f"},
			want: []recordedRequest{call("DELETE", "/helix/moderation/chat?broadcaster_id=a+b&moderator_id=c%26d&message_id=e%3Ff", botAuth, "")},
		},
		{
			name: "shield mode puts the payload as the bot",
			msg:  testMessage{Type: "shield_mode", Payload: `{"is_active":true}`},
			want: []recordedRequest{call("PUT", "/helix/moderation/shield_mode?broadcaster_id=44322889&"+mod, botAuth, `{"is_active":true}`)},
		},
		{
			name: "warn posts the payload as the bot",
			msg:  testMessage{Type: "warn", Payload: `{"data":{"user_id":"1","reason":"spam"}}`},
			want: []recordedRequest{call("POST", "/helix/moderation/warnings?broadcaster_id=44322889&"+mod, botAuth, `{"data":{"user_id":"1","reason":"spam"}}`)},
		},
		{
			name: "ban posts the payload as the bot",
			msg:  testMessage{Type: "ban", Payload: `{"data":{"user_id":"1"}}`},
			want: []recordedRequest{call("POST", "/helix/moderation/bans?broadcaster_id=44322889&"+mod, botAuth, `{"data":{"user_id":"1"}}`)},
		},
		{
			name: "timeout shares the ban route",
			msg:  testMessage{Type: "timeout", Payload: `{"data":{"user_id":"1","duration":60}}`},
			want: []recordedRequest{call("POST", "/helix/moderation/bans?broadcaster_id=44322889&"+mod, botAuth, `{"data":{"user_id":"1","duration":60}}`)},
		},
	})
}

func TestBodiesAreCompletedForTheirPayloadShape(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    string
	}{
		{"an empty object gets the bare field", `{}`, `{"sender_id":"987654"}`},
		{"a non-empty object appends with a comma", `{"a":"b"}`, `{"a":"b","sender_id":"987654"}`},
		{"a field already present is left alone", `{"sender_id":"already"}`, `{"sender_id":"already"}`},
		{"whitespace before the closing brace is kept", "{\"a\":\"b\"  \n\t}", "{\"a\":\"b\"  \n\t,\"sender_id\":\"987654\"}"},
		{"whitespace inside an empty object stays bare", `{   }`, `{   "sender_id":"987654"}`},
		{"an absent payload synthesizes an object", "", `{"sender_id":"987654"}`},
		{"a top-level array is returned unchanged", `["a","b"]`, `["a","b"]`},
		{"a bare scalar is returned unchanged", `"hi"`, `"hi"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{}

			require.NoError(t, testMessage{Type: "chat", Payload: tt.payload}.send(pipelineWorker(t, rt)))

			require.Len(t, rt.recorded(), 1)
			assert.Equal(t, tt.want, rt.recorded()[0].Body)
		})
	}
}

func TestGeneralHelixRequestsChargeTheTokenSpecificBucket(t *testing.T) {
	tests := []struct {
		name string
		msg  testMessage
		want string
	}{
		{"the app token", testMessage{Type: "api", Method: "GET", As: "app", Endpoint: "/helix/users"}, "ratelimit:helix:app"},
		{"the bot user token", testMessage{Type: "api", Method: "POST", As: "bot", Endpoint: "/helix/moderation/bans"}, "ratelimit:helix:user:bot"},
		{"an auto-routed bot user token", testMessage{Type: "api", Method: "GET", Endpoint: "/helix/moderation/channels?first=100"}, "ratelimit:helix:user:bot"},
		{
			"a broadcaster user token",
			testMessage{Type: "api", Method: "POST", As: "broadcaster", Endpoint: "/helix/clips"},
			"ratelimit:helix:user:" + testBroadcaster,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limiter := &scriptedLimiter{}

			require.NoError(t, tt.msg.send(pipelineWorker(t, &scriptedTransport{}, withLimiter(limiter))))

			assert.Equal(t, []string{tt.want}, limiter.keys())
		})
	}
}

func TestPinOnlyFollowsAChatLineTwitchSent(t *testing.T) {
	tests := []struct {
		name         string
		reply        string
		wantRequests int
	}{
		{"pins a message twitch sent", sentReply, 2},
		{"skips the pin when twitch dropped the message", `{"data":[{"message_id":"","is_sent":false,"drop_reason":{"code":"msg_rejected"}}]}`, 1},
		{"skips the pin when twitch answered with no data", `{"data":[]}`, 1},
		{"skips the pin when the reply is malformed", `{`, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: []scriptedResponse{{status: 200, body: tt.reply}}}

			require.NoError(t, testMessage{Type: "pin", Payload: `{"message":"rules"}`}.send(pipelineWorker(t, rt)))

			assert.Len(t, rt.recorded(), tt.wantRequests)
		})
	}
}

type rehostTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (r rehostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme, req.URL.Host = r.target.Scheme, r.target.Host
	return r.base.RoundTrip(req)
}

func TestResponsesAreDrainedSoHTTP11ConnectionsAreReused(t *testing.T) {
	var connections atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 1024))
	}))
	server.EnableHTTP2 = false
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	require.NoError(t, err)
	w := pipelineWorker(t, rehostTransport{base: server.Client().Transport, target: target})

	for range 2 {
		require.NoError(t, testMessage{Type: "delete", MsgID: "m"}.send(w))
	}

	assert.EqualValues(t, 1, connections.Load())
}

type endlessBody struct {
	remaining int
	read      int
}

func (b *endlessBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.remaining)
	b.remaining -= n
	b.read += n
	return n, nil
}

func (*endlessBody) Close() error { return nil }

type oversizedTransport struct{ body *endlessBody }

func (o oversizedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: o.body}, nil
}

func TestResponseDrainIsBounded(t *testing.T) {
	body := &endlessBody{remaining: maxResponseDrain * 2}

	require.NoError(t, testMessage{Type: "delete", MsgID: "m"}.send(pipelineWorker(t, oversizedTransport{body})))

	assert.Equal(t, maxResponseDrain+1, body.read)
}
