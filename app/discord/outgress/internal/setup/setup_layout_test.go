// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestGuildLayout(t *testing.T) {
	yes, no := true, false
	everyone := setup.GuildEntry{ID: "guild-1", Name: "@everyone"}
	cases := []struct {
		name     string
		caller   string
		botID    string
		channels []discapi.Snowflake
		want     setup.GuildLayout
		wantErr  error
	}{{
		name:     "refuses a broadcaster who does not own the guild",
		caller:   "7",
		channels: []discapi.Snowflake{{ID: "c1", Name: "general"}},
		wantErr:  setup.ErrNotBound,
	}, {
		name:     "lists the channels and roles for the owner",
		caller:   "42",
		channels: []discapi.Snowflake{{ID: "c1", Name: "general"}},
		want:     setup.GuildLayout{Channels: []setup.GuildEntry{{ID: "c1", Name: "general"}}, Roles: []setup.GuildEntry{everyone}},
	}, {
		name:   "flags which text channels the bot can post and embed in",
		caller: "42",
		botID:  "bot",
		channels: []discapi.Snowflake{
			{ID: "c1", Name: "general"},
			{ID: "c2", Name: "quiet", PermissionOverwrites: []discapi.PermissionOverwrite{{ID: "guild-1", Allow: "0", Deny: "2048"}}},
			{ID: "c3", Name: "plain", PermissionOverwrites: []discapi.PermissionOverwrite{{ID: "guild-1", Allow: "0", Deny: "16384"}}},
			{ID: "c4", Name: "lobby", Type: ddiscord.ChannelVoice},
		},
		want: setup.GuildLayout{Channels: []setup.GuildEntry{
			{ID: "c1", Name: "general", CanSend: &yes, CanEmbed: &yes},
			{ID: "c2", Name: "quiet", CanSend: &no, CanEmbed: &no},
			{ID: "c3", Name: "plain", CanSend: &yes, CanEmbed: &no},
			{ID: "c4", Name: "lobby", Type: ddiscord.ChannelVoice},
		}, Roles: []setup.GuildEntry{everyone}},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{channels: tc.channels, everyonePerms: "19456"}
			w := setup.New(setup.Config{Discord: d, Store: boundStore(owners{"guild-1": "42"}), Log: zap.NewNop(), BotID: tc.botID})

			got, err := w.GuildLayout(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: tc.caller})

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestUnbindGuild(t *testing.T) {
	cases := []struct {
		name    string
		caller  string
		wantErr error
		bound   string
	}{
		{name: "refuses a broadcaster who does not own the guild", caller: "7", wantErr: setup.ErrNotBound, bound: "42"},
		{name: "unbinds the owner's guild", caller: "42"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := boundStore(owners{"guild-1": "42"})

			err := newWorker(&fakeDiscord{}, store).UnbindGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: tc.caller})

			require.ErrorIs(t, err, tc.wantErr)
			owner, _ := store.Broadcaster(context.Background(), discordstore.Guild{ID: "guild-1"})
			require.Equal(t, tc.bound, owner.ID)
		})
	}
}

func TestUnbindStaysIdempotent(t *testing.T) {
	w := newWorker(&fakeDiscord{}, boundStore(owners{"guild-1": "42"}))
	ctx := context.Background()
	req := setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"}

	require.NoError(t, w.UnbindGuild(ctx, req), "first unbind")
	require.NoError(t, w.UnbindGuild(ctx, req), "second unbind, want the same silence")
	other := setup.GuildSetupRequest{GuildID: "guild-9", BroadcasterID: "42"}
	require.NoError(t, w.UnbindGuild(ctx, other), "unbinding an unknown guild")
}

func workerFor(d *fakeDiscord, store discordstore.Store, offline bool) *setup.Worker {
	if offline {
		return setup.New(setup.Config{Store: store, Log: zap.NewNop()})
	}
	return newWorker(d, store)
}

func TestPostDiscordCachesTheChannelCheckUntilDiscordSays404(t *testing.T) {
	d := &fakeDiscord{}
	w := workerFor(d, boundStore(owners{"guild-1": "42", "guild-2": "42"}), false)
	post := setup.PostRequest{BroadcasterID: "42", GuildID: "guild-1", ChannelID: "chan", Content: "hi"}
	ctx := context.Background()

	require.NoError(t, w.PostDiscord(ctx, post))
	require.Eventually(t, func() bool {
		reads := d.channelReads
		return w.PostDiscord(ctx, post) == nil && d.channelReads == reads
	}, 2*time.Second, 5*time.Millisecond, "a repeat post reuses the positive check")

	other := post
	other.GuildID = "guild-2"
	require.ErrorIs(t, w.PostDiscord(ctx, other), discapi.ErrForbidden, "a cached channel still refuses another guild")

	d.chatErr = discapi.ErrChannelNotFound
	require.ErrorIs(t, w.PostDiscord(ctx, post), discapi.ErrChannelNotFound)
	d.chatErr = nil
	reads := d.channelReads
	require.NoError(t, w.PostDiscord(ctx, post))
	require.Equal(t, reads+1, d.channelReads, "a 404 forgets the channel")
}

func TestPostDiscord(t *testing.T) {
	longest := strings.Repeat("é", 2000)
	mine := func(channel, content string) setup.PostRequest {
		return setup.PostRequest{BroadcasterID: "42", GuildID: "guild-1", ChannelID: channel, Content: content}
	}
	cases := []struct {
		name          string
		post          setup.PostRequest
		offline       bool
		channelGuilds map[string]string
		wantErr       error
		want          []discapi.ChatPost
	}{
		{name: "posts content up to the 2000 rune limit with every mention off", post: mine("chan", longest),
			want: []discapi.ChatPost{{ChannelID: "chan", Content: longest, NoMentions: true}}},
		{name: "requires a channel", post: mine("", "hi"), wantErr: discapi.ErrBadRequest},
		{name: "requires content", post: mine("chan", ""), wantErr: discapi.ErrBadRequest},
		{name: "refuses content past the rune limit", post: mine("chan", longest+"é"), wantErr: discapi.ErrBadRequest},
		{name: "requires a discord client", post: mine("chan", "hi"), offline: true, wantErr: discapi.ErrAuth},
		{name: "refuses a guild the broadcaster does not own",
			post: setup.PostRequest{BroadcasterID: "7", GuildID: "guild-1", ChannelID: "chan", Content: "hi"}, wantErr: setup.ErrNotBound},
		{name: "refuses a guild nobody bound",
			post: setup.PostRequest{BroadcasterID: "42", GuildID: "guild-9", ChannelID: "chan", Content: "hi"}, wantErr: setup.ErrNotBound},
		{name: "refuses a channel of another guild", post: mine("elsewhere", "hi"),
			channelGuilds: map[string]string{"elsewhere": "guild-2"}, wantErr: discapi.ErrForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{channelGuilds: tc.channelGuilds}

			err := workerFor(d, boundStore(owners{"guild-1": "42"}), tc.offline).PostDiscord(context.Background(), tc.post)

			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.want, d.chats)
		})
	}
}
