// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package twitch

import (
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreatePollChannelPointsVoting(t *testing.T) {
	cases := []struct {
		name string
		cost int
		body string
	}{
		{"disabled", 0, `{"broadcaster_id":"42","title":"Pick","choices":[{"title":"A"},{"title":"B"}],"duration":60}`},
		{"enabled", 100, `{"broadcaster_id":"42","title":"Pick","choices":[{"title":"A"},{"title":"B"}],"duration":60,"channel_points_voting_enabled":true,"channel_points_per_vote":100}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tokens := NewBroadcasterTokens(func(string) *Source { return NewStaticTokenSource("broadcaster-token") })
			client := NewClient("client", nil, nil, tokens)
			var body []byte
			client.SetTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
				var err error
				body, err = io.ReadAll(req.Body)
				return respond(http.StatusOK, `{}`), err
			}))
			err := client.CreatePoll(t.Context(), ContestCreation{
				Channel: ContestChannel{BroadcasterID: "42"},
				Options: CreateOptions{Title: "Pick", Items: []string{"A", "B"}, Seconds: 60, ChannelPointsPerVote: tc.cost},
			})
			require.NoError(t, err)
			assert.JSONEq(t, tc.body, string(body))
		})
	}
}
