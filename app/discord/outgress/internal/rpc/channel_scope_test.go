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

type guildScopeCase struct {
	name    string
	refused func(t *testing.T, guildID string) (errText string, touched bool)
}

func engineScope(call func(h *engineRPC, guildID string) string, touched func(rest *fakeEngineREST) bool) func(*testing.T, string) (string, bool) {
	return func(_ *testing.T, guildID string) (string, bool) {
		rest := &fakeEngineREST{channelGuilds: map[string]string{"c2": "g2"}, listed: []discapi.Snowflake{{ID: "m1"}, {ID: "m2"}}}
		errText := call(&engineRPC{rest: rest}, guildID)
		return errText, touched(rest)
	}
}

func ticketScope(call func(h *ticketRPC, guildID string) string, writes ...[2]string) func(*testing.T, string) (string, bool) {
	return func(t *testing.T, guildID string) (string, bool) {
		h, tr := newTicketRPC(t, nil)
		tr.channelGuilds = map[string]string{"c2": "g2"}
		errText := call(h, guildID)
		hits := 0
		for _, w := range writes {
			hits += len(tr.find(w[0], w[1]))
		}
		return errText, hits > 0
	}
}

func TestChannelMutationsRefuseForeignAndEmptyGuild(t *testing.T) {
	ctx := context.Background()
	cases := []guildScopeCase{
		{"delete", engineScope(func(h *engineRPC, g string) string {
			return h.handleDelete(ctx, discordoutgress.ChannelDeleteRequest{GuildID: g, ChannelID: "c2"}).Error
		}, func(r *fakeEngineREST) bool { return len(r.deleted) > 0 })},
		{"modify", engineScope(func(h *engineRPC, g string) string {
			return h.handleModify(ctx, discordoutgress.ChannelModifyRequest{GuildID: g, ChannelID: "c2", Name: "renamed"}).Error
		}, func(r *fakeEngineREST) bool { return len(r.modified) > 0 })},
		{"purge", engineScope(func(h *engineRPC, g string) string {
			return h.handlePurge(ctx, discordoutgress.PurgeRequest{GuildID: g, ChannelID: "c2", Count: 50}).Error
		}, func(r *fakeEngineREST) bool { return len(r.bulkDel) > 0 })},
		{"ticket claim", ticketScope(func(h *ticketRPC, g string) string {
			return h.claim(ctx, discordoutgress.TicketClaimRequest{GuildID: g, ChannelID: "c2", MessageID: "m1"}).Error
		}, [2]string{"PATCH", "/channels/c2/messages/m1"})},
		{"ticket add", ticketScope(func(h *ticketRPC, g string) string {
			return h.add(ctx, discordoutgress.TicketMemberAddRequest{GuildID: g, ChannelID: "c2", UserID: "u1"}).Error
		}, [2]string{"PUT", "/channels/c2/permissions/u1"})},
		{"ticket close", ticketScope(func(h *ticketRPC, g string) string {
			return h.close(ctx, discordoutgress.TicketCloseRequest{GuildID: g, ChannelID: "c2"}).Error
		}, [2]string{"DELETE", "/channels/c2"}, [2]string{"PATCH", "/channels/c2"})},
	}
	for _, tc := range cases {
		for _, guildID := range []string{"g1", ""} {
			t.Run(tc.name+"/"+guildID, func(t *testing.T) {
				errText, touched := tc.refused(t, guildID)
				if errText == "" {
					t.Fatalf("%s allowed for guild %q", tc.name, guildID)
				}
				if touched {
					t.Fatalf("refused %s reached Discord", tc.name)
				}
			})
		}
	}
}
