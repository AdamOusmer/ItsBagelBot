// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import "testing"

func rewardTestEvent() redemptionEvent {
	var ev redemptionEvent
	ev.UserName = "Sam"
	ev.UserLogin = "sam"
	ev.UserInput = "a song"
	return ev
}

func TestSongqueueRedeemReplyResolvesDynamic(t *testing.T) {
	ev := rewardTestEvent()
	for _, tc := range [][2]string{
		{"{choice:only} pick, @{user}", "only pick, @Sam"},
		{"[{choice:}]", "[]"},
		{"{choice}", "{choice}"},
		{"{random:7-7}", "7"},
		{"{unknown}", "{unknown}"},
		{"@{user} queued {track}, position #{pos}.", "@Sam queued Song, position #3."},
	} {
		if got := renderSongqueueRedeemReply(songqueueRedeemReplyParams{event: ev, text: tc[0], track: "Song", pos: 3}); got != tc[1] {
			t.Errorf("renderSongqueueRedeemReply(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}

func TestGoveeReplyResolvesDynamic(t *testing.T) {
	ev := rewardTestEvent()
	for _, tc := range [][2]string{
		{"{choice:only} light, @{user}", "only light, @Sam"},
		{"[{choice:}]", "[]"},
		{"{choice}", "{choice}"},
		{"{random:7-7}", "7"},
		{"{unknown}", "{unknown}"},
		{"@{user} set the lights to {color}!", "@Sam set the lights to blue!"},
	} {
		if got := renderGoveeReply("", tc[0], ev, "blue"); got != tc[1] {
			t.Errorf("renderGoveeReply(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}
