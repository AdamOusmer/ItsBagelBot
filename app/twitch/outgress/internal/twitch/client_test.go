// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func respond(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func helixClient(app, bot *Source, handler roundTripFunc) *Client {
	c := NewClient("client", app, bot, nil)
	c.SetTransport(handler)
	return c
}

func respondWith(status int, body string) roundTripFunc {
	return func(*http.Request) (*http.Response, error) { return respond(status, body), nil }
}

func TestIsMissingScope(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"twitch error", `{"status":401,"message":"Missing scope: user:read:moderated_channels"}`, true},
		{"case insensitive", `{"message":"MISSING SCOPE"}`, true},
		{"expired token", `{"status":401,"message":"Invalid OAuth token"}`, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isMissingScope([]byte(tc.body)); got != tc.want {
				t.Fatalf("isMissingScope() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHTTPClientPoolMatchesWorkerConcurrency(t *testing.T) {
	client := newHTTPClient()
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport type = %T", client.Transport)
	}
	if transport.MaxIdleConns != maxIdleConnections {
		t.Fatalf("MaxIdleConns = %d, want %d", transport.MaxIdleConns, maxIdleConnections)
	}
	if transport.MaxIdleConnsPerHost != maxIdleConnectionsPerHost {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", transport.MaxIdleConnsPerHost, maxIdleConnectionsPerHost)
	}
	if !transport.ForceAttemptHTTP2 {
		t.Fatal("HTTP/2 is not enabled")
	}
	client.CloseIdleConnections()
}

func TestWarmupMintsTokenAndPrimesAppConnection(t *testing.T) {
	refreshes := 0
	requests := 0
	source := &Source{refresh: func(context.Context) (string, time.Duration, error) {
		refreshes++
		return "warm-token", time.Hour, nil
	}}
	client := &Client{
		clientID: "client",
		app:      source,
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if req.Method != http.MethodGet || req.URL.Path != "/helix/streams" || req.URL.Query().Get("first") != "1" {
				t.Fatalf("warmup request = %s %s", req.Method, req.URL.String())
			}
			if got := req.Header.Get("Authorization"); got != "Bearer warm-token" {
				t.Fatalf("authorization = %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	if err := client.Warmup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests != 1 || refreshes != 1 {
		t.Fatalf("requests/refreshes = %d/%d, want 1/1", requests, refreshes)
	}
}

func TestWarmupRejectsTwitchFailure(t *testing.T) {
	source := &Source{token: "cached", expires: time.Now().Add(time.Hour)}
	client := &Client{
		clientID: "client",
		app:      source,
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusServiceUnavailable,
				Body:       io.NopCloser(strings.NewReader(`{"message":"unavailable"}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	if err := client.Warmup(context.Background()); err == nil {
		t.Fatal("Warmup() accepted a Twitch failure")
	}
}

func TestCloudBotChatAutoRoutingUsesAppToken(t *testing.T) {
	app := &Source{}
	user := &Source{}
	client := &Client{app: app, user: user}

	for _, endpoint := range []string{
		"/helix/chat/messages",
		"/helix/chat/announcements?broadcaster_id=1&moderator_id=2",
		"/helix/chat/shoutouts?from_broadcaster_id=1&to_broadcaster_id=2&moderator_id=3",
		"/helix/chat/pins?broadcaster_id=1&moderator_id=2&message_id=abc",
	} {
		if got := client.sourceFor(endpoint); got != app {
			t.Errorf("sourceFor(%q) = user token, want app token", endpoint)
		}
	}
}

func TestExecuteRoutesAutoIdentityByEndpoint(t *testing.T) {
	tests := []struct {
		endpoint string
		wantAuth string
	}{
		{"/helix/chat/messages", "Bearer app"},
		{"/helix/chat/announcements?broadcaster_id=1&moderator_id=2", "Bearer app"},
		{"/helix/chat/shoutouts?from_broadcaster_id=1&to_broadcaster_id=2&moderator_id=3", "Bearer app"},
		{"/helix/chat/pins?broadcaster_id=1&moderator_id=2&message_id=abc", "Bearer app"},
		{"/helix/users?login=a", "Bearer app"},
		{"/helix/moderation/channels?user_id=1", "Bearer bot"},
		{"/helix/chat/chatters?broadcaster_id=1", "Bearer bot"},
		{"/helix/channels/followers?broadcaster_id=1", "Bearer bot"},
	}
	for _, tt := range tests {
		t.Run(tt.endpoint, func(t *testing.T) {
			var gotAuth string
			c := helixClient(NewStaticTokenSource("app"), NewStaticTokenSource("bot"), func(req *http.Request) (*http.Response, error) {
				gotAuth = req.Header.Get("Authorization")
				return respond(http.StatusOK, `{}`), nil
			})

			res, err := c.Execute(context.Background(), http.MethodGet, tt.endpoint, nil)
			require.NoError(t, err)
			res.Body.Close()

			assert.Equal(t, tt.wantAuth, gotAuth)
		})
	}
}

func TestMissingScope401DoesNotRefreshToken(t *testing.T) {
	refreshes := 0
	bot := &Source{token: "still-valid", expires: time.Now().Add(time.Hour), refresh: func(context.Context) (string, time.Duration, error) {
		refreshes++
		return "new-token", time.Hour, nil
	}}
	wantBody := `{"status":401,"message":"Missing scope: user:read:moderated_channels"}`
	c := helixClient(NewStaticTokenSource("app"), bot, respondWith(http.StatusUnauthorized, wantBody))

	res, err := c.Execute(context.Background(), http.MethodGet, "/helix/moderation/channels", nil)
	require.NoError(t, err)
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	assert.Equal(t, wantBody, string(body))
	assert.Zero(t, refreshes, "a missing scope is not an expired token")

	token, err := bot.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "new-token", token, "the cached token was dropped so re-authorization is picked up")
}

func TestInvalidToken401StillRefreshesAndRetries(t *testing.T) {
	refreshes, requests := 0, 0
	bot := &Source{token: "expired-early", expires: time.Now().Add(time.Hour), refresh: func(context.Context) (string, time.Duration, error) {
		refreshes++
		return "new-token", time.Hour, nil
	}}
	c := helixClient(NewStaticTokenSource("app"), bot, func(req *http.Request) (*http.Response, error) {
		requests++
		if requests == 1 {
			return respond(http.StatusUnauthorized, `{"message":"Invalid OAuth token"}`), nil
		}
		assert.Equal(t, "Bearer new-token", req.Header.Get("Authorization"))
		return respond(http.StatusOK, `{}`), nil
	})

	res, err := c.Execute(context.Background(), http.MethodGet, "/helix/moderation/channels", nil)
	require.NoError(t, err)
	defer res.Body.Close()

	assert.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, [2]int{2, 1}, [2]int{requests, refreshes})
}

func TestStreamReads(t *testing.T) {
	startedAt := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		status      int
		body        string
		wantLive    bool
		wantStarted time.Time
		wantDetails StreamDetails
		wantErr     bool
	}{
		{
			name: "reads a live stream", status: http.StatusOK,
			body:     `{"data":[{"type":"live","started_at":"2026-08-24T12:00:00Z","title":"Ranked grind","game_name":"Fortnite","viewer_count":42}]}`,
			wantLive: true, wantStarted: startedAt,
			wantDetails: StreamDetails{Title: "Ranked grind", GameName: "Fortnite", ViewerCount: 42, StartedAt: startedAt},
		},
		{name: "reads an offline channel", status: http.StatusOK, body: `{"data":[]}`},
		{name: "treats a non-live type as offline", status: http.StatusOK, body: `{"data":[{"type":"error","started_at":"2026-08-24T12:00:00Z"}]}`},
		{name: "surfaces a Twitch failure", status: http.StatusServiceUnavailable, body: `{"message":"unavailable"}`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := helixClient(NewStaticTokenSource("app"), nil, respondWith(tt.status, tt.body))

			started, live, startedErr := c.StreamStartedAt(context.Background(), "123")
			isLive, liveErr := c.IsStreamLive(context.Background(), "123")
			details, detailsLive, detailsErr := c.StreamDetails(context.Background(), "123")

			if tt.wantErr {
				assert.Error(t, startedErr)
				assert.Error(t, liveErr)
				assert.Error(t, detailsErr)
				return
			}
			require.NoError(t, startedErr)
			require.NoError(t, liveErr)
			require.NoError(t, detailsErr)
			assert.Equal(t, [2]bool{tt.wantLive, tt.wantLive}, [2]bool{live, isLive})
			assert.True(t, tt.wantStarted.Equal(started))
			assert.Equal(t, tt.wantLive, detailsLive)
			assert.Equal(t, tt.wantDetails, details)
		})
	}
}
