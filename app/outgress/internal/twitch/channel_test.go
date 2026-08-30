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

func channelClient(t *testing.T, handler func(*http.Request) (*http.Response, error)) *Client {
	t.Helper()
	return &Client{
		clientID: "client",
		app:      &Source{token: "app", expires: time.Now().Add(time.Hour)},
		broadcasters: NewBroadcasterTokens(func(string) *Source {
			return &Source{token: "user", expires: time.Now().Add(time.Hour)}
		}),
		http: &http.Client{Transport: roundTripFunc(handler)},
	}
}

func jsonOK(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

// routeClient fails the test on any request that is not method+path and
// answers the expected one with body.
func routeClient(t *testing.T, method, path, body string) *Client {
	t.Helper()
	return channelClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != method || req.URL.Path != path {
			t.Fatalf("unexpected %s %s, want %s %s", req.Method, req.URL.Path, method, path)
		}
		return jsonOK(body), nil
	})
}

func TestChannelInfo(t *testing.T) {
	c := channelClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || !strings.Contains(req.URL.RawQuery, "broadcaster_id=123") {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.String())
		}
		return jsonOK(`{"data":[{"title":"Ranked grind","game_id":"33214","game_name":"Fortnite","tags":["English"]}]}`), nil
	})
	info, err := c.ChannelInfo(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if info.Title != "Ranked grind" || info.GameName != "Fortnite" || info.GameID != "33214" {
		t.Fatalf("info = %+v", info)
	}
	if len(info.Tags) != 1 || info.Tags[0] != "English" {
		t.Fatalf("tags = %v", info.Tags)
	}
}

func TestSearchCategory(t *testing.T) {
	cases := []struct {
		name, body string
		wantOK     bool
	}{
		{"first hit", `{"data":[{"id":"33214","name":"Fortnite"}]}`, true},
		{"miss", `{"data":[]}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := channelClient(t, func(*http.Request) (*http.Response, error) {
				return jsonOK(tc.body), nil
			})
			cat, ok, err := c.SearchCategory(context.Background(), "fort")
			if err != nil || ok != tc.wantOK {
				t.Fatalf("SearchCategory() = ok=%v err=%v, want ok=%v", ok, err, tc.wantOK)
			}
			if ok && (cat.ID != "33214" || cat.Name != "Fortnite") {
				t.Fatalf("cat = %+v", cat)
			}
		})
	}
}

func TestModifyChannelPatch(t *testing.T) {
	var method, path string
	c := channelClient(t, func(req *http.Request) (*http.Response, error) {
		method, path = req.Method, req.URL.Path
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})
	if err := c.ModifyChannel(context.Background(), "123", ChannelPatch{Title: "Hello"}); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPatch || path != "/helix/channels" {
		t.Fatalf("got %s %s, want PATCH /helix/channels", method, path)
	}
}

func TestCreateMarker(t *testing.T) {
	c := channelClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/helix/streams/markers" {
			t.Fatalf("unexpected %s %s", req.Method, req.URL.Path)
		}
		return jsonOK(`{"data":[{"id":"1"}]}`), nil
	})
	if err := c.CreateMarker(context.Background(), "123", "boss"); err != nil {
		t.Fatal(err)
	}
}

func TestStartCommercial(t *testing.T) {
	c := channelClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Path != "/helix/channels/commercial" {
			t.Fatalf("unexpected %s %s", req.Method, req.URL.Path)
		}
		return jsonOK(`{"data":[{"length":30}]}`), nil
	})
	if err := c.StartCommercial(context.Background(), "123", 30); err != nil {
		t.Fatal(err)
	}
}
