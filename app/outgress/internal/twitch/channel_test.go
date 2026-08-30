// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"io"
	"net/http"
	"reflect"
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
	c := routeClient(t, http.MethodGet, "/helix/channels",
		`{"data":[{"title":"Ranked grind","game_id":"33214","game_name":"Fortnite","tags":["English"]}]}`)
	info, err := c.ChannelInfo(context.Background(), "123")
	if err != nil {
		t.Fatal(err)
	}
	want := ChannelInfo{Title: "Ranked grind", GameID: "33214", GameName: "Fortnite", Tags: []string{"English"}}
	if !reflect.DeepEqual(info, want) {
		t.Fatalf("info = %+v, want %+v", info, want)
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

// TestChannelWrites pins each write helper to its Helix method+path; the
// route assertion lives in routeClient so a rename here cannot silently
// stop checking it.
func TestChannelWrites(t *testing.T) {
	cases := []struct {
		name, method, path, body string
		call                     func(*Client) error
	}{
		{"modify channel", http.MethodPatch, "/helix/channels", `{}`,
			func(c *Client) error {
				return c.ModifyChannel(context.Background(), "123", ChannelPatch{Title: "Hello"})
			}},
		{"create marker", http.MethodPost, "/helix/streams/markers", `{"data":[{"id":"1"}]}`,
			func(c *Client) error { return c.CreateMarker(context.Background(), "123", "boss") }},
		{"start commercial", http.MethodPost, "/helix/channels/commercial", `{"data":[{"length":30}]}`,
			func(c *Client) error { return c.StartCommercial(context.Background(), "123", 30) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(routeClient(t, tc.method, tc.path, tc.body)); err != nil {
				t.Fatal(err)
			}
		})
	}
}
