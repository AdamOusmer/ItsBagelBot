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
)

func uptimeClient(t *testing.T, status int, body string) *Client {
	t.Helper()
	return &Client{
		clientID: "client",
		app:      &Source{token: "cached", expires: time.Now().Add(time.Hour)},
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})},
	}
}

func TestStreamStartedAtLive(t *testing.T) {
	client := uptimeClient(t, http.StatusOK,
		`{"data":[{"type":"live","started_at":"2026-08-24T12:00:00Z"}]}`)

	startedAt, live, err := client.StreamStartedAt(context.Background(), "123")
	if err != nil || !live {
		t.Fatalf("StreamStartedAt() = live=%v err=%v, want live=true err=nil", live, err)
	}
	want := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	if !startedAt.Equal(want) {
		t.Fatalf("started_at = %v, want %v", startedAt, want)
	}
}

func TestStreamStartedAtOffline(t *testing.T) {
	client := uptimeClient(t, http.StatusOK, `{"data":[]}`)

	startedAt, live, err := client.StreamStartedAt(context.Background(), "123")
	if err != nil || live {
		t.Fatalf("StreamStartedAt() = live=%v err=%v, want live=false err=nil", live, err)
	}
	if !startedAt.IsZero() {
		t.Fatalf("started_at = %v, want zero", startedAt)
	}
}

func TestIsStreamLiveFalseWhenTypeNotLive(t *testing.T) {
	client := uptimeClient(t, http.StatusOK,
		`{"data":[{"type":"error","started_at":"2026-08-24T12:00:00Z"}]}`)

	live, err := client.IsStreamLive(context.Background(), "123")
	if err != nil || live {
		t.Fatalf("IsStreamLive() = live=%v err=%v, want live=false err=nil", live, err)
	}
}

func TestStreamStartedAtSurfacesTwitchFailure(t *testing.T) {
	client := uptimeClient(t, http.StatusServiceUnavailable, `{"message":"unavailable"}`)

	if _, _, err := client.StreamStartedAt(context.Background(), "123"); err == nil {
		t.Fatal("StreamStartedAt() error = nil, want StatusError")
	}
}

func TestStreamDetailsLive(t *testing.T) {
	client := uptimeClient(t, http.StatusOK,
		`{"data":[{"type":"live","started_at":"2026-08-24T12:00:00Z","title":"Ranked grind","game_name":"Fortnite","viewer_count":42}]}`)

	details, live, err := client.StreamDetails(context.Background(), "123")
	if err != nil || !live {
		t.Fatalf("StreamDetails() = live=%v err=%v, want live=true err=nil", live, err)
	}
	want := StreamDetails{
		Title:       "Ranked grind",
		GameName:    "Fortnite",
		ViewerCount: 42,
		StartedAt:   time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC),
	}
	if details != want {
		t.Fatalf("StreamDetails() = %+v, want %+v", details, want)
	}
}

func TestStreamDetailsOffline(t *testing.T) {
	client := uptimeClient(t, http.StatusOK, `{"data":[]}`)

	details, live, err := client.StreamDetails(context.Background(), "123")
	if err != nil || live {
		t.Fatalf("StreamDetails() = live=%v err=%v, want live=false err=nil", live, err)
	}
	if details != (StreamDetails{}) {
		t.Fatalf("StreamDetails() = %+v, want zero value", details)
	}
}
