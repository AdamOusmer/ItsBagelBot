// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
)

func mcsrChat(text, config string) gossipChat {
	return gossipChat{module: "mcsr", text: text, config: config}
}

func TestMcsrStreamOnlineSnapshots(t *testing.T) {
	replies := map[string]any{"mcsr.session_start": gossiprpc.McsrSnapshotReply{Nickname: "Feinberg", Elo: 1650}}
	runSnapshotCases(t, []snapshotCase{
		{name: "stream online stores the linked name baseline", module: "mcsr", event: "stream.online", config: `{"account":"Feinberg"}`, replies: replies,
			call: &gossipCall{"mcsr.session_start", gossiprpc.Request{Account: "Feinberg", ChannelID: "2"}}},
		{name: "stream online prefers the stored uuid", module: "mcsr", event: "stream.online",
			config: `{"account":"Feinberg","accountUuid":"` + testUUID + `"}`, replies: replies,
			call: &gossipCall{"mcsr.session_start", gossiprpc.Request{Account: testUUID, ChannelID: "2"}}},
	})
}
