// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"context"
	"testing"
)

func TestLiveRefusesForeignTargetAndStoredMessage(t *testing.T) {
	rest := &fakeEngineREST{channelGuilds: map[string]string{"c2": "g2"}}
	live := newMemLive()
	h := &engineRPC{rest: rest, live: live}
	r := h.handleLiveOnline(context.Background(), discordoutgress.LiveOnlineRequest{GuildID: "g1", ChannelID: "c2"})
	if r.Error == "" || len(rest.sent) != 0 {
		t.Fatal("foreign live post allowed")
	}
	live.msgs[kv.GuildID("g1")] = discapi.Message{ChannelID: "c2", ID: "old"}
	o := h.handleLiveOffline(context.Background(), discordoutgress.LiveOfflineRequest{GuildID: "g1"})
	if o.Error == "" || len(rest.edited) != 0 {
		t.Fatal("foreign remembered message edited")
	}
}

func TestTicketPanelAndTranscriptRefuseForeignChannel(t *testing.T) {
	h, tr := newTicketRPC(t, nil)
	tr.channelGuilds = map[string]string{"c2": "g2"}
	r := h.panel(context.Background(), discordoutgress.TicketPanelRequest{GuildID: "g1", ChannelID: "c2"})
	if r.Error == "" || len(tr.find("POST", "/channels/c2/messages")) != 0 {
		t.Fatal("foreign panel posted")
	}
	h.postSummary(context.Background(), summaryPost{req: discordoutgress.TicketCloseRequest{GuildID: "g1", LogChannelID: "c2"}, body: "private transcript"})
	if len(tr.find("POST", "/channels/c2/messages")) != 0 {
		t.Fatal("foreign transcript/summary posted")
	}
}
