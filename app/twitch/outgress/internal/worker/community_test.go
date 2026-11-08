// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const broadcasterAuth = "Bearer broadcaster-token"

var bobLookup = scriptedResponse{status: 200, body: `{"data":[{"id":"777","login":"bob"}]}`}

func userLookup(login string) recordedRequest {
	return call("GET", "/helix/users?login="+login, appAuth, "")
}

func TestCommunityActionsResolveTheTargetAndActAsTheBroadcaster(t *testing.T) {
	roleQuery := "?broadcaster_id=" + testBroadcaster + "&user_id=777"
	runActionCases(t, []actionCase{
		{
			name:   "raid sends the viewers to the target",
			msg:    testMessage{Type: "raid_start", To: "bob", Payload: `{}`},
			script: []scriptedResponse{bobLookup, {status: 200, body: `{"data":[{}]}`}},
			want: []recordedRequest{userLookup("bob"),
				call("POST", "/helix/raids?from_broadcaster_id="+testBroadcaster+"&to_broadcaster_id=777", broadcasterAuth, "")},
		},
		{
			name: "unraid cancels the pending raid",
			msg:  testMessage{Type: "raid_cancel", Payload: `{}`},
			want: []recordedRequest{call("DELETE", "/helix/raids?broadcaster_id="+testBroadcaster, broadcasterAuth, "")},
		},
		{
			name:   "mod adds a channel moderator",
			msg:    testMessage{Type: "mod_add", To: "@Bob", Payload: `{}`},
			script: []scriptedResponse{bobLookup},
			want:   []recordedRequest{userLookup("bob"), call("POST", "/helix/moderation/moderators"+roleQuery, broadcasterAuth, "")},
		},
		{
			name:   "unmod removes a channel moderator",
			msg:    testMessage{Type: "mod_remove", To: "bob", Payload: `{}`},
			script: []scriptedResponse{bobLookup},
			want:   []recordedRequest{userLookup("bob"), call("DELETE", "/helix/moderation/moderators"+roleQuery, broadcasterAuth, "")},
		},
		{
			name:   "vip adds a channel VIP",
			msg:    testMessage{Type: "vip_add", To: "bob", Payload: `{}`},
			script: []scriptedResponse{bobLookup},
			want:   []recordedRequest{userLookup("bob"), call("POST", "/helix/channels/vips"+roleQuery, broadcasterAuth, "")},
		},
		{
			name:   "unvip removes a channel VIP and confirms in chat",
			msg:    testMessage{Type: "vip_remove", To: "bob", Payload: `{}`},
			script: []scriptedResponse{bobLookup},
			want:   []recordedRequest{userLookup("bob"), call("DELETE", "/helix/channels/vips"+roleQuery, broadcasterAuth, "")},
		},
		{
			name:   "an unknown login costs no action call",
			msg:    testMessage{Type: "mod_add", To: "ghost", Payload: `{}`},
			script: []scriptedResponse{{status: 200, body: `{"data":[]}`}},
			want:   []recordedRequest{userLookup("ghost")},
		},
		{
			name:   "a raid on the channel itself is refused",
			msg:    testMessage{Type: "raid_start", To: "self", Payload: `{}`},
			script: []scriptedResponse{{status: 200, body: `{"data":[{"id":"` + testBroadcaster + `","login":"self"}]}`}},
			want:   []recordedRequest{userLookup("self")},
		},
		{
			name:   "a rejected promotion is dropped without its confirmation",
			msg:    testMessage{Type: "mod_add", To: "bob", Payload: `{}`},
			script: []scriptedResponse{bobLookup, {status: 422, body: `{"message":"user is a vip"}`}},
			want:   []recordedRequest{userLookup("bob"), call("POST", "/helix/moderation/moderators"+roleQuery, broadcasterAuth, "")},
		},
	})
}

func TestCommunityActionRetriesWhenTwitchThrottles(t *testing.T) {
	rt := &scriptedTransport{responses: []scriptedResponse{bobLookup, {status: 429, body: `{}`}}}

	err := testMessage{Type: "vip_add", To: "bob", Payload: `{}`}.send(pipelineWorker(t, rt))

	require.Error(t, err)
	assert.Equal(t, 2, rt.callCount())
}
