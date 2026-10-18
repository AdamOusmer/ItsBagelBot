// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"errors"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	dd "ItsBagelBot/internal/domain/rpc/discorddata"
)

var noModules = pair(ddiscord.GuildModules{}, false)

func moduleScenarios() []scenario {
	row := ddiscord.ModuleRow{Name: ddiscord.ModuleLive, Enabled: true, Settings: []byte(`{"liveChannelId":"123"}`), Version: 2}
	wantMods := ddiscord.GuildModules{GuildID: "g1", Rows: map[string]ddiscord.ModuleRow{row.Name: row}}
	readStored := step{
		serve: replies{dd.VerbModuleList: dd.ModuleListReply{Modules: []ddiscord.ModuleRow{row}, Found: true}},
		do:    callFound(store.GuildModules, g1), want: pair(wantMods, true),
		sent: replies{dd.VerbModuleList: dd.ModuleListRequest{GuildID: "g1"}},
	}
	readCached := step{do: callFound(store.GuildModules, g1), want: pair(wantMods, true)}
	readGone := step{do: callFound(store.GuildModules, g1), want: noModules}
	write := callResult(store.SetGuildModule, discordstore.SetModule{Guild: g1, Broadcaster: owner, Row: row, ExpectedVersion: 2})
	refusing := func(reply dd.ModuleSetReply, want error) step {
		return step{serve: replies{dd.VerbModuleSet: reply}, do: write, want: outcome(0, want)}
	}
	return []scenario{{
		name:  "modules are cached and served through an outage",
		steps: []step{readStored, readCached},
	}, {
		name: "a guild with no module rows drops the cache",
		steps: []step{readStored, {
			serve: replies{dd.VerbModuleList: dd.ModuleListReply{Found: false}},
			do:    callFound(store.GuildModules, g1), want: noModules,
		}, readGone},
	}, {
		name: "a module write invalidates the cache and maps every refusal",
		steps: []step{readStored, {
			serve: replies{dd.VerbModuleSet: dd.ModuleSetReply{Version: 3}},
			do:    write, want: outcome(3, nil),
			sent: replies{dd.VerbModuleSet: dd.ModuleSetRequest{
				GuildID: "g1", BroadcasterID: 77, Name: "live", Enabled: true,
				Settings: row.Settings, ExpectedVersion: 2,
			}},
		},
			readGone,
			refusing(dd.ModuleSetReply{Error: "stale", Code: dd.CodeConflict}, discordstore.ErrConfigConflict),
			refusing(dd.ModuleSetReply{Error: "not yours", Code: dd.CodeNotBound}, discordstore.ErrNotBound),
			refusing(dd.ModuleSetReply{Error: "db down", Code: dd.CodeInternal}, errors.New("db down")),
			{do: write, want: outcome(0, nats.ErrNoResponders)},
		},
	}, {
		name:  "Invalidate drops the cached modules",
		steps: []step{readStored, {do: invalidate, want: nil}, readGone},
	}}
}
