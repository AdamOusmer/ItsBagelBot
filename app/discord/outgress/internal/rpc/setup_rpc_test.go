// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/internal/domain/rpc"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/kv"
	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"go.uber.org/zap"
)

type fakeSetupREST struct {
	channels  []discapi.Snowflake
	guild     discapi.GuildInfo
	guildErr  error
	guildHits int
	panelID   string
}

func (f *fakeSetupREST) SendChat(context.Context, discapi.ChatPost) error { return nil }

func (f *fakeSetupREST) DeleteMessage(context.Context, discapi.Message) error { return nil }

func (f *fakeSetupREST) SendPanel(_ context.Context, post discapi.EmbedPost, _ []discapi.Button) (discapi.Message, error) {
	return discapi.Message{ChannelID: post.ChannelID, ID: f.panelID}, nil
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

func newDiscordRPC(t *testing.T, rest *fakeSetupREST, status botStatusReader, bind bool) *discordRPC {
	t.Helper()
	store := discordstore.NewMem()
	if bind {
		if err := store.BindGuild(context.Background(), discordstore.Binding{
			Guild: discordstore.Guild{ID: "g1"}, Broadcaster: discordstore.Broadcaster{ID: "b1"},
		}); err != nil {
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

	wantReplyField(t, "online", got.Online, true)
	wantReplyField(t, "since", got.SinceUnixMS, int64(1700))
	wantReplyField(t, "session resumes", got.SessionResumes, 3)
	wantReplyField(t, "guild present", got.GuildPresent, true)
	wantReplyField(t, "guild name", got.GuildName, "Bagel HQ")
	wantReplyField(t, "member count", got.MemberCount, 42)
	wantReplyField(t, "icon url", got.IconURL, "https://cdn.discordapp.com/icons/g1/abc.png")
	wantReplyField(t, "last close code", got.LastCloseCode, 4009)
	wantReplyField(t, "code", got.Code, outgressrpc.CodeOK)
	wantReplyField(t, "error", got.Error, "")
}

func wantReplyField(t *testing.T, name string, got, want any) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestHandleStatusUnboundGuildCarriesNotBoundCode(t *testing.T) {
	d := newDiscordRPC(t, &fakeSetupREST{}, fakeBotStatus{}, false)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	if got.Code != outgressrpc.CodeNotBound {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeNotBound)
	}
	if got.Error != setup.ErrNotBound.Error() {
		t.Fatalf("error = %q, want the not-bound message", got.Error)
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
	d := newDiscordRPC(t, &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "HQ"}}, nil, true)
	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})
	wantReplyField(t, "online", got.Online, false)
	wantReplyField(t, "last close code", got.LastCloseCode, 0)
	wantReplyField(t, "error", got.Error, "")
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

	wantReplyField(t, "categories", len(got.Categories), 1)
	wantReplyField(t, "category id", got.Categories[0].ID, "cat-1")
	wantReplyField(t, "channels", len(got.Channels), 2)
	wantReplyField(t, "bot online", got.BotOnline, true)
	wantReplyField(t, "bot since", got.BotSinceUnixMS, int64(99))
	wantReplyField(t, "last close code", got.LastCloseCode, 4000)
	if got.Guild == nil {
		t.Fatal("guild = nil, want the with-counts lookup carried alongside the pickers")
	}
	wantReplyField(t, "guild name", got.Guild.Name, "Bagel HQ")
	wantReplyField(t, "member count", got.Guild.MemberCount, 9)
}

func TestHandleLayoutSurvivesAFailedGuildLookup(t *testing.T) {
	rest := &fakeSetupREST{
		channels: []discapi.Snowflake{{ID: "ch-1", Name: "general", Type: ddiscord.ChannelText}},
		guildErr: discapi.ErrForbidden,
	}
	d := newDiscordRPC(t, rest, fakeBotStatus{}, true)

	got := d.handleLayout(context.Background(), outgressrpc.DiscordLayoutRequest{UserID: "b1", GuildID: "g1"})

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
		want rpc.Code
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
		{errors.New("discord: something nobody has classified"), outgressrpc.CodeUnknown},
	}
	for _, tc := range cases {
		if got := codeFor(tc.err); got != tc.want {
			t.Fatalf("codeFor(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
	if codeFor(setup.ErrGuildNotBound) == codeFor(setup.ErrGuildBoundElsewhere) {
		t.Fatal("not_bound and bound_elsewhere collapsed into one code")
	}
}

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
		{"connected, no beat yet", ddiscord.BotStatus{Connected: true}, true},
	}
	for _, tc := range cases {
		if got := botOnline(tc.st, now); got != tc.want {
			t.Fatalf("%s: botOnline = %t, want %t", tc.name, got, tc.want)
		}
	}
}

func TestHandleStatusCarriesTheConnectBudget(t *testing.T) {
	rest := &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ"}}
	status := fakeBotStatus{ok: true, st: ddiscord.BotStatus{
		LastCloseCode: 4000, Flapping: true, ConnectsInWindow: 800,
		AtCeiling: true, ParkUntilUnixMS: 1_700_000_000_000,
	}}
	d := newDiscordRPC(t, rest, status, true)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	wantReplyField(t, "flapping", got.Flapping, true)
	wantReplyField(t, "connects in window", got.ConnectsInWindow, 800)
	wantReplyField(t, "at ceiling", got.AtCeiling, true)
	if got.ParkUntilUnixMS != 1_700_000_000_000 {
		t.Fatalf("park_until_unix_ms = %d, want the key's deadline", got.ParkUntilUnixMS)
	}
	if got.Online {
		t.Fatalf("reply = %+v, want offline: a parked ingress holds no session", got)
	}
}

func TestHandleStatusOmitsAnUnpressuredBudget(t *testing.T) {
	rest := &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ"}}
	status := fakeBotStatus{ok: true, st: ddiscord.BotStatus{Connected: true, ConnectsInWindow: 4}}
	d := newDiscordRPC(t, rest, status, true)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	wantReplyField(t, "flapping", got.Flapping, false)
	wantReplyField(t, "at ceiling", got.AtCeiling, false)
	wantReplyField(t, "park until", got.ParkUntilUnixMS, int64(0))
	if got.ConnectsInWindow != 4 {
		t.Fatalf("connects_in_window = %d, want 4 published even when healthy", got.ConnectsInWindow)
	}
}

func setupRPC() *discordRPC {
	w := setup.New(setup.Config{Store: discordstore.NewMem(), Log: zap.NewNop()})
	return &discordRPC{w: w, log: zap.NewNop()}
}

const rpcGuildID = "100000000000000001"

func TestSetupRefusesAPinnedRoleThatIsNotASnowflake(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: rpcGuildID,
		PinnedRoles: map[string]string{ddiscord.SlotMods: "<@&12345>"},
	})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
	if !slices.Equal(got.Fields, []string{"pinnedRoles"}) {
		t.Fatalf("fields = %v, want [pinnedRoles]", got.Fields)
	}
	if got.Error == "" {
		t.Fatal("a refusal must still carry prose for the release that predates code")
	}
}

func TestSetupRefusesAnUnknownPinnedSlot(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: rpcGuildID,
		PinnedRoles: map[string]string{"janitor": "100000000000000002"},
	})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
}

func TestHandleDeskRepostAnswersWithTheNewMessageID(t *testing.T) {
	d := newDiscordRPC(t, &fakeSetupREST{panelID: "m-new"}, fakeBotStatus{}, true)

	got := d.handleDeskRepost(context.Background(), outgressrpc.DiscordDeskRepostRequest{
		UserID: "b1", GuildID: "g1", ChannelID: "support",
		Panel: outgressrpc.DiscordPanelSpec{Title: "Need a hand?", Color: intPtr(0x112233), Button: "Contact staff"},
	})

	if got.Error != "" || got.Code != outgressrpc.CodeOK {
		t.Fatalf("reply = %+v", got)
	}
	if got.MessageID != "m-new" {
		t.Fatalf("message id = %q", got.MessageID)
	}
}

func TestHandleDeskRepostRejectsAMissingGuild(t *testing.T) {
	d := newDiscordRPC(t, &fakeSetupREST{}, fakeBotStatus{}, true)

	got := d.handleDeskRepost(context.Background(), outgressrpc.DiscordDeskRepostRequest{UserID: "b1"})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
}

func TestSetupRefusesAGuildIDThatIsNotASnowflake(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: "my-server",
	})

	if got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeInvalid)
	}
	if !slices.Equal(got.Fields, []string{"guildId"}) {
		t.Fatalf("fields = %v, want [guildId]", got.Fields)
	}
}

func TestSetupAcceptsWellFormedPins(t *testing.T) {
	got := setupRPC().handleSetup(context.Background(), outgressrpc.DiscordSetupRequest{
		UserID: "42", GuildID: rpcGuildID,
		PinnedRoles: map[string]string{ddiscord.SlotMods: "100000000000000002"},
	})

	if got.Code == outgressrpc.CodeInvalid {
		t.Fatalf("a well-formed request was refused: %+v", got)
	}
	if got.Error == "" {
		t.Fatal("the fill should have failed with no discord client")
	}
}

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
	wantReplyField(t, "save code", set.Code, outgressrpc.CodeOK)
	wantReplyField(t, "save version", set.Version, 1)

	got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "42", GuildID: "guild-1"})
	wantReplyField(t, "read code", got.Code, outgressrpc.CodeOK)
	wantReplyField(t, "found", got.Found, true)
	wantReplyField(t, "live channel", got.Config.LiveChannelID, "123")
}

func TestHandleConfigSetReportsConflict(t *testing.T) {
	d := configRPCFor(t, "guild-1")
	ctx := context.Background()
	req := outgressrpc.DiscordConfigSetRequest{UserID: "42", GuildID: "guild-1"}

	if reply := d.handleConfigSet(ctx, req); reply.Code != outgressrpc.CodeOK {
		t.Fatalf("first save: %+v", reply)
	}
	if reply := d.handleConfigSet(ctx, req); reply.Code != outgressrpc.CodeConflict {
		t.Fatalf("want conflict, got %+v", reply)
	}
}

func TestHandleConfigReportsNotBound(t *testing.T) {
	d := configRPCFor(t, "guild-1")
	ctx := context.Background()

	got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "99", GuildID: "guild-1"})
	if got.Code != outgressrpc.CodeNotBound {
		t.Fatalf("want not_bound for another broadcaster's guild, got %+v", got)
	}
	set := d.handleConfigSet(ctx, outgressrpc.DiscordConfigSetRequest{UserID: "42", GuildID: "guild-9"})
	if set.Code != outgressrpc.CodeNotBound {
		t.Fatalf("want not_bound for an unbound guild, got %+v", set)
	}
}

func TestHandleConfigRejectsMissingIDs(t *testing.T) {
	d := configRPCFor(t)
	ctx := context.Background()

	if got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "42"}); got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("want invalid, got %+v", got)
	}
	if got := d.handleGuildsList(ctx, outgressrpc.DiscordGuildsListRequest{}); got.Code != outgressrpc.CodeInvalid {
		t.Fatalf("want invalid, got %+v", got)
	}
}

func TestHandleGuildsListReturnsEveryBinding(t *testing.T) {
	d := configRPCFor(t, "guild-1", "guild-2")

	got := d.handleGuildsList(context.Background(), outgressrpc.DiscordGuildsListRequest{UserID: "42"})
	if got.Code != outgressrpc.CodeOK || len(got.Guilds) != 2 {
		t.Fatalf("want two servers, got %+v", got)
	}
	if got.Guilds[0].BotPresent {
		t.Fatalf("want bot_present false with no Discord client, got %+v", got.Guilds[0])
	}
}

func TestHandleGuildsListSaysTimeoutAndKeepsThePartial(t *testing.T) {
	d := configRPCFor(t, "guild-1", "guild-2")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got := d.handleGuildsList(ctx, outgressrpc.DiscordGuildsListRequest{UserID: "42"})
	if got.Code != outgressrpc.CodeTimeout {
		t.Fatalf("want the timeout code, got %+v", got)
	}
	if got.Guilds == nil {
		t.Fatal("a partial listing must still travel")
	}
}

type flaggedReauth map[string]bool

func (f flaggedReauth) NeedsReauth(_ context.Context, g kv.GuildID) bool { return f[string(g)] }

func TestGuildEntriesNeverGuessAReauthFlag(t *testing.T) {
	d := configRPCFor(t, "guild-1", "guild-2")
	d.reauth = flaggedReauth{"guild-1": true}
	guilds := []setup.GuildSummary{{GuildID: "guild-1"}, {GuildID: "guild-2"}}

	live := d.guildEntries(context.Background(), guilds)
	if !live[0].NeedsReauth || live[0].ReauthUnknown {
		t.Fatalf("read entry = %+v, want a known, raised flag", live[0])
	}
	if live[1].NeedsReauth || live[1].ReauthUnknown {
		t.Fatalf("read entry = %+v, want a known, clear flag", live[1])
	}

	dead, cancel := context.WithCancel(context.Background())
	cancel()
	for _, got := range d.guildEntries(dead, guilds) {
		if got.NeedsReauth || !got.ReauthUnknown {
			t.Fatalf("unread entry = %+v, want reauth_unknown rather than a false", got)
		}
	}
}

func TestHandleGuildsListFlagsATruncatedListing(t *testing.T) {
	ids := make([]string, 0, setup.MaxListedGuilds+1)
	for i := range setup.MaxListedGuilds + 1 {
		ids = append(ids, "guild-"+strconv.Itoa(i))
	}
	d := configRPCFor(t, ids...)

	got := d.handleGuildsList(context.Background(), outgressrpc.DiscordGuildsListRequest{UserID: "42"})

	if len(got.Guilds) != setup.MaxListedGuilds || !got.Truncated {
		t.Fatalf("listing = %d entries, truncated=%v", len(got.Guilds), got.Truncated)
	}
	if short := configRPCFor(t, "guild-1").handleGuildsList(
		context.Background(), outgressrpc.DiscordGuildsListRequest{UserID: "42"},
	); short.Truncated {
		t.Fatalf("listing = %+v, want no truncation flag", short)
	}
}

func TestHandleDeskRepostOnAnUnboundGuildCarriesNotBound(t *testing.T) {
	d := newDiscordRPC(t, &fakeSetupREST{}, fakeBotStatus{}, false)

	got := d.handleDeskRepost(context.Background(), outgressrpc.DiscordDeskRepostRequest{
		UserID: "b1", GuildID: "g1", ChannelID: "support",
	})

	if got.Code != outgressrpc.CodeNotBound {
		t.Fatalf("code = %q, want %q", got.Code, outgressrpc.CodeNotBound)
	}
}

func intPtr(v int) *int { return &v }

func TestPanelSpecCarriesEveryField(t *testing.T) {
	got := panelSpec(outgressrpc.DiscordPanelSpec{Title: "t", Body: "b", Color: intPtr(7), Button: "go"})

	wantReplyField(t, "title", got.Title, "t")
	wantReplyField(t, "body", got.Body, "b")
	wantReplyField(t, "color", got.ColorOr(0), 7)
	wantReplyField(t, "button", got.Button, "go")
}

func TestPanelSpecKeepsBlackAndUnsetApart(t *testing.T) {
	if got := panelSpec(outgressrpc.DiscordPanelSpec{Title: "t"}); got.Color != nil {
		t.Fatalf("color = %v, want nil for an omitted key", got.Color)
	}
	got := panelSpec(outgressrpc.DiscordPanelSpec{Title: "t", Color: intPtr(0)})
	if got.Color == nil || got.ColorOr(ddiscord.LiveColor) != 0 {
		t.Fatalf("color = %v, want a set 0", got.Color)
	}
	if got.OrDefaults().ColorOr(ddiscord.LiveColor) != 0 {
		t.Fatal("OrDefaults must not repaint a black panel")
	}
}

func TestHandleGuildsListCarriesTheIconURL(t *testing.T) {
	rest := &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ", Icon: "abc", ApproximateMemberCount: 9}}
	d := newDiscordRPC(t, rest, fakeBotStatus{}, true)

	got := d.handleGuildsList(context.Background(), outgressrpc.DiscordGuildsListRequest{UserID: "b1"})
	if got.Code != outgressrpc.CodeOK || len(got.Guilds) != 1 {
		t.Fatalf("want one server, got %+v", got)
	}
	wantReplyField(t, "icon url", got.Guilds[0].IconURL, "https://cdn.discordapp.com/icons/g1/abc.png")
	wantReplyField(t, "member count", got.Guilds[0].MemberCount, 9)
}
