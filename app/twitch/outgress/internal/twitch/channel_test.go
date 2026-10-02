// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func channelClient(handler roundTripFunc) *Client {
	c := NewClient("client", NewStaticTokenSource("app"), nil, NewBroadcasterTokens(func(string) *Source { return NewStaticTokenSource("user") }))
	c.SetTransport(handler)
	return c
}

func routeClient(t *testing.T, method, path, body string) *Client {
	t.Helper()
	return channelClient(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, [2]string{method, path}, [2]string{req.Method, req.URL.Path})
		return respond(http.StatusOK, body), nil
	})
}

func TestChannelInfo(t *testing.T) {
	c := routeClient(t, http.MethodGet, "/helix/channels",
		`{"data":[{"title":"Ranked grind","game_id":"33214","game_name":"Fortnite","tags":["English"]}]}`)

	info, err := c.ChannelInfo(context.Background(), "123")

	require.NoError(t, err)
	assert.Equal(t, ChannelInfo{Title: "Ranked grind", GameID: "33214", GameName: "Fortnite", Tags: []string{"English"}}, info)
}

func TestSearchCategory(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		wantOK bool
		want   Category
	}{
		{"returns the first hit", `{"data":[{"id":"33214","name":"Fortnite"}]}`, true, Category{ID: "33214", Name: "Fortnite"}},
		{"reports a miss", `{"data":[]}`, false, Category{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := channelClient(respondWith(http.StatusOK, tt.body))

			cat, ok, err := c.SearchCategory(context.Background(), "fort")

			require.NoError(t, err)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, cat)
		})
	}
}

func TestChannelWrites(t *testing.T) {
	tests := []struct {
		name, method, path, body string
		call                     func(*Client) error
	}{
		{"modify channel", http.MethodPatch, "/helix/channels", `{}`, func(c *Client) error {
			return c.ModifyChannel(context.Background(), "123", ChannelPatch{Title: "Hello"})
		}},
		{"create marker", http.MethodPost, "/helix/streams/markers", `{"data":[{"id":"1"}]}`, func(c *Client) error {
			return c.CreateMarker(context.Background(), "123", "boss")
		}},
		{"start commercial", http.MethodPost, "/helix/channels/commercial", `{"data":[{"length":30}]}`, func(c *Client) error {
			return c.StartCommercial(context.Background(), "123", 30)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.NoError(t, tt.call(routeClient(t, tt.method, tt.path, tt.body)))
		})
	}
}
