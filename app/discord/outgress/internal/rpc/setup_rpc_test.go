// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
)

// fakeSetupREST is the setup worker's REST slice. Only the read half is
// scripted: nothing here fills a guild.
type fakeSetupREST struct {
	channels  []discapi.Snowflake
	guild     discapi.GuildInfo
	guildErr  error
	guildHits int
}

func (f *fakeSetupREST) SendChat(context.Context, discapi.ChatPost) error { return nil }

func (f *fakeSetupREST) SendPanel(context.Context, discapi.EmbedPost, []discapi.Button) (discapi.Message, error) {
	return discapi.Message{}, nil
}

func (f *fakeSetupREST) CreateChannel(context.Context, discapi.GuildChannel) (discapi.Snowflake, error) {
	return discapi.Snowflake{}, nil
}

func (f *fakeSetupREST) CreateRole(context.Context, discapi.GuildRole) (discapi.Snowflake, error) {
	return discapi.Snowflake{}, nil
}

func (f *fakeSetupREST) ListGuildChannels(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	return f.channels, nil
}

func (f *fakeSetupREST) ListGuildRoles(context.Context, discapi.Guild) ([]discapi.Snowflake, error) {
	return []discapi.Snowflake{{ID: "role-1", Name: "@everyone"}}, nil
}

func (f *fakeSetupREST) GetGuildWithCounts(context.Context, discapi.Guild) (discapi.GuildInfo, error) {
	f.guildHits++
	return f.guild, f.guildErr
}

type fakeBotStatus struct {
	st ddiscord.BotStatus
	ok bool
}

func (f fakeBotStatus) BotStatus(context.Context) (ddiscord.BotStatus, bool) { return f.st, f.ok }

// newDiscordRPC wires a handler over a bound guild (g1 -> b1) unless bind is
// false.
func newDiscordRPC(t *testing.T, rest *fakeSetupREST, status botStatusReader, bind bool) *discordRPC {
	t.Helper()
	store := discordstore.NewMem()
	if bind {
		if err := store.BindGuild(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Broadcaster{ID: "b1"}); err != nil {
			t.Fatal(err)
		}
	}
	return &discordRPC{w: setup.New(setup.Config{Discord: rest, Store: store}), status: status}
}

func TestHandleStatusReportsSessionAndGuildSeparately(t *testing.T) {
	rest := &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ", Icon: "abc", ApproximateMemberCount: 42}}
	status := fakeBotStatus{ok: true, st: ddiscord.BotStatus{
		Connected: true, SinceUnixMS: 1700, Resumes: 3, LastCloseCode: 4009,
	}}
	d := newDiscordRPC(t, rest, status, true)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	if !got.Online || got.SinceUnixMS != 1700 || got.SessionResumes != 3 {
		t.Fatalf("session fields = %+v", got)
	}
	if !got.GuildPresent || got.GuildName != "Bagel HQ" || got.MemberCount != 42 {
		t.Fatalf("guild fields = %+v", got)
	}
	if got.IconURL != "https://cdn.discordapp.com/icons/g1/abc.png" {
		t.Fatalf("icon_url = %q, want a CDN url the console never has to assemble", got.IconURL)
	}
	// 4009 is history on a connected bot, not a fault, but it still ships:
	// it is what explains the resume count.
	if got.LastCloseCode != 4009 || got.Code != outgressrpc.CodeOK || got.Error != "" {
		t.Fatalf("reply = %+v, want no error", got)
	}
}

func TestHandleStatusUnboundGuildCarriesNotBoundCode(t *testing.T) {
	d := newDiscordRPC(t, &fakeSetupREST{}, fakeBotStatus{}, false)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	if got.Code != outgressrpc.CodeNotBound {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeNotBound)
	}
	// The message is the console's old substring contract and must not
	// change in the release that introduces the code.
	if got.Error != setup.ErrGuildBoundElsewhere.Error() {
		t.Fatalf("error = %q, want the unchanged message text", got.Error)
	}
	if got.GuildPresent {
		t.Fatal("an unbound guild must not report present")
	}
}

func TestHandleStatusRejectsMissingFields(t *testing.T) {
	d := newDiscordRPC(t, &fakeSetupREST{}, fakeBotStatus{}, true)
	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{GuildID: "g1"})
	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
}

func TestHandleStatusWithoutAStatusKeyReportsOffline(t *testing.T) {
	// Nil reader: outgress genuinely does not know, and saying "offline with
	// no close code" is the honest answer, not an error.
	d := newDiscordRPC(t, &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "HQ"}}, nil, true)
	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})
	if got.Online || got.LastCloseCode != 0 || got.Error != "" {
		t.Fatalf("reply = %+v", got)
	}
	if !got.GuildPresent {
		t.Fatal("a missing status key must not cost the guild lookup")
	}
}

func TestHandleLayoutSplitsCategoriesAndCarriesBotFields(t *testing.T) {
	rest := &fakeSetupREST{
		channels: []discapi.Snowflake{
			{ID: "cat-1", Name: "Community", Type: ddiscord.ChannelCategory},
			{ID: "ch-1", Name: "general", Type: ddiscord.ChannelText},
			{ID: "vc-1", Name: "Voice", Type: 2},
		},
		guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ", ApproximateMemberCount: 9},
	}
	status := fakeBotStatus{ok: true, st: ddiscord.BotStatus{Connected: true, SinceUnixMS: 99, LastCloseCode: 4000}}
	d := newDiscordRPC(t, rest, status, true)

	got := d.handleLayout(context.Background(), outgressrpc.DiscordLayoutRequest{UserID: "b1", GuildID: "g1"})

	if len(got.Categories) != 1 || got.Categories[0].ID != "cat-1" {
		t.Fatalf("categories = %+v", got.Categories)
	}
	// Text and voice both stay in Channels; only categories move out.
	if len(got.Channels) != 2 {
		t.Fatalf("channels = %+v, want the two non-category channels", got.Channels)
	}
	if !got.BotOnline || got.BotSinceUnixMS != 99 || got.LastCloseCode != 4000 {
		t.Fatalf("bot fields = %+v", got)
	}
	if got.Guild == nil || got.Guild.Name != "Bagel HQ" || got.Guild.MemberCount != 9 {
		t.Fatalf("guild = %+v", got.Guild)
	}
}

func TestHandleLayoutSurvivesAFailedGuildLookup(t *testing.T) {
	rest := &fakeSetupREST{
		channels: []discapi.Snowflake{{ID: "ch-1", Name: "general", Type: ddiscord.ChannelText}},
		guildErr: discapi.ErrForbidden,
	}
	d := newDiscordRPC(t, rest, fakeBotStatus{}, true)

	got := d.handleLayout(context.Background(), outgressrpc.DiscordLayoutRequest{UserID: "b1", GuildID: "g1"})

	// The pickers are the point of this call; one failing lookup must not
	// take the channel list with it.
	if got.Guild != nil {
		t.Fatalf("guild = %+v, want nil", got.Guild)
	}
	if len(got.Channels) != 1 || got.Error != "" {
		t.Fatalf("reply = %+v", got)
	}
}

func TestCodeForMapsEveryDashboardFailure(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{nil, outgressrpc.CodeOK},
		{setup.ErrGuildNotBound, outgressrpc.CodeNotBound},
		{setup.ErrGuildBoundElsewhere, outgressrpc.CodeBoundElsewhere},
		{setup.ErrDiscordUnavailable, outgressrpc.CodeDiscordUnavailable},
		{discapi.ErrAuth, outgressrpc.CodeDiscordUnavailable},
		{discapi.ErrForbidden, outgressrpc.CodeForbidden},
		{discapi.ErrRateLimited, outgressrpc.CodeRateLimited},
		{discapi.ErrBadRequest, outgressrpc.CodeInvalid},
		{discapi.ErrChannelNotFound, outgressrpc.CodeNotFound},
		{context.DeadlineExceeded, outgressrpc.CodeTimeout},
		{context.Canceled, outgressrpc.CodeTimeout},
		{fmt.Errorf("wrapped: %w", discapi.ErrChannelNotFound), outgressrpc.CodeNotFound},
		// The catch-all. An unclassified failure must never answer CodeOK:
		// the console reads "" as success and would render an error reply as
		// a completed action.
		{errors.New("discord: something nobody has classified"), outgressrpc.CodeUnknown},
	}
	for _, tc := range cases {
		if got := codeFor(tc.err); got != tc.want {
			t.Fatalf("codeFor(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	// ErrGuildNotBound wraps ErrGuildBoundElsewhere, so the order of the
	// switch is load-bearing: the wrapped case must be checked first.
	if codeFor(setup.ErrGuildNotBound) == codeFor(setup.ErrGuildBoundElsewhere) {
		t.Fatal("not_bound and bound_elsewhere collapsed into one code")
	}
}

// TestBotOnlineNeedsAFreshHeartbeat is the shared-definition test: the
// layout reply and the status reply must agree on what "online" means, and
// connected:true alone is not it. The status key has no TTL, so an ingress
// that was killed mid-session leaves connected:true behind forever; without
// the staleness check the dashboard pill stayed green for a bot that no
// longer existed.
func TestBotOnlineNeedsAFreshHeartbeat(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name string
		st   ddiscord.BotStatus
		want bool
	}{
		{"connected and beating", ddiscord.BotStatus{
			Connected: true, HeartbeatUnixMS: now.Add(-time.Second).UnixMilli(),
		}, true},
		{"connected but the writer died", ddiscord.BotStatus{
			Connected: true, HeartbeatUnixMS: now.Add(-ddiscord.BotHeartbeatMaxAge - time.Second).UnixMilli(),
		}, false},
		{"not connected", ddiscord.BotStatus{
			Connected: false, HeartbeatUnixMS: now.UnixMilli(),
		}, false},
		// No heartbeat at all is a status written before the first beat, not
		// a stale one: HeartbeatStale reads 0 as "no evidence".
		{"connected, no beat yet", ddiscord.BotStatus{Connected: true}, true},
	}
	for _, tc := range cases {
		if got := botOnline(tc.st, now); got != tc.want {
			t.Fatalf("%s: botOnline = %t, want %t", tc.name, got, tc.want)
		}
	}
}
