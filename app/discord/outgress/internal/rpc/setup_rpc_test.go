// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
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

// fakeSetupREST is the setup worker's REST slice. Only the read half is
// scripted: nothing here fills a guild.
type fakeSetupREST struct {
	channels  []discapi.Snowflake
	guild     discapi.GuildInfo
	guildErr  error
	guildHits int
	// panelID is the id SendPanel hands back, which the desk-repost reply
	// carries straight through to the dashboard.
	panelID string
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

// newDiscordRPC wires a handler over a bound guild (g1 -> b1) unless bind is
// false.
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
	// The message moved with the handler: status now takes the strict
	// ownership check, which reports ErrNotBound for both "no binding" and
	// "somebody else's". The console reads the code, not the text.
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

// The dashboard has to be able to tell "the bot is offline" from "the bot is
// offline and its ingress has stopped dialling until tomorrow morning". The
// budget rides the status key; handleStatus must carry all four fields
// through, including the deadline that says when to look again.
func TestHandleStatusCarriesTheConnectBudget(t *testing.T) {
	rest := &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ"}}
	status := fakeBotStatus{ok: true, st: ddiscord.BotStatus{
		LastCloseCode: 4000, Flapping: true, ConnectsInWindow: 800,
		AtCeiling: true, ParkUntilUnixMS: 1_700_000_000_000,
	}}
	d := newDiscordRPC(t, rest, status, true)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	if !got.Flapping || got.ConnectsInWindow != 800 || !got.AtCeiling {
		t.Fatalf("budget fields = %+v, want the key's own values", got)
	}
	if got.ParkUntilUnixMS != 1_700_000_000_000 {
		t.Fatalf("park_until_unix_ms = %d, want the key's deadline", got.ParkUntilUnixMS)
	}
	if got.Online {
		t.Fatalf("reply = %+v, want offline: a parked ingress holds no session", got)
	}
}

// A bot with a healthy session publishes no budget pressure, and the reply
// must not invent any -- an at_ceiling that is merely a stale default puts a
// permanent warning on a dashboard for a bot that is fine.
func TestHandleStatusOmitsAnUnpressuredBudget(t *testing.T) {
	rest := &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ"}}
	status := fakeBotStatus{ok: true, st: ddiscord.BotStatus{Connected: true, ConnectsInWindow: 4}}
	d := newDiscordRPC(t, rest, status, true)

	got := d.handleStatus(context.Background(), outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"})

	if got.Flapping || got.AtCeiling || got.ParkUntilUnixMS != 0 {
		t.Fatalf("budget fields = %+v, want nothing flagged", got)
	}
	if got.ConnectsInWindow != 4 {
		t.Fatalf("connects_in_window = %d, want 4 published even when healthy", got.ConnectsInWindow)
	}
}

// setupRPC builds the handler with no Discord client attached. Validation
// runs BEFORE the fill, so a refused request never reaches the client at
// all and an accepted one is recognisable by the "unavailable" it then
// fails with.
func setupRPC() *discordRPC {
	w := setup.New(setup.Config{Store: discordstore.NewMem(), Log: zap.NewNop()})
	return &discordRPC{w: w, log: zap.NewNop()}
}

// A real guild id: ValidateConfig refuses anything that cannot be a
// snowflake, this file included.
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

// A slot that is not part of the template is refused rather than silently
// dropped: the dashboard sent something this build does not understand, and
// filling the guild anyway hides that from the streamer until they notice
// the role never got applied.
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

// A guild id that cannot be a snowflake is refused before the bind, so a
// paste error never writes a binding nobody can unbind.
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

// Valid pins pass validation and reach the fill, which is what fails here
// (no client). The point is the absence of CodeInvalid: a well-formed
// request must not be refused by the validator.
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
	if set.Code != outgressrpc.CodeOK || set.Version != 1 {
		t.Fatalf("save: %+v", set)
	}

	got := d.handleConfigGet(ctx, outgressrpc.DiscordConfigGetRequest{UserID: "42", GuildID: "guild-1"})
	if got.Code != outgressrpc.CodeOK || !got.Found || got.Config.LiveChannelID != "123" {
		t.Fatalf("read back: %+v", got)
	}
}

func TestHandleConfigSetReportsConflict(t *testing.T) {
	d := configRPCFor(t, "guild-1")
	ctx := context.Background()
	req := outgressrpc.DiscordConfigSetRequest{UserID: "42", GuildID: "guild-1"}

	if reply := d.handleConfigSet(ctx, req); reply.Code != outgressrpc.CodeOK {
		t.Fatalf("first save: %+v", reply)
	}
	// The same expected_version again: the page is out of date.
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
	if got.Code != outgressrpc.CodeTimeout {
		t.Fatalf("want the timeout code, got %+v", got)
	}
	if got.Guilds == nil {
		t.Fatal("a partial listing must still travel")
	}
}

// flaggedReauth is the stale-grant bookkeeping, scripted per guild.
type flaggedReauth map[string]bool

func (f flaggedReauth) NeedsReauth(_ context.Context, g kv.GuildID) bool { return f[string(g)] }

// TestGuildEntriesNeverGuessAReauthFlag is the fix for a listing that lies
// reassuringly. The flag is one Valkey read, the listing hands it a context
// that is already dead whenever the partial path was taken, and a failed read
// is a plain false -- so the guild whose grant had died was drawn as healthy
// on exactly the slow load where it matters. Unknown is a third state.
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

// TestHandleGuildsListFlagsATruncatedListing: the cap is silent on the wire
// without this, so a streamer past it sees a page that quietly forgot a
// server.
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
	// And a listing that fits says nothing.
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

	if got.Title != "t" || got.Body != "b" || got.ColorOr(0) != 7 || got.Button != "go" {
		t.Fatalf("spec = %+v", got)
	}
}

// TestPanelSpecKeepsBlackAndUnsetApart is the wire half of the pointer colour:
// a request that omits "color" must reach the renderer as unset (brand
// default), and one that sends 0 must reach it as black.
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
