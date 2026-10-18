// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"slices"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	outgressrpc "ItsBagelBot/internal/domain/rpc/outgress"
	"ItsBagelBot/pkg/codec"
)

const (
	liveChan    = "100000000000000123"
	foreignChan = "100000000000000456"
)

var storedLive = storedConfig{Config: ddiscord.Config{LiveChannelID: liveChan}, Version: 1, Found: true}

var live123 = ddiscord.Config{LiveChannelID: liveChan}

func liveRest() *fakeSetupREST { return &fakeSetupREST{channels: []discapi.Snowflake{{ID: liveChan}}} }

func seededWorld() setupWorld {
	cfg := live123
	return setupWorld{rest: liveRest(), owners: boundG1, config: &cfg}
}

func liveSettings(channel string) codec.RawMessage {
	return codec.RawMessage(`{"liveChannelId":"` + channel + `"}`)
}

func invalidModuleSet(field string) outgressrpc.DiscordModuleSetReply {
	return outgressrpc.DiscordModuleSetReply{
		Code: outgressrpc.CodeInvalid, Fields: []string{field}, Error: "some settings are not valid: " + field,
	}
}

func moduleCases() []setupCase {
	return slices.Concat(moduleListCases(), moduleSetCases(), moduleSetRefusalCases())
}

func moduleListCases() []setupCase {
	return []setupCase{
		{
			name:   "lists the legacy config as module rows in catalog order",
			world:  seededWorld(),
			verb:   "discord.module.list",
			req:    outgressrpc.DiscordModuleListRequest{UserID: "b1", GuildID: "g1"},
			want:   outgressrpc.DiscordModuleListReply{Modules: legacyRows(live123), Found: true},
			stored: storedLive,
		}, {
			name:   "refuses another broadcaster's module read",
			world:  seededWorld(),
			verb:   "discord.module.list",
			req:    outgressrpc.DiscordModuleListRequest{UserID: "99", GuildID: "g1"},
			want:   outgressrpc.DiscordModuleListReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound},
			stored: storedLive,
		}, {
			name:  "rejects a module read without a guild",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.list",
			req:   outgressrpc.DiscordModuleListRequest{UserID: "b1"},
			want:  outgressrpc.DiscordModuleListReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
		},
	}
}

func moduleSetCases() []setupCase {
	notBound := outgressrpc.DiscordModuleSetReply{Error: setup.ErrNotBound.Error(), Code: outgressrpc.CodeNotBound}
	return []setupCase{
		{
			name:  "saves a module at version 1",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req: outgressrpc.DiscordModuleSetRequest{
				UserID: "b1", GuildID: "g1", Name: ddiscord.ModuleLive, Enabled: true, Settings: liveSettings(liveChan),
			},
			want: outgressrpc.DiscordModuleSetReply{Version: 1},
		}, {
			name:  "refuses a stale module save",
			world: seededWorld(),
			verb:  "discord.module.set",
			req: outgressrpc.DiscordModuleSetRequest{
				UserID: "b1", GuildID: "g1", Name: ddiscord.ModuleLive, Enabled: true, Settings: liveSettings(liveChan),
			},
			want:   outgressrpc.DiscordModuleSetReply{Error: discordstore.ErrConfigConflict.Error(), Code: outgressrpc.CodeConflict},
			stored: storedLive,
		}, {
			name:  "refuses another broadcaster's module write",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req: outgressrpc.DiscordModuleSetRequest{
				UserID: "99", GuildID: "g1", Name: ddiscord.ModuleLive, Settings: liveSettings(liveChan),
			},
			want: notBound,
		}, {
			name:  "refuses a channel that is not in the guild",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req: outgressrpc.DiscordModuleSetRequest{
				UserID: "b1", GuildID: "g1", Name: ddiscord.ModuleLive, Settings: liveSettings(foreignChan),
			},
			want: outgressrpc.DiscordModuleSetReply{Error: discapi.ErrForbidden.Error(), Code: outgressrpc.CodeForbidden},
		},
	}
}

func moduleSetRefusalCases() []setupCase {
	return []setupCase{
		{
			name:  "names the invalid setting fields",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req: outgressrpc.DiscordModuleSetRequest{
				UserID: "b1", GuildID: "g1", Name: ddiscord.ModuleLive, Settings: liveSettings("not-a-snowflake"),
			},
			want: invalidModuleSet("liveChannelId"),
		}, {
			name:  "rejects an unknown module",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req:   outgressrpc.DiscordModuleSetRequest{UserID: "b1", GuildID: "g1", Name: "nope"},
			want:  invalidModuleSet("name"),
		}, {
			name:  "rejects the identity module",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req:   outgressrpc.DiscordModuleSetRequest{UserID: "b1", GuildID: "g1", Name: ddiscord.ModuleIdentity},
			want:  invalidModuleSet("name"),
		}, {
			name:  "rejects the voicestate module",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req:   outgressrpc.DiscordModuleSetRequest{UserID: "b1", GuildID: "g1", Name: ddiscord.ModuleVoiceState},
			want:  invalidModuleSet("name"),
		}, {
			name:  "rejects a module write without a guild",
			world: setupWorld{rest: liveRest(), owners: boundG1},
			verb:  "discord.module.set",
			req:   outgressrpc.DiscordModuleSetRequest{UserID: "b1", Name: ddiscord.ModuleLive},
			want:  outgressrpc.DiscordModuleSetReply{Error: missingIDs, Code: outgressrpc.CodeInvalid},
		},
	}
}

func legacyRows(cfg ddiscord.Config) []ddiscord.ModuleRow {
	rows := ddiscord.SplitConfig(cfg)
	for i := range rows {
		rows[i].Version = 1
	}
	return rows
}
