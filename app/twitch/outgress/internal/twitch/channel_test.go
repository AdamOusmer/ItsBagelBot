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
	const categories = `{"data":[{"id":"1","name":"Fortnite Festival"},{"id":"2","name":"Fortnite"}]}`
	tests := []struct {
		name  string
		query string
		body  string
		want  Category
	}{
		{"prefers the exact match", "fortnite", categories, Category{ID: "2", Name: "Fortnite"}},
		{"falls back to the first hit", "fort", categories, Category{ID: "1", Name: "Fortnite Festival"}},
		{"ignores empty IDs", "fort", `{"data":[{"name":"Fort"},{"id":"2","name":"Fortnite"}]}`, Category{ID: "2", Name: "Fortnite"}},
		{"reports a miss", "fort", `{"data":[]}`, Category{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := channelClient(respondWith(http.StatusOK, tt.body))

			cat, ok, err := c.SearchCategory(context.Background(), tt.query)

			require.NoError(t, err)
			assert.Equal(t, tt.want.ID != "", ok)
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
