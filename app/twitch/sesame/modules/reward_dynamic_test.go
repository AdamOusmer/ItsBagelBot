// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import "testing"

// The dynamic-span fallthrough on every reward surface. Decision record.
//
// A broadcaster's reward reply is one line of copy, and the same line gets
// pasted from one reward into another. Until this change two surfaces did not
// resolve the generic dynamic spans: govee and songqueue_redeem ended their
// repl switch in `return "", false` while channelpoints, alerts, shoutout,
// timeofday and emoteplay ended it in the dynamic vars. So
// "{choice:nice,great} pick, @{user}" worked in a channel-points reward and
// printed literal braces in the song-request one, with nothing on either
// surface explaining why.
//
// The drift had already leaked out of Go: the web command builder carries a
// hand-written per-surface exception for it. Making the surfaces agree is what
// lets that exception be deleted rather than kept in sync forever, so the two
// tests below are the contract the console and web sides now build against:
// {random} and {choice:…} resolve on ALL reward surfaces.
//
// The rows deliberately include the {choice} / {choice:} pair, because that is
// the one distinction a "just make it resolve" fix tends to flatten: no
// payload names no options and stays literal, an empty payload names an empty
// option and resolves to "".

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
		if got := renderSongqueueRedeemReply(tc[0], ev, "Song", 3); got != tc[1] {
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
		if got := renderGoveeReply(tc[0], ev, "blue"); got != tc[1] {
			t.Errorf("renderGoveeReply(%q) = %q, want %q", tc[0], got, tc[1])
		}
	}
}
