// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

// configRPCFor wires the handlers over a memory store holding guildIDs bound
// to broadcaster 42. The REST client is nil on purpose: these tests are about
// the reply codes the console switches on, and nothing here calls Discord.
func configRPCFor(t *testing.T, guildIDs ...string) *discordRPC {
	t.Helper()
	store := discordstore.NewMem()
	for _, id := range guildIDs {
		if err := store.BindGuild(context.Background(), discordstore.Binding{Guild: discordstore.Guild{ID: id}, Broadcaster: discordstore.Broadcaster{ID: "42"}}); err != nil {
			t.Fatalf("BindGuild: %v", err)
		}
	}
	w := setup.New(setup.Config{Store: store, Log: zap.NewNop()})
	return &discordRPC{w: w, log: zap.NewNop()}
}

func TestHandleConfigSetAndGet(t *testing.T) {
	d := configRPCFor(t, "guild-1")
	ctx := context.Background()

	set := d.handleConfigSet(ctx, outgressrpc.DiscordConfigSetRequest{
		UserID: "42", GuildID: "guild-1",
		Config: ddiscord.Config{LiveChannelID: "123"},
	})
	if set.Code != outgressrpc.DiscordCodeOK || set.Version != 1 {
		t.Fatalf("save: %+v", set)
	}

	got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "42", GuildID: "guild-1"})
	if got.Code != outgressrpc.DiscordCodeOK || !got.Found || got.Config.LiveChannelID != "123" {
		t.Fatalf("read back: %+v", got)
	}
}

func TestHandleConfigSetReportsConflict(t *testing.T) {
	d := configRPCFor(t, "guild-1")
	ctx := context.Background()
	req := outgressrpc.DiscordConfigSetRequest{UserID: "42", GuildID: "guild-1"}

	if reply := d.handleConfigSet(ctx, req); reply.Code != outgressrpc.DiscordCodeOK {
		t.Fatalf("first save: %+v", reply)
	}
	// The same expected_version again: the page is out of date.
	if reply := d.handleConfigSet(ctx, req); reply.Code != outgressrpc.DiscordCodeConflict {
		t.Fatalf("want conflict, got %+v", reply)
	}
}

func TestHandleConfigReportsNotBound(t *testing.T) {
	d := configRPCFor(t, "guild-1")
	ctx := context.Background()

	got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "99", GuildID: "guild-1"})
	if got.Code != outgressrpc.DiscordCodeNotBound {
		t.Fatalf("want not_bound for another broadcaster's guild, got %+v", got)
	}
	set := d.handleConfigSet(ctx, outgressrpc.DiscordConfigSetRequest{UserID: "42", GuildID: "guild-9"})
	if set.Code != outgressrpc.DiscordCodeNotBound {
		t.Fatalf("want not_bound for an unbound guild, got %+v", set)
	}
}

func TestHandleConfigRejectsMissingIDs(t *testing.T) {
	d := configRPCFor(t)
	ctx := context.Background()

	if got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "42"}); got.Code != outgressrpc.DiscordCodeInvalid {
		t.Fatalf("want invalid, got %+v", got)
	}
	if got := d.handleGuildsList(ctx, outgressrpc.DiscordGuildsListRequest{}); got.Code != outgressrpc.DiscordCodeInvalid {
		t.Fatalf("want invalid, got %+v", got)
	}
}

func TestHandleGuildsListReturnsEveryBinding(t *testing.T) {
	d := configRPCFor(t, "guild-1", "guild-2")

	got := d.handleGuildsList(context.Background(), outgressrpc.DiscordGuildsListRequest{UserID: "42"})
	if got.Code != outgressrpc.DiscordCodeOK || len(got.Guilds) != 2 {
		t.Fatalf("want two servers, got %+v", got)
	}
	// No REST client is wired, so nothing could confirm the bot is there.
	if got.Guilds[0].BotPresent {
		t.Fatalf("want bot_present false with no Discord client, got %+v", got.Guilds[0])
	}
}

// TestHandleGuildsListSaysTimeoutAndKeepsThePartial: a deadline reached
// part-way leaves a list the dashboard can still render. The code says it is
// short; an empty reply would have said the streamer connected nothing.
func TestHandleGuildsListSaysTimeoutAndKeepsThePartial(t *testing.T) {
	d := configRPCFor(t, "guild-1", "guild-2")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := d.handleGuildsList(ctx, outgressrpc.DiscordGuildsListRequest{UserID: "42"})
	if got.Code != outgressrpc.DiscordCodeTimeout {
		t.Fatalf("want the timeout code, got %+v", got)
	}
	if got.Guilds == nil {
		t.Fatal("a partial listing must still travel")
	}
}
