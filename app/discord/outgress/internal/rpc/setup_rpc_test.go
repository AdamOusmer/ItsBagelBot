// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const rpcGuildID = "100000000000000001"

const missingIDs = "missing guild_id or user_id"

var (
	boundG1        = map[string]string{"g1": "b1"}
	trackedGuilds  = []string{"g1", "g2", rpcGuildID}
	storedLive123  = storedConfig{Config: ddiscord.Config{LiveChannelID: "123"}, Version: 1, Found: true}
	notBoundStatus = outgressrpc.DiscordStatusReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound}
)

type setupWorld struct {
	rest      *fakeSetupREST
	status    botStatusReader
	reauth    reauthReader
	owners    map[string]string
	config    *ddiscord.Config
	botID     string
	storeDown bool
}

type storedConfig struct {
	Config  ddiscord.Config
	Version int
	Found   bool
}

type setupCase struct {
	name   string
	world  setupWorld
	verb   string
	req    any
	want   any
	owners map[string]string
	stored storedConfig
	desks  []deskPanel
	chats  []discapi.ChatPost
}

type setupOutcome struct {
	Reply  any
	Owners map[string]string
	Stored storedConfig
	Desks  []deskPanel
	Chats  []discapi.ChatPost
}

func intPtr(v int) *int { return &v }

func flag(v bool) *bool { return &v }

func boundStore(t *testing.T, owners map[string]string) *discordstore.Mem {
	t.Helper()
	mem := discordstore.NewMem()
	for guild, owner := range owners {
		require.NoError(t, mem.BindGuild(context.Background(), discordstore.Binding{
			Guild: discordstore.Guild{ID: guild}, Broadcaster: discordstore.Broadcaster{ID: owner},
		}))
	}
	return mem
}

func configRPCFor(t *testing.T, guildIDs ...string) *discordRPC {
	t.Helper()
	owners := map[string]string{}
	for _, id := range guildIDs {
		owners[id] = "42"
	}
	w := setup.New(setup.Config{
		Store: boundStore(t, owners), Discord: &fakeSetupREST{channelGuild: "guild-1", guildErr: discapi.ErrForbidden},
		Log: zap.NewNop(),
	})
	return &discordRPC{w: w, log: zap.NewNop()}
}

func (w setupWorld) serve(t *testing.T) (*discordstore.Mem, Wiring) {
	mem := boundStore(t, w.owners)
	if w.config != nil {
		mem.PutGuildConfig(discordstore.Guild{ID: "g1"}, *w.config)
	}
	cfg := setup.Config{Store: mem, BotID: w.botID}
	if w.storeDown {
		cfg.Store = unavailableStore{mem}
	}
	if w.rest != nil {
		cfg.Discord = w.rest
	}
	wire := rpcWiring(t)
	require.NoError(t, SubscribeSetup(setup.New(cfg), SetupWiring{Wiring: wire, Reauth: w.reauth, Status: w.status}))
	return mem, wire
}

func (w setupWorld) writes() ([]deskPanel, []discapi.ChatPost) {
	if w.rest == nil {
		return nil, nil
	}
	return w.rest.desks, w.rest.chats
}

func ownersIn(mem *discordstore.Mem) map[string]string {
	out := map[string]string{}
	for _, guild := range trackedGuilds {
		if owner, ok := mem.Broadcaster(context.Background(), discordstore.Guild{ID: guild}); ok {
			out[guild] = owner.ID
		}
	}
	return orNil(out)
}

func orNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func (tc setupCase) expected() setupOutcome {
	owners := tc.owners
	if owners == nil {
		owners = tc.world.owners
	}
	return setupOutcome{Reply: tc.want, Owners: orNil(owners), Stored: tc.stored, Desks: tc.desks, Chats: tc.chats}
}

func (tc setupCase) outcome(t *testing.T) setupOutcome {
	mem, wire := tc.world.serve(t)
	reply := decodeAs(t, requestRPC(t, wire, tc.verb, tc.req), tc.want)
	cfg, version, found := mem.GuildConfig(context.Background(), discordstore.Guild{ID: "g1"})
	desks, chats := tc.world.writes()
	return setupOutcome{
		Reply: reply, Owners: ownersIn(mem), Stored: storedConfig{Config: cfg, Version: version, Found: found},
		Desks: desks, Chats: chats,
	}
}

func statusCases() []setupCase {
	ask := outgressrpc.DiscordStatusRequest{UserID: "b1", GuildID: "g1"}
	hq := discapi.GuildInfo{ID: "g1", Name: "Bagel HQ", Icon: "abc", ApproximateMemberCount: 42}
	return []setupCase{{
		name: "reports the session, the guild and the reauth flag separately",
		world: setupWorld{
			rest:   &fakeSetupREST{guild: hq},
			status: fakeBotStatus{ok: true, st: ddiscord.BotStatus{Connected: true, SinceUnixMS: 1700, Resumes: 3, LastCloseCode: 4009}},
			reauth: flaggedReauth{"g1": true},
			owners: boundG1,
		},
		verb: "discord.status", req: ask,
		want: outgressrpc.DiscordStatusReply{
			Online: true, SinceUnixMS: 1700, SessionResumes: 3, LastCloseCode: 4009, NeedsReauth: true,
			GuildPresent: true, GuildName: "Bagel HQ", IconURL: "https://cdn.discordapp.com/icons/g1/abc.png", MemberCount: 42,
		},
	}, {
		name:  "carries not_bound for an unbound guild",
		world: setupWorld{rest: &fakeSetupREST{}, status: fakeBotStatus{}},
		verb:  "discord.status", req: ask,
		want: notBoundStatus,
	}, {
		name:  "rejects a status request without a user",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.status", req: outgressrpc.DiscordStatusRequest{GuildID: "g1"},
		want: outgressrpc.DiscordStatusReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}, {
		name:  "reports offline without a status key and still looks the guild up",
		world: setupWorld{rest: &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "HQ"}}, owners: boundG1},
		verb:  "discord.status", req: ask,
		want: outgressrpc.DiscordStatusReply{GuildPresent: true, GuildName: "HQ"},
	}, {
		name: "carries the connect budget of a parked ingress as offline",
		world: setupWorld{
			rest: &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ"}},
			status: fakeBotStatus{ok: true, st: ddiscord.BotStatus{
				LastCloseCode: 4000, Flapping: true, ConnectsInWindow: 800, AtCeiling: true, ParkUntilUnixMS: 1_700_000_000_000,
			}},
			owners: boundG1,
		},
		verb: "discord.status", req: ask,
		want: outgressrpc.DiscordStatusReply{
			LastCloseCode: 4000, Flapping: true, ConnectsInWindow: 800, AtCeiling: true, ParkUntilUnixMS: 1_700_000_000_000,
			GuildPresent: true, GuildName: "Bagel HQ",
		},
	}, {
		name: "publishes the connect count of a healthy session",
		world: setupWorld{
			rest:   &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ"}},
			status: fakeBotStatus{ok: true, st: ddiscord.BotStatus{Connected: true, ConnectsInWindow: 4}},
			owners: boundG1,
		},
		verb: "discord.status", req: ask,
		want: outgressrpc.DiscordStatusReply{Online: true, ConnectsInWindow: 4, GuildPresent: true, GuildName: "Bagel HQ"},
	}}
}

func layoutCases() []setupCase {
	ask := outgressrpc.DiscordLayoutRequest{UserID: "b1", GuildID: "g1"}
	everyone := []outgressrpc.DiscordLayoutEntry{{ID: "role-1", Name: "@everyone"}}
	community := []outgressrpc.DiscordLayoutEntry{{ID: "cat-1", Name: "Community", Type: ddiscord.ChannelCategory}}
	return []setupCase{{
		name: "splits categories from channels and carries the bot session",
		world: setupWorld{
			rest: &fakeSetupREST{
				channels: []discapi.Snowflake{
					{ID: "cat-1", Name: "Community", Type: ddiscord.ChannelCategory},
					{ID: "ch-1", Name: "general", Type: ddiscord.ChannelText},
					{ID: "vc-1", Name: "Voice", Type: 2},
				},
				guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ", ApproximateMemberCount: 9},
			},
			status: fakeBotStatus{ok: true, st: ddiscord.BotStatus{Connected: true, SinceUnixMS: 99, LastCloseCode: 4000}},
			owners: boundG1,
		},
		verb: "discord.layout", req: ask,
		want: outgressrpc.DiscordLayoutReply{
			Channels:   []outgressrpc.DiscordLayoutEntry{{ID: "ch-1", Name: "general"}, {ID: "vc-1", Name: "Voice", Type: 2}},
			Categories: community,
			Roles:      everyone,
			Guild:      &outgressrpc.DiscordGuildInfo{ID: "g1", Name: "Bagel HQ", MemberCount: 9},
			BotOnline:  true, BotSinceUnixMS: 99, LastCloseCode: 4000,
		},
	}, {
		name: "survives a failed guild lookup",
		world: setupWorld{
			rest: &fakeSetupREST{
				channels: []discapi.Snowflake{{ID: "ch-1", Name: "general", Type: ddiscord.ChannelText}},
				guildErr: discapi.ErrForbidden,
			},
			status: fakeBotStatus{},
			owners: boundG1,
		},
		verb: "discord.layout", req: ask,
		want: outgressrpc.DiscordLayoutReply{
			Channels: []outgressrpc.DiscordLayoutEntry{{ID: "ch-1", Name: "general"}}, Roles: everyone,
		},
	}, {
		name:  "carries each channel's parent and what the bot may post there",
		world: setupWorld{rest: botAccessREST(), botID: "bot", owners: boundG1},
		verb:  "discord.layout", req: ask,
		want: outgressrpc.DiscordLayoutReply{
			Channels: []outgressrpc.DiscordLayoutEntry{
				{ID: "open", Name: "general", ParentID: "cat-1", ParentName: "Community", BotCanSend: flag(true), BotCanEmbed: flag(true)},
				{ID: "locked", Name: "general", ParentID: "cat-1", ParentName: "Community", BotCanSend: flag(false), BotCanEmbed: flag(false)},
				{ID: "noembed", Name: "news", Type: 5, BotCanSend: flag(true), BotCanEmbed: flag(false)},
			},
			Categories: community,
			Roles:      []outgressrpc.DiscordLayoutEntry{{ID: "g1"}},
			Guild:      &outgressrpc.DiscordGuildInfo{},
		},
	}, {
		name:  "rejects a layout request without a user",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.layout", req: outgressrpc.DiscordLayoutRequest{GuildID: "g1"},
		want: outgressrpc.DiscordLayoutReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}, {
		name:  "refuses the layout of an unbound guild",
		world: setupWorld{rest: &fakeSetupREST{}},
		verb:  "discord.layout", req: ask,
		want: outgressrpc.DiscordLayoutReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound},
	}}
}

func botAccessREST() *fakeSetupREST {
	const view, send, embed = 1 << 10, 1 << 11, 1 << 14
	return &fakeSetupREST{
		channels: []discapi.Snowflake{
			{ID: "cat-1", Name: "Community", Type: ddiscord.ChannelCategory},
			{ID: "open", Name: "general", Type: ddiscord.ChannelText, ParentID: "cat-1"},
			{ID: "locked", Name: "general", Type: ddiscord.ChannelText, ParentID: "cat-1",
				PermissionOverwrites: []discapi.PermissionOverwrite{{ID: "g1", Type: 0, Deny: "2048"}}},
			{ID: "noembed", Name: "news", Type: 5,
				PermissionOverwrites: []discapi.PermissionOverwrite{{ID: "g1", Type: 0, Deny: "16384"}}},
		},
		roles: []discapi.Snowflake{{ID: "g1", Permissions: strconv.Itoa(view | send | embed)}},
	}
}

func guildSetupCases() []setupCase {
	ours := map[string]string{rpcGuildID: "b1"}
	invalid := func(field string) outgressrpc.DiscordSetupReply {
		return outgressrpc.DiscordSetupReply{
			Code: outgressrpc.CodeInvalid, Fields: []string{field}, Error: "some settings are not valid: " + field,
		}
	}
	return []setupCase{{
		name:  "refuses a pinned role that is not a snowflake",
		world: setupWorld{owners: ours},
		verb:  "discord.setup",
		req: outgressrpc.DiscordSetupRequest{
			UserID: "b1", GuildID: rpcGuildID, PinnedRoles: map[string]string{ddiscord.SlotMods: "<@&12345>"},
		},
		want: invalid("pinnedRoles"),
	}, {
		name:  "refuses an unknown pinned slot",
		world: setupWorld{owners: ours},
		verb:  "discord.setup",
		req: outgressrpc.DiscordSetupRequest{
			UserID: "b1", GuildID: rpcGuildID, PinnedRoles: map[string]string{"janitor": "100000000000000002"},
		},
		want: invalid("pinnedRoles"),
	}, {
		name:  "names a bad field once however many of its pins are bad",
		world: setupWorld{owners: ours},
		verb:  "discord.setup",
		req: outgressrpc.DiscordSetupRequest{
			UserID: "b1", GuildID: rpcGuildID,
			PinnedRoles: map[string]string{ddiscord.SlotMods: "<@&12345>", "janitor": "100000000000000002"},
		},
		want: invalid("pinnedRoles"),
	}, {
		name:  "refuses a guild id that is not a snowflake",
		world: setupWorld{owners: ours},
		verb:  "discord.setup",
		req:   outgressrpc.DiscordSetupRequest{UserID: "b1", GuildID: "my-server"},
		want:  invalid("guildId"),
	}, {
		name:  "passes well-formed pins on to the fill",
		world: setupWorld{owners: ours},
		verb:  "discord.setup",
		req: outgressrpc.DiscordSetupRequest{
			UserID: "b1", GuildID: rpcGuildID, PinnedRoles: map[string]string{ddiscord.SlotMods: "100000000000000002"},
		},
		want: outgressrpc.DiscordSetupReply{
			Error: setup.ErrDiscordUnavailable.Error(), Code: outgressrpc.CodeDiscordUnavailable,
		},
	}, {
		name:  "refuses a guild linked to another broadcaster",
		world: setupWorld{rest: &fakeSetupREST{}, owners: map[string]string{rpcGuildID: "99"}},
		verb:  "discord.setup",
		req:   outgressrpc.DiscordSetupRequest{UserID: "b1", GuildID: rpcGuildID},
		want: outgressrpc.DiscordSetupReply{
			Error: setup.ErrGuildBoundElsewhere.Error(), Code: outgressrpc.CodeBoundElsewhere,
		},
	}, {
		name:  "rejects a setup request without a user",
		world: setupWorld{owners: ours},
		verb:  "discord.setup",
		req:   outgressrpc.DiscordSetupRequest{GuildID: rpcGuildID},
		want:  outgressrpc.DiscordSetupReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}}
}

func deskCases() []setupCase {
	return []setupCase{{
		name:  "reposts the desk panel with the requested look",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.desk.repost",
		req: outgressrpc.DiscordDeskRepostRequest{
			UserID: "b1", GuildID: "g1", ChannelID: "support",
			Panel: outgressrpc.DiscordPanelSpec{
				Title: "Need a hand?", Body: "We reply fast", Color: intPtr(0x112233), Button: "Contact staff",
			},
		},
		want: outgressrpc.DiscordDeskRepostReply{MessageID: "m-new"},
		desks: []deskPanel{{
			Channel: "support", Title: "Need a hand?", Body: "We reply fast", Color: 0x112233, Button: "Contact staff",
		}},
	}, {
		name:  "rejects a repost without a guild",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.desk.repost",
		req:   outgressrpc.DiscordDeskRepostRequest{UserID: "b1"},
		want:  outgressrpc.DiscordDeskRepostReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}, {
		name:  "refuses a repost on an unbound guild",
		world: setupWorld{rest: &fakeSetupREST{}},
		verb:  "discord.desk.repost",
		req:   outgressrpc.DiscordDeskRepostRequest{UserID: "b1", GuildID: "g1", ChannelID: "support"},
		want: outgressrpc.DiscordDeskRepostReply{
			Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound,
		},
	}}
}

func configCases() []setupCase {
	live123 := ddiscord.Config{LiveChannelID: "123"}
	seeded := func() setupWorld { return setupWorld{rest: &fakeSetupREST{}, owners: boundG1, config: &live123} }
	notBound := outgressrpc.DiscordConfigSetReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound}
	return []setupCase{{
		name:   "saves a config at version 1",
		world:  setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:   "discord.config.set",
		req:    outgressrpc.DiscordConfigSetRequest{UserID: "b1", GuildID: "g1", Config: live123},
		want:   outgressrpc.DiscordConfigSetReply{Version: 1},
		stored: storedLive123,
	}, {
		name:  "refuses a stale save and keeps the stored config",
		world: seeded(),
		verb:  "discord.config.set",
		req: outgressrpc.DiscordConfigSetRequest{
			UserID: "b1", GuildID: "g1", Config: ddiscord.Config{LiveChannelID: "456"},
		},
		want: outgressrpc.DiscordConfigSetReply{
			Error: discordstore.ErrConfigConflict.Error(), Code: outgressrpc.CodeConflict,
		},
		stored: storedLive123,
	}, {
		name:   "reads the stored config",
		world:  seeded(),
		verb:   "discord.config.get",
		req:    outgressrpc.DiscordConfigGetRequest{UserID: "b1", GuildID: "g1"},
		want:   outgressrpc.DiscordConfigGetReply{Config: live123, Version: 1, Found: true},
		stored: storedLive123,
	}, {
		name:   "refuses another broadcaster's config read",
		world:  seeded(),
		verb:   "discord.config.get",
		req:    outgressrpc.DiscordConfigGetRequest{UserID: "99", GuildID: "g1"},
		want:   outgressrpc.DiscordConfigGetReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound},
		stored: storedLive123,
	}, {
		name:  "refuses another broadcaster's config write",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.config.set",
		req: outgressrpc.DiscordConfigSetRequest{
			UserID: "99", GuildID: "g1", Config: ddiscord.Config{LiveChannelID: "injected"},
		},
		want: notBound,
	}, {
		name:  "refuses a save to an unbound guild",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.config.set",
		req:   outgressrpc.DiscordConfigSetRequest{UserID: "b1", GuildID: "guild-9"},
		want:  notBound,
	}, {
		name:  "rejects a config read without a guild",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.config.get",
		req:   outgressrpc.DiscordConfigGetRequest{UserID: "b1"},
		want:  outgressrpc.DiscordConfigGetReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}, {
		name:  "rejects a config write without a guild",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.config.set",
		req:   outgressrpc.DiscordConfigSetRequest{UserID: "b1"},
		want:  outgressrpc.DiscordConfigSetReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}}
}

func guildListCases() []setupCase {
	ask := outgressrpc.DiscordGuildsListRequest{UserID: "b1"}
	return []setupCase{{
		name: "lists every binding and marks guilds Discord does not show the bot",
		world: setupWorld{
			rest: &fakeSetupREST{guildErr: discapi.ErrForbidden}, owners: map[string]string{"g1": "b1", "g2": "b1"},
		},
		verb: "discord.guilds.list", req: ask,
		want: outgressrpc.DiscordGuildsListReply{Guilds: []outgressrpc.DiscordGuildEntry{{GuildID: "g1"}, {GuildID: "g2"}}},
	}, {
		name: "carries what Discord and the reauth store know about a guild",
		world: setupWorld{
			rest:   &fakeSetupREST{guild: discapi.GuildInfo{ID: "g1", Name: "Bagel HQ", Icon: "abc", ApproximateMemberCount: 9}},
			reauth: flaggedReauth{"g1": true},
			owners: boundG1,
		},
		verb: "discord.guilds.list", req: ask,
		want: outgressrpc.DiscordGuildsListReply{Guilds: []outgressrpc.DiscordGuildEntry{{
			GuildID: "g1", Name: "Bagel HQ", IconURL: "https://cdn.discordapp.com/icons/g1/abc.png",
			MemberCount: 9, BotPresent: true, NeedsReauth: true,
		}}},
	}, {
		name:  "rejects a listing without a user",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.guilds.list", req: outgressrpc.DiscordGuildsListRequest{},
		want: outgressrpc.DiscordGuildsListReply{Error: "missing user_id", Code: outgressrpc.CodeInvalid},
	}, {
		name:  "reports an unavailable store with an empty listing",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1, storeDown: true},
		verb:  "discord.guilds.list", req: ask,
		want: outgressrpc.DiscordGuildsListReply{
			Guilds: []outgressrpc.DiscordGuildEntry{},
			Error:  discordstore.ErrStoreUnavailable.Error(), Code: outgressrpc.CodeDiscordUnavailable,
		},
	}}
}

func mutationCases() []setupCase {
	return []setupCase{{
		name:  "refuses to unbind another broadcaster's guild",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.unbind",
		req:   outgressrpc.DiscordUnbindRequest{UserID: "99", GuildID: "g1"},
		want:  outgressrpc.DiscordUnbindReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound},
	}, {
		name:   "unbinds the owner's guild",
		world:  setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:   "discord.unbind",
		req:    outgressrpc.DiscordUnbindRequest{UserID: "b1", GuildID: "g1"},
		want:   outgressrpc.DiscordUnbindReply{},
		owners: map[string]string{},
	}, {
		name:  "rejects an unbind without a user",
		world: setupWorld{rest: &fakeSetupREST{}, owners: boundG1},
		verb:  "discord.unbind",
		req:   outgressrpc.DiscordUnbindRequest{GuildID: "g1"},
		want:  outgressrpc.DiscordUnbindReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
	}, {
		name:  "posts a chat message",
		world: setupWorld{rest: &fakeSetupREST{}},
		verb:  "discord.post",
		req:   outgressrpc.DiscordPostRequest{ChannelID: "c1", Content: "hello"},
		want:  outgressrpc.DiscordPostReply{},
		chats: []discapi.ChatPost{{ChannelID: "c1", Content: "hello"}},
	}, {
		name:  "rejects an empty post",
		world: setupWorld{rest: &fakeSetupREST{}},
		verb:  "discord.post",
		req:   outgressrpc.DiscordPostRequest{ChannelID: "c1"},
		want:  outgressrpc.DiscordPostReply{Error: "missing channel or content", Code: outgressrpc.CodeInvalid},
	}, {
		name:  "says Discord is unavailable when posting without a client",
		world: setupWorld{},
		verb:  "discord.post",
		req:   outgressrpc.DiscordPostRequest{ChannelID: "c1", Content: "hello"},
		want: outgressrpc.DiscordPostReply{
			Error: discapi.ErrAuth.Error(), Code: outgressrpc.CodeDiscordUnavailable,
		},
	}}
}

func TestSetupVerbsAnswerAndKeepState(t *testing.T) {
	cases := slices.Concat(statusCases(), layoutCases(), guildSetupCases(), deskCases(),
		configCases(), guildListCases(), mutationCases())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected(), tc.outcome(t))
		})
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
