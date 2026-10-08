// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/internal/domain/rpc"
	dd "ItsBagelBot/internal/domain/rpc/discorddata"
)

func TestRPCStoreFollowsDiscordData(t *testing.T) {
	scenarios := slices.Concat(bindingScenarios(), guildListScenarios(), configScenarios(),
		ticketScenarios(), xpScenarios(), localScenarios())
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			s, data := newRPCStore(t)
			runSteps(t, s, data, tc.steps)
		})
	}
}

var (
	cachedBinding = binding{ID: "77", Source: discordstore.BindingFromCache, OK: true}
	noBinding     = binding{Source: discordstore.BindingFromCache}
	storedBinding = step{
		serve: replies{dd.VerbBindingGet: dd.BindingGetReply{BroadcasterID: 77, Found: true}},
		do:    readBinding,
		want:  binding{ID: "77", Source: discordstore.BindingFromStore, OK: true},
	}
)

func bindingScenarios() []scenario {
	installed := discordstore.Binding{Guild: g1, Broadcaster: owner, InstalledBy: "discord-user-9"}
	return []scenario{{
		name: "a stored binding is cached and replayed from the cache during an outage",
		steps: []step{{
			serve: replies{dd.VerbBindingGet: dd.BindingGetReply{BroadcasterID: 77, Found: true}},
			do:    callFound(store.Broadcaster, g1), want: pair(owner, true),
			sent: replies{dd.VerbBindingGet: dd.BindingGetRequest{GuildID: "g1"}},
		}, {do: readBinding, want: cachedBinding}},
	}, {
		name: "a binding gone from the store drops the stale cache",
		steps: []step{storedBinding, {
			serve: replies{dd.VerbBindingGet: dd.BindingGetReply{Found: false}},
			do:    readBinding, want: binding{},
		}, {do: readBinding, want: noBinding}},
	}, {
		name: "a confirmed bind carries the installer and seeds the cache",
		steps: []step{{
			serve: replies{dd.VerbBindingSet: dd.BindingSetReply{}},
			do:    call(store.BindGuild, installed), want: nil,
			sent: replies{dd.VerbBindingSet: dd.BindingSetRequest{GuildID: "g1", BroadcasterID: 77, InstalledBy: "discord-user-9"}},
		}, {do: readBinding, want: cachedBinding}},
	}, {
		name:  "an unreachable store fails the bind and caches nothing",
		steps: []step{{do: call(store.BindGuild, bound), want: nats.ErrNoResponders}, {do: readBinding, want: noBinding}},
	}, {
		name: "a guild bound elsewhere is refused and caches nothing",
		steps: []step{{
			serve: replies{dd.VerbBindingSet: dd.BindingSetReply{Error: "already bound", Code: dd.CodeBoundElsewhere}},
			do:    call(store.BindGuild, bound), want: discordstore.ErrBoundElsewhere,
		}, {do: readBinding, want: noBinding}},
	}, {
		name:  "a failed unbind keeps the cached binding",
		steps: []step{storedBinding, {do: call(store.UnbindGuild, bound), want: nats.ErrNoResponders}, {do: readBinding, want: cachedBinding}},
	}, {
		name: "a confirmed unbind names the owner and drops the cache",
		steps: []step{storedBinding, {
			serve: replies{dd.VerbBindingDelete: dd.BindingDeleteReply{}},
			do:    call(store.UnbindGuild, bound), want: nil,
			sent: replies{dd.VerbBindingDelete: dd.BindingDeleteRequest{GuildID: "g1", BroadcasterID: 77}},
		}, {do: readBinding, want: noBinding}},
	}, nonNumericBroadcaster()}
}

func nonNumericBroadcaster() scenario {
	nope := discordstore.Binding{Guild: g1, Broadcaster: discordstore.Broadcaster{ID: "nope"}}
	refused := errors.New("discordstore: broadcaster id must be numeric")
	writes := replies{
		dd.VerbBindingSet:               dd.BindingSetReply{},
		dd.VerbBindingDelete:            dd.BindingDeleteReply{},
		dd.VerbBindingListByBroadcaster: dd.BindingListByBroadcasterReply{},
		dd.VerbConfigSet:                dd.ConfigSetReply{},
	}
	untouched := replies{}
	for v := range writes {
		untouched[v] = unsent{}
	}
	return scenario{
		name: "a non-numeric broadcaster never reaches discord-data",
		steps: []step{
			{serve: writes, do: call(store.BindGuild, nope), want: refused, sent: untouched},
			{serve: writes, do: call(store.UnbindGuild, nope), want: refused, sent: untouched},
			{serve: writes, do: callResult(store.GuildsOf, nope.Broadcaster), want: outcome([]discordstore.Binding(nil), refused), sent: untouched},
			{
				serve: writes, do: callResult(store.SetGuildConfig, discordstore.SetConfig{Guild: g1, Broadcaster: nope.Broadcaster}),
				want: outcome(0, refused), sent: untouched,
			},
		},
	}
}

func guildListScenarios() []scenario {
	listing := dd.BindingListByBroadcasterReply{Guilds: []dd.Binding{
		{GuildID: "g1", BoundAtUnixMs: 111, InstalledBy: "u9"},
		{GuildID: "g2", BoundAtUnixMs: 222},
	}}
	listed := []discordstore.Binding{
		{Guild: g1, Broadcaster: owner, InstalledBy: "u9", BoundAtUnixMs: 111},
		{Guild: g2, Broadcaster: owner, BoundAtUnixMs: 222},
	}
	unavailable := outcome([]discordstore.Binding(nil), discordstore.ErrStoreUnavailable)
	ownedG2 := discordstore.Binding{Guild: g2, Broadcaster: owner}
	failing := replies{dd.VerbBindingListByBroadcaster: dd.BindingListByBroadcasterReply{Error: "db down"}}
	return []scenario{{
		name: "the guild list keeps order and bind times, then serves its cache through an outage",
		steps: []step{{
			serve: replies{dd.VerbBindingListByBroadcaster: listing},
			do:    callResult(store.GuildsOf, owner), want: outcome(listed, nil),
			sent: replies{dd.VerbBindingListByBroadcaster: dd.BindingListByBroadcasterRequest{BroadcasterID: 77}},
		}, {do: callResult(store.GuildsOf, owner), want: outcome(listed, nil)}},
	}, {
		name: "a bind invalidates the cached guild list",
		steps: []step{
			{serve: replies{dd.VerbBindingListByBroadcaster: listing}, do: callResult(store.GuildsOf, owner), want: outcome(listed, nil)},
			{serve: replies{dd.VerbBindingSet: dd.BindingSetReply{}}, do: call(store.BindGuild, ownedG2), want: nil},
			{do: callResult(store.GuildsOf, owner), want: unavailable},
		},
	}, {
		name: "the guild list fails loudly when discord-data cannot say",
		steps: []step{
			{do: callResult(store.GuildsOf, owner), want: unavailable},
			{serve: failing, do: callResult(store.GuildsOf, owner), want: unavailable},
		},
	}}
}

func configScenarios() []scenario {
	live := ddiscord.Config{LiveChannelID: "123"}
	stored := settings{Config: live, Version: 3, OK: true}
	readStored := step{
		serve: replies{dd.VerbConfigGet: dd.ConfigGetReply{Config: live, Version: 3, Found: true}},
		do:    readConfig, want: stored,
		sent: replies{dd.VerbConfigGet: dd.ConfigGetRequest{GuildID: "g1"}},
	}
	write := callResult(store.SetGuildConfig, discordstore.SetConfig{Guild: g1, Broadcaster: owner, Config: live, ExpectedVersion: 3})
	refusing := func(reply dd.ConfigSetReply, want error) step {
		return step{serve: replies{dd.VerbConfigSet: reply}, do: write, want: outcome(0, want)}
	}
	return []scenario{{
		name:  "settings are cached and served through an outage",
		steps: []step{readStored, {do: readConfig, want: stored}},
	}, {
		name: "a deleted settings row drops the cache",
		steps: []step{readStored, {
			serve: replies{dd.VerbConfigGet: dd.ConfigGetReply{Found: false}},
			do:    readConfig, want: settings{},
		}, {do: readConfig, want: settings{}}},
	}, {
		name: "a settings write invalidates the cache and maps every refusal",
		steps: []step{readStored, {
			serve: replies{dd.VerbConfigSet: dd.ConfigSetReply{Version: 4}},
			do:    write, want: outcome(4, nil),
			sent: replies{dd.VerbConfigSet: dd.ConfigSetRequest{GuildID: "g1", BroadcasterID: 77, Config: live, ExpectedVersion: 3}},
		},
			{do: readConfig, want: settings{}},
			refusing(dd.ConfigSetReply{Error: "stale", Code: dd.CodeConflict}, discordstore.ErrConfigConflict),
			refusing(dd.ConfigSetReply{Error: "not yours", Code: dd.CodeNotBound}, discordstore.ErrNotBound),
			refusing(dd.ConfigSetReply{Error: "db down", Code: dd.CodeInternal}, errors.New("db down")),
			{do: write, want: outcome(0, nats.ErrNoResponders)},
		},
	}, {
		name:  "Invalidate drops the cached settings",
		steps: []step{readStored, {do: invalidate, want: nil}, {do: readConfig, want: settings{}}},
	}}
}

func ticketScenarios() []scenario {
	open := discordstore.TicketOpen{
		GuildID: "g1", ChannelID: "c1", OpenerID: "u1", Subject: "help", PanelMessageID: "m1", OpenLimit: 2,
	}
	ticket := discordstore.Ticket{
		ID: 1, ChannelID: "c1", GuildID: "g1", OpenerID: "u1", Status: discordstore.TicketStatusOpen, PanelMessageID: "m1",
	}
	closing := discordstore.TicketClose{GuildID: "g1", ChannelID: "c1", ClosedBy: "mod1", ArchivedChannelID: "a1"}
	claim := discordstore.TicketClaim{GuildID: "g1", ChannelID: "c1", StaffID: "mod1"}
	return []scenario{{
		name: "a ticket opens, resolves in its guild and closes",
		steps: []step{{
			serve: replies{dd.VerbTicketOpen: dd.TicketOpenReply{TicketID: 1, OpenCount: 1}},
			do:    callResult(store.TrackTicket, open), want: outcome(discordstore.TicketOpenResult{TicketID: 1, OpenCount: 1}, nil),
			sent: replies{dd.VerbTicketOpen: dd.TicketOpenRequest{
				GuildID: "g1", ChannelID: "c1", OpenerID: "u1", Subject: "help", OpenLimit: 2, PanelMessageID: "m1",
			}},
		}, {
			serve: replies{dd.VerbTicketGet: dd.TicketGetReply{Found: true, Ticket: dd.Ticket{
				ID: 1, GuildID: "g1", ChannelID: "c1", OpenerID: "u1", Status: dd.StatusOpen, PanelMessageID: "m1",
			}}},
			do: readTicket(c1), want: pair(ticket, true),
			sent: replies{dd.VerbTicketGet: dd.TicketGetRequest{GuildID: "g1", ChannelID: "c1"}},
		}, {
			serve: replies{dd.VerbTicketClose: dd.TicketCloseReply{TicketID: 1, OpenerID: "u1"}},
			do:    call(store.CloseTicket, closing), want: nil,
			sent: replies{dd.VerbTicketClose: dd.TicketCloseRequest{GuildID: "g1", ChannelID: "c1", ClosedBy: "mod1", ArchivedChannelID: "a1"}},
		}},
	}, {
		name: "the open limit is a refusal with the held count, other refusals fail the open",
		steps: []step{{
			serve: replies{dd.VerbTicketOpen: dd.TicketOpenReply{OpenCount: 2, Refusal: rpc.Refused(dd.CodeLimit, "limit")}},
			do:    callResult(store.TrackTicket, open), want: outcome(discordstore.TicketOpenResult{OpenCount: 2, AtLimit: true}, nil),
		}, {
			serve: replies{dd.VerbTicketOpen: dd.TicketOpenReply{Refusal: rpc.Refused(dd.CodeInternal, "db down")}},
			do:    callResult(store.TrackTicket, open), want: outcome(discordstore.TicketOpenResult{}, errors.New("db down")),
		}},
	}, {
		name: "claims, counts and transcripts carry their fields",
		steps: []step{{
			serve: replies{dd.VerbTicketClaim: dd.TicketClaimReply{TicketID: 1}},
			do:    call(store.ClaimTicket, claim), want: nil,
			sent: replies{dd.VerbTicketClaim: dd.TicketClaimRequest{GuildID: "g1", ChannelID: "c1", StaffID: "mod1"}},
		}, {
			serve: replies{dd.VerbTicketCount: dd.TicketCountReply{Count: 2}},
			do:    call(store.OpenTicketCount, u1), want: 2,
			sent: replies{dd.VerbTicketCount: dd.TicketCountRequest{GuildID: "g1", OpenerID: "u1"}},
		}, {
			serve: replies{dd.VerbTranscriptPut: dd.TranscriptPutReply{}},
			do:    call(store.PutTranscript, discordstore.Transcript{TicketID: 1, Body: "body", MessageCount: 2}), want: nil,
			sent: replies{dd.VerbTranscriptPut: dd.TranscriptPutRequest{TicketID: 1, Body: "body", MessageCount: 2}},
		}, {
			serve: replies{dd.VerbTranscriptPut: dd.TranscriptPutReply{}},
			do:    call(store.PutTranscript, discordstore.Transcript{Body: "untracked"}), want: nil,
			sent: replies{dd.VerbTranscriptPut: unsent{}},
		}},
	}, {
		name: "an unreachable store reads no ticket, counts none and fails the writes",
		steps: []step{
			{do: readTicket(c1), want: pair(discordstore.Ticket{}, false)},
			{do: callResult(store.TrackTicket, open), want: outcome(discordstore.TicketOpenResult{}, nats.ErrNoResponders)},
			{do: call(store.OpenTicketCount, u1), want: 0},
			{do: call(store.ClaimTicket, claim), want: nats.ErrNoResponders},
		},
	}}
}

func xpScenarios() []scenario {
	return []scenario{{
		name: "xp is awarded once per cooldown and the held message reads the rank",
		steps: []step{{
			serve: replies{dd.VerbXPAdd: dd.XPAddReply{XPValue: 100, Level: 1, LeveledUp: true}},
			do:    addXP, want: award{XP: 100, Leveled: true, Level: 1},
			sent: replies{dd.VerbXPAdd: dd.XPAddRequest{GuildID: "g1", UserID: "u1", Delta: 15}},
		}, {
			serve: replies{dd.VerbXPGet: dd.XPGetReply{XPValue: 100, Level: 1, Found: true}},
			do:    addXP, want: award{XP: 100, Level: 1},
		}},
	}, {
		name: "the daily is granted once and the rank reads the store",
		steps: []step{{
			serve: replies{dd.VerbXPDaily: dd.XPDailyReply{Granted: true, XPValue: 50}},
			do:    claimDaily, want: daily{Granted: true, XP: 50},
			sent: replies{dd.VerbXPDaily: dd.XPDailyRequest{GuildID: "g1", UserID: "u1", Amount: 50}},
		}, {
			serve: replies{dd.VerbXPDaily: dd.XPDailyReply{Granted: false, XPValue: 50}},
			do:    claimDaily, want: daily{XP: 50},
		}, {
			serve: replies{dd.VerbXPGet: dd.XPGetReply{XPValue: 400, Level: 2, Found: true}},
			do:    readRank, want: rank{XP: 400, Level: 2},
			sent: replies{dd.VerbXPGet: dd.XPGetRequest{GuildID: "g1", UserID: "u1"}},
		}},
	}, {
		name: "xp reads zero when discord-data is unreachable",
		steps: []step{
			{do: readRank, want: rank{}},
			{do: claimDaily, want: daily{}},
			{do: addXP, want: award{}},
			{serve: replies{dd.VerbXPGet: dd.XPGetReply{Error: "db down"}}, do: readRank, want: rank{}},
		},
	}}
}

func localScenarios() []scenario {
	clone := discordstore.Clone{ChannelID: "v1", GuildID: "g1", OwnerID: "u1"}
	return []scenario{{
		name: "clones, the desk lock and voice seats work with discord-data down",
		steps: []step{
			{do: call(store.TrackClone, clone), want: nil},
			{do: callFound(store.Clone, discordstore.Channel{ID: "v1"}), want: pair(clone, true)},
			{do: call(store.CloneCount, g1), want: 1},
			{do: call(store.ClaimDesk, g1), want: true},
			{do: call(store.ClaimDesk, g1), want: false},
			{do: moveVoice(discordstore.VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "v1"}), want: seat{}},
			{do: ticketsDurable, want: true},
		},
	}}
}
