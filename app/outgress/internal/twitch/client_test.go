package twitch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

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
	} {
		if got := client.sourceFor(endpoint); got != app {
			t.Errorf("sourceFor(%q) = user token, want app token", endpoint)
		}
	}
}

func TestMissingScope401DoesNotRefreshToken(t *testing.T) {
	refreshes := 0
	source := &Source{
		token:   "still-valid",
		expires: time.Now().Add(time.Hour),
		refresh: func(context.Context) (string, time.Duration, error) {
			refreshes++
			return "new-token", time.Hour, nil
		},
	}
	wantBody := `{"status":401,"message":"Missing scope: user:read:moderated_channels"}`
	client := &Client{
		clientID: "client",
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Body:       io.NopCloser(strings.NewReader(wantBody)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	res, err := client.request(context.Background(), source, http.MethodGet, "/helix/moderation/channels", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != wantBody {
		t.Fatalf("response body = %q, want %q", body, wantBody)
	}
	if refreshes != 0 {
		t.Fatalf("refresh calls = %d, want 0", refreshes)
	}
	if token, ok := source.cached(0); ok || token != "" {
		t.Fatal("missing-scope token remained cached; re-authorization would not be picked up")
	}
}

func TestInvalidToken401StillRefreshesAndRetries(t *testing.T) {
	refreshes := 0
	requests := 0
	source := &Source{
		token:   "expired-early",
		expires: time.Now().Add(time.Hour),
		refresh: func(context.Context) (string, time.Duration, error) {
			refreshes++
			return "new-token", time.Hour, nil
		},
	}
	client := &Client{
		clientID: "client",
		http: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			requests++
			if requests == 1 {
				return &http.Response{
					StatusCode: http.StatusUnauthorized,
					Body:       io.NopCloser(strings.NewReader(`{"message":"Invalid OAuth token"}`)),
					Header:     make(http.Header),
				}, nil
			}
			if got := req.Header.Get("Authorization"); got != "Bearer new-token" {
				t.Fatalf("retry authorization = %q", got)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{}`)),
				Header:     make(http.Header),
			}, nil
		})},
	}

	res, err := client.request(context.Background(), source, http.MethodGet, "/helix/users", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	if requests != 2 || refreshes != 1 {
		t.Fatalf("requests/refreshes = %d/%d, want 2/1", requests, refreshes)
	}
}
