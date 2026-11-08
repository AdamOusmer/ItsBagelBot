// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func titleList(titles ...string) string {
	parts := make([]string, len(titles))
	for i, t := range titles {
		parts[i] = `{"title":"` + t + `"}`
	}
	return strings.Join(parts, ",")
}

type expectedCreation struct {
	listKey, secondsKey, title, list string
	seconds                          int
}

func createBody(want expectedCreation) string {
	return `{"broadcaster_id":"` + testBroadcaster + `","` + want.listKey + `":[` + want.list + `],"` + want.secondsKey + `":` + strconv.Itoa(want.seconds) + `,"title":"` + want.title + `"}`
}

type expectedEnding struct{ id, status, winner string }

func endBody(want expectedEnding) string {
	extra := ""
	if want.winner != "" {
		extra = `,"winning_outcome_id":"` + want.winner + `"`
	}
	return `{"broadcaster_id":"` + testBroadcaster + `","id":"` + want.id + `","status":"` + want.status + `"` + extra + `}`
}

func latest(path string) recordedRequest {
	return call("GET", path+"?broadcaster_id="+testBroadcaster+"&first=1", broadcasterAuth, "")
}

func ok(body string) []scriptedResponse { return []scriptedResponse{{status: 200, body: body}} }

func TestPollCreateAppliesTheTwitchLimits(t *testing.T) {
	long := strings.Repeat("é", 70)
	runActionCases(t, []actionCase{
		{
			name: "trimmed poll is created and confirmed",
			msg:  testMessage{Type: "poll_create", Payload: `{"title":" Pick ","choices":[" A ","B"],"duration":120}`},
			want: []recordedRequest{
				call("POST", "/helix/polls", broadcasterAuth, createBody(expectedCreation{"choices", "duration", "Pick", titleList("A", "B"), 120})),
			},
		},
		{
			name: "long title, long choices, extra choices and long duration are cut",
			msg:  testMessage{Type: "poll_create", Payload: `{"title":"` + long + `","choices":["` + strings.Repeat("x", 40) + `","b","c","d","e","f"],"duration":5000}`},
			want: []recordedRequest{call("POST", "/helix/polls", broadcasterAuth,
				createBody(expectedCreation{"choices", "duration", strings.Repeat("é", 60), titleList(strings.Repeat("x", 25), "b", "c", "d", "e"), 1800}))},
		},
		{
			name: "zero duration defaults to a minute",
			msg:  testMessage{Type: "poll_create", Payload: `{"title":"t","choices":["a","b"]}`},
			want: []recordedRequest{call("POST", "/helix/polls", broadcasterAuth, createBody(expectedCreation{"choices", "duration", "t", titleList("a", "b"), 60}))},
		},
		{
			name: "short duration is raised to the minimum",
			msg:  testMessage{Type: "poll_create", Payload: `{"title":"t","choices":["a","b"],"duration":3}`},
			want: []recordedRequest{call("POST", "/helix/polls", broadcasterAuth, createBody(expectedCreation{"choices", "duration", "t", titleList("a", "b"), 15}))},
		},
		{
			name: "empty title is dropped",
			msg:  testMessage{Type: "poll_create", Payload: `{"title":" ","choices":["a","b"]}`},
		},
		{
			name: "fewer than two usable choices is dropped",
			msg:  testMessage{Type: "poll_create", Payload: `{"title":"t","choices":["a"," "]}`},
		},
	})
}

func TestPollEndOnlyEndsTheActivePoll(t *testing.T) {
	active := ok(`{"data":[{"id":"p1","status":"ACTIVE"}]}`)
	runActionCases(t, []actionCase{
		{
			name:   "active poll is terminated by default and confirmed",
			msg:    testMessage{Type: "poll_end", Payload: `{}`},
			script: active,
			want: []recordedRequest{latest("/helix/polls"),
				call("PATCH", "/helix/polls", broadcasterAuth, endBody(expectedEnding{"p1", "TERMINATED", ""}))},
		},
		{
			name:   "archive is accepted",
			msg:    testMessage{Type: "poll_end", Payload: `{"status":"ARCHIVED"}`},
			script: active,
			want:   []recordedRequest{latest("/helix/polls"), call("PATCH", "/helix/polls", broadcasterAuth, endBody(expectedEnding{"p1", "ARCHIVED", ""}))},
		},
		{
			name:   "finished poll is left alone without a confirmation",
			msg:    testMessage{Type: "poll_end", Payload: `{}`},
			script: ok(`{"data":[{"id":"p1","status":"COMPLETED"}]}`),
			want:   []recordedRequest{latest("/helix/polls")},
		},
		{
			name:   "no polls at all",
			msg:    testMessage{Type: "poll_end", Payload: `{}`},
			script: ok(`{"data":[]}`),
			want:   []recordedRequest{latest("/helix/polls")},
		},
		{
			name: "unsupported status never reaches twitch",
			msg:  testMessage{Type: "poll_end", Payload: `{"status":"COMPLETED"}`},
		},
	})
}

func TestPredictionCreateAppliesTheTwitchLimits(t *testing.T) {
	runActionCases(t, []actionCase{
		{
			name: "long title and outcome are cut, short window raised, confirmed",
			msg: testMessage{Type: "prediction_create",
				Payload: `{"title":"` + strings.Repeat("t", 80) + `","outcomes":["yes","` + strings.Repeat("n", 30) + `"],"window":10}`},
			want: []recordedRequest{
				call("POST", "/helix/predictions", broadcasterAuth,
					createBody(expectedCreation{"outcomes", "prediction_window", strings.Repeat("t", 45), titleList("yes", strings.Repeat("n", 25)), 30})),
			},
		},
		{
			name: "zero window defaults to two minutes",
			msg:  testMessage{Type: "prediction_create", Payload: `{"title":"t","outcomes":["a","b"]}`},
			want: []recordedRequest{call("POST", "/helix/predictions", broadcasterAuth, createBody(expectedCreation{"outcomes", "prediction_window", "t", titleList("a", "b"), 120}))},
		},
		{
			name: "one outcome is dropped",
			msg:  testMessage{Type: "prediction_create", Payload: `{"title":"t","outcomes":["a"]}`},
		},
	})
}

func TestPredictionEndFollowsTheStatusTransitions(t *testing.T) {
	prediction := func(status string) []scriptedResponse {
		return ok(`{"data":[{"id":"pr1","status":"` + status + `","outcomes":[{"id":"o1"},{"id":"o2"}]}]}`)
	}
	patch := func(want expectedEnding) recordedRequest {
		want.id = "pr1"
		return call("PATCH", "/helix/predictions", broadcasterAuth, endBody(want))
	}
	runActionCases(t, []actionCase{
		{
			name:   "lock an active prediction",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"LOCKED"}`},
			script: prediction("ACTIVE"),
			want:   []recordedRequest{latest("/helix/predictions"), patch(expectedEnding{status: "LOCKED", winner: ""})},
		},
		{
			name:   "locking a locked prediction is dropped",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"LOCKED"}`},
			script: prediction("LOCKED"),
			want:   []recordedRequest{latest("/helix/predictions")},
		},
		{
			name:   "winner 2 resolves to the second outcome id",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"RESOLVED","winner":2}`},
			script: prediction("LOCKED"),
			want:   []recordedRequest{latest("/helix/predictions"), patch(expectedEnding{status: "RESOLVED", winner: "o2"})},
		},
		{
			name:   "out of range winner is dropped",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"RESOLVED","winner":3}`},
			script: prediction("LOCKED"),
			want:   []recordedRequest{latest("/helix/predictions")},
		},
		{
			name:   "missing winner is dropped",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"RESOLVED"}`},
			script: prediction("ACTIVE"),
			want:   []recordedRequest{latest("/helix/predictions")},
		},
		{
			name:   "cancel an active prediction",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"CANCELED"}`},
			script: prediction("ACTIVE"),
			want:   []recordedRequest{latest("/helix/predictions"), patch(expectedEnding{status: "CANCELED", winner: ""})},
		},
		{
			name:   "cancelling a resolved prediction is dropped",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"CANCELED"}`},
			script: prediction("RESOLVED"),
			want:   []recordedRequest{latest("/helix/predictions")},
		},
		{
			name:   "no predictions at all",
			msg:    testMessage{Type: "prediction_end", Payload: `{"status":"CANCELED"}`},
			script: ok(`{"data":[]}`),
			want:   []recordedRequest{latest("/helix/predictions")},
		},
	})
}

func TestPollCreateFailureClassification(t *testing.T) {
	msg := testMessage{Type: "poll_create", Payload: `{"title":"t","choices":["a","b"]}`}
	cases := []struct {
		name      string
		status    int
		wantRetry bool
	}{
		{"bad request is dropped without retry", http.StatusBadRequest, false},
		{"unavailable twitch is retried", http.StatusServiceUnavailable, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rt := &scriptedTransport{responses: []scriptedResponse{{status: tc.status, body: `{"message":"x"}`}}}

			err := msg.send(pipelineWorker(t, rt))

			assert.Equal(t, tc.wantRetry, err != nil)
			require.Equal(t, 1, rt.callCount())
		})
	}
}
