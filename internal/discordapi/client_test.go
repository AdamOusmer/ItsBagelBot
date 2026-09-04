// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestSendMessageClassifiesStatuses(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"ok", 200, `{}`, nil},
		{"bad request is permanent", 400, `{"message": "Cannot send an empty message"}`, ErrBadRequest},
		{"unauthorized is permanent", 401, `{}`, ErrAuth},
		{"forbidden is permanent", 403, `{"message": "Missing Permissions"}`, ErrForbidden},
		{"unknown channel is permanent", 404, `{"message": "Unknown Channel"}`, ErrChannelNotFound},
		{"rate limited nacks", 429, `{"retry_after": 1.5, "global": false}`, ErrRateLimited},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := NewClient("bot-token")
			client.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
				return jsonResponse(tc.status, tc.body), nil
			}))

			err := client.SendMessage(context.Background(), "1234", "hello", false)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestServerErrorIsTransient(t *testing.T) {
	client := NewClient("bot-token")
	client.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(502, `{}`), nil
	}))

	err := client.SendMessage(context.Background(), "1234", "hi", false)
	if err == nil {
		t.Fatal("a 502 must surface as an error the worker nacks on")
	}
	if permanent(err) {
		t.Fatalf("err = %v, want an unclassified transient error", err)
	}
}

func TestSendMessageRequestShape(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	client := NewClient("bot-token")
	client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		return jsonResponse(200, `{}`), nil
	}))

	if err := client.SendMessage(context.Background(), "1234567890", "hello", true); err != nil {
		t.Fatalf("send: %v", err)
	}
	if !strings.HasSuffix(gotPath, "/channels/1234567890/messages") {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bot bot-token" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	for _, want := range []string{`"content":"hello"`, `"tts":true`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("body %s missing %s", gotBody, want)
		}
	}
}

func TestTTSShouldStayOffTheWireWhenUnset(t *testing.T) {
	var gotBody string
	client := NewClient("bot-token")
	client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		return jsonResponse(200, `{}`), nil
	}))

	if err := client.SendMessage(context.Background(), "1", "hi", false); err != nil {
		t.Fatalf("send: %v", err)
	}
	if strings.Contains(gotBody, "tts") {
		t.Fatalf("body %s should omit tts entirely", gotBody)
	}
}

// permanent mirrors the worker's drop set.
func permanent(err error) bool {
	for _, typed := range []error{ErrAuth, ErrForbidden, ErrChannelNotFound, ErrBadRequest} {
		if errors.Is(err, typed) {
			return true
		}
	}
	return false
}

// TestGetGuildWithCounts pins both halves of the one call the dashboard's
// server card makes: with_counts=true must actually be on the request (the
// member count is absent from the reply without it, and a missing count
// looks like a guild of zero people), and the icon hash must render as a CDN
// URL rather than being handed to the browser raw.
func TestGetGuildWithCounts(t *testing.T) {
	var gotURL string
	client := NewClient("bot-token")
	client.SetTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotURL = r.URL.String()
		return jsonResponse(200, `{"id":"9","name":"Bagel HQ","icon":"abc","approximate_member_count":1234}`), nil
	}))

	got, err := client.GetGuildWithCounts(context.Background(), Guild{ID: "9"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "/guilds/9") || !strings.Contains(gotURL, "with_counts=true") {
		t.Fatalf("url = %q, want /guilds/9 with with_counts=true", gotURL)
	}
	if got.Name != "Bagel HQ" || got.ApproximateMemberCount != 1234 {
		t.Fatalf("guild = %+v", got)
	}
	if want := "https://cdn.discordapp.com/icons/9/abc.png"; got.IconURL() != want {
		t.Fatalf("IconURL() = %q, want %q", got.IconURL(), want)
	}
}

func TestGuildIconURLIsEmptyWithoutAnIcon(t *testing.T) {
	if got := (GuildInfo{ID: "9"}).IconURL(); got != "" {
		t.Fatalf("IconURL() = %q, want empty so the dashboard renders its placeholder", got)
	}
}

func TestGetGuildWithCountsClassifiesForbidden(t *testing.T) {
	client := NewClient("bot-token")
	client.SetTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return jsonResponse(403, `{"message": "Missing Access"}`), nil
	}))
	if _, err := client.GetGuildWithCounts(context.Background(), Guild{ID: "9"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}
