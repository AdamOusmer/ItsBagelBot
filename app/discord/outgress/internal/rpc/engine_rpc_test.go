// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	discapi "ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var (
	refusedByDiscord = answer{status: http.StatusForbidden, body: "Missing Permissions"}
	twoMessages      = answer{status: http.StatusOK, body: `[{"id":"m1"},{"id":"m2"}]`}
	liveMessage      = map[kv.GuildID]discapi.Message{"g1": {ChannelID: "live1", ID: "tracked1"}}
)

const refusedError = "discord: forbidden: Missing Permissions"

func serveEngine(rest *discapi.Client, live kv.LiveStore, wire Wiring) error {
	return SubscribeEngine(rest, live, wire)
}

func channelCases() []discordCase {
	overwrites := []discapi.PermissionOverwrite{{ID: "u1", Type: 1, Allow: "1024", Deny: "0"}}
	return []discordCase{{
		name: "creates a channel with the requested spec",
		verb: "channel.create",
		req: discordoutgress.ChannelCreateRequest{
			GuildID: "g1", Name: "voice", Type: 2, ParentID: "cat1", Topic: "Welcome", Overwrites: overwrites,
		},
		want:  discordoutgress.ChannelCreateReply{ChannelID: "m-new"},
		calls: []string{"POST /guilds/g1/channels"},
		write: "POST /guilds/g1/channels",
		body:  `{"name":"voice","type":2,"parent_id":"cat1","topic":"Welcome","permission_overwrites":[{"id":"u1","type":1,"allow":"1024","deny":"0"}]}`,
	}, {
		name:   "reports a refused channel create",
		verb:   "channel.create",
		req:    discordoutgress.ChannelCreateRequest{GuildID: "g1", Name: "voice"},
		routes: map[string]answer{"POST /guilds/g1/channels": refusedByDiscord},
		want:   discordoutgress.ChannelCreateReply{Error: refusedError},
		calls:  []string{"POST /guilds/g1/channels"},
	}, {
		name:  "deletes a channel of the guild",
		verb:  "channel.delete",
		req:   discordoutgress.ChannelDeleteRequest{GuildID: "g1", ChannelID: "old1"},
		want:  discordoutgress.ChannelDeleteReply{},
		calls: []string{"GET /channels/old1", "DELETE /channels/old1"},
	}, {
		name:   "reports a refused channel delete",
		verb:   "channel.delete",
		req:    discordoutgress.ChannelDeleteRequest{GuildID: "g1", ChannelID: "old1"},
		routes: map[string]answer{"DELETE /channels/old1": refusedByDiscord},
		want:   discordoutgress.ChannelDeleteReply{Error: refusedError},
		calls:  []string{"GET /channels/old1", "DELETE /channels/old1"},
	}, {
		name: "modifies a channel of the guild",
		verb: "channel.modify",
		req: discordoutgress.ChannelModifyRequest{
			GuildID: "g1", ChannelID: "c1", Name: "renamed", UserLimit: 4, Overwrites: overwrites,
		},
		want:  discordoutgress.ChannelModifyReply{},
		calls: []string{"GET /channels/c1", "PATCH /channels/c1"},
		write: "PATCH /channels/c1",
		body:  `{"name":"renamed","user_limit":4,"permission_overwrites":[{"id":"u1","type":1,"allow":"1024","deny":"0"}]}`,
	}}
}

func memberCases() []discordCase {
	return []discordCase{{
		name:  "moves a member into a voice channel",
		verb:  "member.move",
		req:   discordoutgress.MemberMoveRequest{GuildID: "g1", UserID: "u1", ChannelID: "voice1"},
		want:  discordoutgress.MemberMoveReply{},
		calls: []string{"PATCH /guilds/g1/members/u1"},
		write: "PATCH /guilds/g1/members/u1",
		body:  `{"channel_id":"voice1"}`,
	}, {
		name:  "disconnects a member when no channel is given",
		verb:  "member.move",
		req:   discordoutgress.MemberMoveRequest{GuildID: "g1", UserID: "u1"},
		want:  discordoutgress.MemberMoveReply{},
		calls: []string{"PATCH /guilds/g1/members/u1"},
		write: "PATCH /guilds/g1/members/u1",
		body:  `{"channel_id":null}`,
	}, {
		name:   "reports a refused member move",
		verb:   "member.move",
		req:    discordoutgress.MemberMoveRequest{GuildID: "g1", UserID: "u1", ChannelID: "voice1"},
		routes: map[string]answer{"PATCH /guilds/g1/members/u1": refusedByDiscord},
		want:   discordoutgress.MemberMoveReply{Error: refusedError},
		calls:  []string{"PATCH /guilds/g1/members/u1"},
	}}
}

func purgeCases() []discordCase {
	purge := discordoutgress.PurgeRequest{GuildID: "g1", ChannelID: "c1", Count: 50}
	listing := []string{"GET /channels/c1", "GET /channels/c1/messages?limit=50"}
	return []discordCase{{
		name:   "bulk-deletes the listed messages",
		verb:   "channel.purge",
		req:    purge,
		routes: map[string]answer{"GET /channels/c1/messages?limit=50": twoMessages},
		want:   discordoutgress.PurgeReply{Deleted: 2},
		calls:  append(listing, "POST /channels/c1/messages/bulk-delete"),
		write:  "POST /channels/c1/messages/bulk-delete",
		body:   `{"messages":["m1","m2"]}`,
	}, {
		name: "reports a lone message without a bulk delete",
		verb: "channel.purge",
		req:  purge,
		routes: map[string]answer{
			"GET /channels/c1/messages?limit=50": {status: http.StatusOK, body: `[{"id":"m1"}]`},
		},
		want:  discordoutgress.PurgeReply{Deleted: 1},
		calls: listing,
	}, {
		name: "reports a failed message listing",
		verb: "channel.purge",
		req:  purge,
		routes: map[string]answer{
			"GET /channels/c1/messages?limit=50": {status: http.StatusInternalServerError, body: "boom"},
		},
		want:  discordoutgress.PurgeReply{Error: "discord: api rejected request (500): boom"},
		calls: listing,
	}, {
		name: "reports a refused bulk delete",
		verb: "channel.purge",
		req:  purge,
		routes: map[string]answer{
			"GET /channels/c1/messages?limit=50":     twoMessages,
			"POST /channels/c1/messages/bulk-delete": refusedByDiscord,
		},
		want:  discordoutgress.PurgeReply{Error: refusedError},
		calls: append(listing, "POST /channels/c1/messages/bulk-delete"),
	}}
}

func liveCases() []discordCase {
	online := discordoutgress.LiveOnlineRequest{
		GuildID: "g1", ChannelID: "live1",
		Embed: ddiscord.Embed{Title: "Live now", URL: "https://twitch.tv/bagel", Color: 7},
	}
	offline := discordoutgress.LiveOfflineRequest{GuildID: "g1"}
	edit := []string{"GET /channels/live1", "PATCH /channels/live1/messages/tracked1"}
	return []discordCase{{
		name:    "posts the go-live embed and remembers it",
		verb:    "live.online",
		req:     online,
		want:    discordoutgress.LiveOnlineReply{},
		calls:   []string{"GET /channels/live1", "POST /channels/live1/messages"},
		write:   "POST /channels/live1/messages",
		body:    `{"embeds":[{"title":"Live now","url":"https://twitch.tv/bagel","color":7}]}`,
		tracked: map[kv.GuildID]discapi.Message{"g1": {ChannelID: "live1", ID: "m-new"}},
	}, {
		name:    "does not post again while the go-live message is remembered",
		verb:    "live.online",
		req:     online,
		live:    liveMessage,
		want:    discordoutgress.LiveOnlineReply{},
		tracked: liveMessage,
	}, {
		name:   "reports a refused go-live post and remembers nothing",
		verb:   "live.online",
		req:    online,
		routes: map[string]answer{"POST /channels/live1/messages": refusedByDiscord},
		want:   discordoutgress.LiveOnlineReply{Error: refusedError},
		calls:  []string{"GET /channels/live1", "POST /channels/live1/messages"},
	}, {
		name:  "edits the go-live message when the stream ends and forgets it",
		verb:  "live.offline",
		req:   offline,
		live:  liveMessage,
		want:  discordoutgress.LiveOfflineReply{},
		calls: edit,
		write: "PATCH /channels/live1/messages/tracked1",
		body:  `{"content":"Stream ended.","embeds":[]}`,
	}, {
		name: "ignores a stream end without a remembered message",
		verb: "live.offline",
		req:  offline,
		want: discordoutgress.LiveOfflineReply{},
	}, {
		name:   "forgets a go-live message Discord already deleted",
		verb:   "live.offline",
		req:    offline,
		live:   liveMessage,
		routes: map[string]answer{"PATCH /channels/live1/messages/tracked1": {status: http.StatusNotFound}},
		want:   discordoutgress.LiveOfflineReply{},
		calls:  edit,
	}, {
		name:    "keeps the go-live message for a retry while the edit is refused",
		verb:    "live.offline",
		req:     offline,
		live:    liveMessage,
		routes:  map[string]answer{"PATCH /channels/live1/messages/tracked1": refusedByDiscord},
		want:    discordoutgress.LiveOfflineReply{Error: refusedError},
		calls:   edit,
		tracked: liveMessage,
	}}
}

func inviteCases() []discordCase {
	return []discordCase{{
		name: "resolves an invite to its guild",
		verb: "invite.resolve",
		req:  discordoutgress.InviteResolveRequest{Code: "bagel"},
		routes: map[string]answer{
			"GET /invites/bagel?with_counts=false": {status: http.StatusOK, body: `{"guild":{"id":"g1"}}`},
		},
		want:  discordoutgress.InviteResolveReply{GuildID: "g1"},
		calls: []string{"GET /invites/bagel?with_counts=false"},
	}}
}

func TestEngineVerbsReachDiscordAndAnswer(t *testing.T) {
	cases := slices.Concat(channelCases(), memberCases(), purgeCases(), liveCases(), inviteCases())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected(t), tc.exchange(t, serveEngine))
		})
	}
}

func TestHandleInviteResolveReturnsTheTargetGuild(t *testing.T) {
	rest := &fakeEngineREST{inviteReply: discapi.Invite{Code: "abc", Guild: &discapi.Snowflake{ID: "g1"}}}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "abc"})
	if reply.Error != "" || reply.NotFound {
		t.Fatalf("reply = %+v, want a resolved guild", reply)
	}
	if reply.GuildID != "g1" {
		t.Fatalf("guild id = %q, want g1", reply.GuildID)
	}
	if len(rest.inviteCodes) != 1 || rest.inviteCodes[0] != "abc" {
		t.Fatalf("GetInvite called with %v, want [abc]", rest.inviteCodes)
	}
}

func TestHandleInviteResolve404IsNotFoundNotError(t *testing.T) {
	rest := &fakeEngineREST{inviteErr: discapi.ErrChannelNotFound}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "dead"})
	if !reply.NotFound || reply.Error != "" {
		t.Fatalf("reply = %+v, want NotFound with no Error", reply)
	}
}

func TestHandleInviteResolveGroupDMInviteIsNotFound(t *testing.T) {
	rest := &fakeEngineREST{inviteReply: discapi.Invite{Code: "dm1"}}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "dm1"})
	if !reply.NotFound || reply.GuildID != "" {
		t.Fatalf("reply = %+v, want NotFound with no guild id", reply)
	}
}

func TestHandleInviteResolveTransientErrorIsError(t *testing.T) {
	rest := &fakeEngineREST{inviteErr: errors.New("boom")}
	h := &engineRPC{rest: rest, log: zap.NewNop()}
	reply := h.handleInviteResolve(context.Background(), discordoutgress.InviteResolveRequest{Code: "x"})
	if reply.Error == "" || reply.NotFound {
		t.Fatalf("reply = %+v, want a non-empty Error and NotFound false", reply)
	}
}
