// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package youtube

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// roundTripFunc fakes the transport without a network (same pattern as
// internal/twitch/client_test.go).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	rec := httptest.NewRecorder()
	rec.Code = status
	rec.Body.WriteString(body)
	return rec.Result()
}

// fakeSource hands out "stale" until invalidated, then "fresh" forever.
// Call counters pin the retry contract without a network.
type fakeSource struct {
	calls   atomic.Int64
	invalid atomic.Int64
}

func (s *fakeSource) Token(context.Context) (string, error) {
	s.calls.Add(1)
	if s.invalid.Load() > 0 {
		return "fresh", nil
	}
	return "stale", nil
}

func (s *fakeSource) Invalidate() { s.invalid.Add(1) }

func TestSendChatMessageClassifiesStatuses(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"ok", 200, `{}`, nil},
		{"quota exhausted is permanent", 403, `{"error":{"errors":[{"reason":"quotaExceeded"}]}}`, ErrQuotaExhausted},
		{"chat ended is permanent", 403, `{"error":{"errors":[{"reason":"liveChatEnded"}]}}`, ErrChatEnded},
		{"chat disabled is permanent", 403, `{"error":{"errors":[{"reason":"liveChatDisabled"}]}}`, ErrChatEnded},
		{"forbidden without known reason is auth", 403, `{"error":"forbidden"}`, ErrAuth},
		{"rate limited nacks", 429, `{}`, ErrRateLimited},
		{"not found is a stale chat id", 404, `{}`, ErrChatNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath string
			client := NewClient(&fakeSource{})
			client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
				gotPath = r.URL.Path + "?" + r.URL.RawQuery
				return jsonResponse(tc.status, tc.body), nil
			}))

			err := client.SendChatMessage(context.Background(), "chat-1", "hello")

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil && !strings.HasSuffix(gotPath, "/liveChatMessages?part=snippet") {
				t.Fatalf("path = %q", gotPath)
			}
		})
	}
}

func TestUnauthorizedRetriesOnceWithFreshToken(t *testing.T) {
	src := &fakeSource{}
	client := NewClient(src)
	statuses := []int{401, 200}
	client.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		status := statuses[0]
		statuses = statuses[1:]
		return jsonResponse(status, `{}`), nil
	}))

	if err := client.SendChatMessage(context.Background(), "chat-1", "hi"); err != nil {
		t.Fatalf("retry after 401 failed: %v", err)
	}
	if src.calls.Load() != 2 {
		t.Fatalf("token calls = %d, want 2 (invalidate forces a fresh mint)", src.calls.Load())
	}
	if src.invalid.Load() != 1 {
		t.Fatalf("invalidations = %d, want 1", src.invalid.Load())
	}
}

func TestSecondUnauthorizedIsPermanent(t *testing.T) {
	client := NewClient(&fakeSource{})
	client.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(401, `{}`), nil
	}))

	err := client.SendChatMessage(context.Background(), "chat-1", "hi")
	if !errors.Is(err, ErrAuth) {
		t.Fatalf("err = %v, want ErrAuth", err)
	}
}

func TestBanBodyShape(t *testing.T) {
	var gotBody string
	client := NewClient(&fakeSource{})
	client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		return jsonResponse(200, `{}`), nil
	}))

	if err := client.Timeout(context.Background(), "chat-1", "UCtarget", 600); err != nil {
		t.Fatalf("timeout: %v", err)
	}
	for _, want := range []string{
		`"liveChatId":"chat-1"`,
		`"type":"temporary"`,
		`"banDurationSeconds":600`,
		`"channelId":"UCtarget"`,
	} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body %s missing %s", gotBody, want)
		}
	}
}
