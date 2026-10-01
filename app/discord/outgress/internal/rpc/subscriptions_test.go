// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"net/http"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func rpcWiring(t *testing.T) Wiring {
	t.Helper()
	return Wiring{NC: testnats.Connect(t), Prefix: "test.discord", Queue: "discord", Log: zap.NewNop()}
}

func requestRPC(t *testing.T, wire Wiring, verb, body string) []byte {
	t.Helper()
	reply, err := wire.NC.Request(wire.Prefix+"."+verb, []byte(body), 2*time.Second)
	require.NoError(t, err)
	return reply.Data
}

func TestSubscribeEngineRoutesRequestsToDiscord(t *testing.T) {
	wire := rpcWiring(t)
	tr := &scriptedTransport{channelGuilds: map[string]string{"live1": "g1", "live2": "g2", "old1": "g1", "c1": "g1"}, reply: func(call recordedCall) (int, string) {
		switch call.path {
		case "/channels/c1/messages":
			if call.method == http.MethodGet {
				return 200, `[{"id":"m1"},{"id":"m2"}]`
			}
		case "/invites/bagel":
			return 200, `{"guild":{"id":"g1"}}`
		}
		return 200, `{"id":"new1"}`
	}}
	client := discapi.NewClient("bot-token")
	client.SetTransport(tr)
	live := newMemLive()
	require.NoError(t, SubscribeEngine(client, live, wire))

	for _, tc := range []struct {
		verb, request, reply, method, path, body, query string
	}{
		{verb: "channel.create", request: `{"guild_id":"g1","name":"voice","type":2,"parent_id":"cat1","topic":"Welcome","overwrites":[{"id":"u1","type":1,"allow":"1024","deny":"0"}]}`,
			reply: `{"channel_id":"new1"}`, method: http.MethodPost, path: "/guilds/g1/channels",
			body: `{"name":"voice","type":2,"parent_id":"cat1","topic":"Welcome","permission_overwrites":[{"id":"u1","type":1,"allow":"1024","deny":"0"}]}`},
		{verb: "channel.delete", request: `{"guild_id":"g1","channel_id":"old1"}`, reply: `{}`, method: http.MethodDelete, path: "/channels/old1"},
		{verb: "channel.modify", request: `{"guild_id":"g1","channel_id":"c1","name":"renamed","user_limit":4,"overwrites":[]}`, reply: `{}`,
			method: http.MethodPatch, path: "/channels/c1", body: `{"name":"renamed","user_limit":4,"permission_overwrites":[]}`},
		{verb: "member.move", request: `{"guild_id":"g1","user_id":"u1","channel_id":"voice1"}`, reply: `{}`,
			method: http.MethodPatch, path: "/guilds/g1/members/u1", body: `{"channel_id":"voice1"}`},
		{verb: "member.move", request: `{"guild_id":"g1","user_id":"u1","channel_id":""}`, reply: `{}`,
			method: http.MethodPatch, path: "/guilds/g1/members/u1", body: `{"channel_id":null}`},
		{verb: "channel.purge", request: `{"guild_id":"g1","channel_id":"c1","count":50}`, reply: `{"deleted":2}`,
			method: http.MethodPost, path: "/channels/c1/messages/bulk-delete", body: `{"messages":["m1","m2"]}`},
		{verb: "live.online", request: `{"guild_id":"g1","channel_id":"live1","embed":{"title":"Live now","url":"https://twitch.tv/bagel","color":7}}`, reply: `{}`,
			method: http.MethodPost, path: "/channels/live1/messages", body: `{"embeds":[{"title":"Live now","url":"https://twitch.tv/bagel","color":7}]}`},
		{verb: "live.offline", request: `{"guild_id":"g2"}`, reply: `{}`,
			method: http.MethodPatch, path: "/channels/live2/messages/tracked1", body: `{"content":"Stream ended.","embeds":[]}`},
		{verb: "invite.resolve", request: `{"code":"bagel"}`, reply: `{"guild_id":"g1"}`,
			method: http.MethodGet, path: "/invites/bagel", query: "with_counts=false"},
	} {
		t.Run(tc.verb, func(t *testing.T) {
			if tc.verb == "live.offline" {
				require.NoError(t, live.PutLiveMessage(context.Background(), "g2", discapi.Message{ChannelID: "live2", ID: "tracked1"}))
			}
			before := len(tr.find(tc.method, tc.path))
			require.JSONEq(t, tc.reply, string(requestRPC(t, wire, tc.verb, tc.request)))
			calls := tr.find(tc.method, tc.path)
			require.Len(t, calls, before+1)
			call := calls[before]
			require.Equal(t, tc.query, call.query)
			if tc.body != "" {
				require.JSONEq(t, tc.body, call.body)
			}
			if tc.verb == "channel.purge" {
				pages := tr.find(http.MethodGet, "/channels/c1/messages")
				require.Len(t, pages, 1)
				require.Equal(t, "limit=50", pages[0].query)
			}
		})
	}
}

func TestSubscribeSetupRejectsMutationsBeforeChangingState(t *testing.T) {
	wire := rpcWiring(t)
	ctx := context.Background()
	store := discordstore.NewMem()
	require.NoError(t, store.BindGuild(ctx, discordstore.Binding{
		Guild: discordstore.Guild{ID: rpcGuildID}, Broadcaster: discordstore.Broadcaster{ID: "42"},
	}))
	tr := &scriptedTransport{}
	client := discapi.NewClient("bot-token")
	client.SetTransport(tr)
	w := setup.New(setup.Config{Discord: client, Store: store, Log: zap.NewNop()})
	require.NoError(t, SubscribeSetup(w, SetupWiring{Wiring: wire}))

	for _, tc := range []struct{ name, verb, request, code string }{
		{"invalid pins", "discord.setup", `{"user_id":"42","guild_id":"` + rpcGuildID + `","pinned_roles":{"mods":"<@&12345>"}}`, "invalid"},
		{"foreign config write", "discord.config.set", `{"user_id":"99","guild_id":"` + rpcGuildID + `","config":{"liveChannelId":"injected"}}`, "not_bound"},
		{"foreign unbind", "discord.unbind", `{"user_id":"99","guild_id":"` + rpcGuildID + `"}`, "not_bound"},
		{"empty post", "discord.post", `{"channel_id":"c1","content":""}`, "invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var reply struct {
				Code  string
				Error string
			}
			require.NoError(t, codec.Unmarshal(requestRPC(t, wire, tc.verb, tc.request), &reply))
			require.Equal(t, tc.code, reply.Code)
			require.NotEmpty(t, reply.Error)
			owner, _, found := store.BindingOf(ctx, discordstore.Guild{ID: rpcGuildID})
			require.True(t, found)
			require.Equal(t, "42", owner.ID)
			_, _, saved, err := w.GuildConfig(ctx, setup.GuildSetupRequest{GuildID: rpcGuildID, BroadcasterID: "42"})
			require.NoError(t, err)
			require.False(t, saved)
			require.Empty(t, tr.calls, "rejected requests must not call Discord")
		})
	}
	require.JSONEq(t, `{"code":""}`, string(requestRPC(t, wire, "discord.post", `{"channel_id":"c1","content":"hello"}`)))
	posts := tr.find(http.MethodPost, "/channels/c1/messages")
	require.Len(t, posts, 1)
	require.JSONEq(t, `{"content":"hello"}`, posts[0].body)
	require.JSONEq(t, `{"code":""}`, string(requestRPC(t, wire, "discord.unbind", `{"user_id":"42","guild_id":"`+rpcGuildID+`"}`)))
	_, _, found := store.BindingOf(ctx, discordstore.Guild{ID: rpcGuildID})
	require.False(t, found)
}
