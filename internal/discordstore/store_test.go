// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore_test

import (
	"slices"
	"strings"
	"testing"

	"ItsBagelBot/internal/discordstore"
)

func TestMemStoreBehavesLikeTheDurableStore(t *testing.T) {
	scenarios := slices.Concat(memBindingScenarios(), memTicketScenarios(), memDeskScenarios(), memMemberScenarios(), memCacheScenarios())
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			runSteps(t, discordstore.NewMem(), nil, tc.steps)
		})
	}
}

func memBindingScenarios() []scenario {
	return []scenario{{
		name: "a bound guild resolves and lists until it is unbound",
		steps: []step{
			{do: call(store.BindGuild, bound), want: nil},
			{do: callFound(store.Broadcaster, g1), want: pair(owner, true)},
			{do: callResult(store.GuildsOf, owner), want: outcome([]discordstore.Binding{bound}, nil)},
			{do: call(store.UnbindGuild, discordstore.Binding{Guild: g1}), want: nil},
			{do: readBinding, want: binding{}},
		},
	}}
}

func memTicketScenarios() []scenario {
	opening := func(channel string) discordstore.TicketOpen {
		return discordstore.TicketOpen{GuildID: "g1", ChannelID: channel, OpenerID: "u1", OpenLimit: 2, PanelMessageID: "m1"}
	}
	done := discordstore.TicketClose{GuildID: "g1", ChannelID: "c1", ClosedBy: "u9", ArchivedChannelID: "c1"}
	return []scenario{{
		name: "TestMemEnforcesTheOpenLimitAndNumbersTickets",
		steps: []step{
			{do: callResult(store.TrackTicket, opening("c1")), want: outcome(discordstore.TicketOpenResult{TicketID: 1, OpenCount: 1}, nil)},
			{do: call(store.OpenTicketCount, u1), want: 1},
			{do: callResult(store.TrackTicket, opening("c2")), want: outcome(discordstore.TicketOpenResult{TicketID: 2, OpenCount: 2}, nil)},
			{do: callResult(store.TrackTicket, opening("c3")), want: outcome(discordstore.TicketOpenResult{OpenCount: 2, AtLimit: true}, nil)},
			{do: readTicket(discordstore.Channel{ID: "c3"}), want: pair(discordstore.Ticket{}, false)},
		},
	}, {
		name: "TestMemClaimAndCloseMoveTheTicketOn",
		steps: []step{
			{do: callResult(store.TrackTicket, opening("c1")), want: outcome(discordstore.TicketOpenResult{TicketID: 1, OpenCount: 1}, nil)},
			{do: call(store.ClaimTicket, discordstore.TicketClaim{GuildID: "g1", ChannelID: "c1", StaffID: "mod1"}), want: nil},
			{do: readTicket(c1), want: pair(discordstore.Ticket{
				ID: 1, ChannelID: "c1", GuildID: "g1", OpenerID: "u1",
				Status: discordstore.TicketStatusClaimed, ClaimedBy: "mod1", PanelMessageID: "m1",
			}, true)},
			{do: call(store.PutTranscript, discordstore.Transcript{TicketID: 1, Body: "body", MessageCount: 2}), want: nil},
			{do: memTranscript(1), want: pair(discordstore.Transcript{TicketID: 1, Body: "body", MessageCount: 2}, true)},
			{do: call(store.CloseTicket, discordstore.TicketClose{GuildID: "g1", ChannelID: "c1", ClosedBy: "mod1"}), want: nil},
			{do: readTicket(c1), want: pair(discordstore.Ticket{}, false)},
		},
	}, {
		name: "TestPendingCloseRoundTrips",
		steps: []step{
			{do: callFound(store.PendingClose, c1), want: pair(discordstore.TicketClose{}, false)},
			{do: call(store.MarkPendingClose, done), want: nil},
			{do: callFound(store.PendingClose, c1), want: pair(done, true)},
			{do: call(store.ClearPendingClose, c1), want: nil},
			{do: callFound(store.PendingClose, c1), want: pair(discordstore.TicketClose{}, false)},
		},
	}, {
		name: "TestClaimSummaryIsOncePerTicket",
		steps: []step{
			{do: call(store.ClaimSummary, 7), want: true},
			{do: call(store.ClaimSummary, 7), want: false},
			{do: call(store.ClaimSummary, 8), want: true},
			{do: call(store.ClaimSummary, 0), want: true},
			{do: call(store.ClaimSummary, 0), want: true},
		},
	}}
}

func memDeskScenarios() []scenario {
	panel := discordstore.DeskPanel{GuildID: "g1", ChannelID: "c1", MessageID: "m1"}
	return []scenario{{
		name: "the desk lock is claimable once and a remembered desk counts as claimed",
		steps: []step{
			{do: call(store.ClaimDesk, g1), want: true},
			{do: call(store.ClaimDesk, g1), want: false},
			{do: call(store.RememberDesk, discordstore.DeskPanel{GuildID: "g2"}), want: nil},
			{do: call(store.ClaimDesk, g2), want: false},
		},
	}, {
		name: "TestMemDeskRemembersThePanelMessage",
		steps: []step{
			{do: callFound(store.Desk, g1), want: pair(discordstore.DeskPanel{}, false)},
			{do: call(store.RememberDesk, panel), want: nil},
			{do: callFound(store.Desk, g1), want: pair(panel, true)},
			{do: call(store.ClaimDesk, g1), want: false},
		},
	}, {
		name: "TestRememberDeskNeverErasesAKnownPanel",
		steps: []step{
			{do: call(store.RememberDesk, panel), want: nil},
			{do: call(store.RememberDesk, discordstore.DeskPanel{GuildID: "g1", ChannelID: "c1"}), want: nil},
			{do: callFound(store.Desk, g1), want: pair(panel, true)},
		},
	}}
}

func memMemberScenarios() []scenario {
	at := func(user, channel string) op {
		return moveVoice(discordstore.VoiceSeat{GuildID: "g1", UserID: user, ChannelID: channel})
	}
	clone := discordstore.Clone{ChannelID: "v1", GuildID: "g1", OwnerID: "u1"}
	return []scenario{{
		name:  "xp is awarded once per cooldown",
		steps: []step{{do: addXP, want: award{XP: 15}}, {do: addXP, want: award{XP: 15}}},
	}, {
		name:  "the daily is granted once",
		steps: []step{{do: claimDaily, want: daily{Granted: true, XP: 50}}, {do: claimDaily, want: daily{XP: 50}}},
	}, {
		name: "clones are counted until forgotten",
		steps: []step{
			{do: call(store.TrackClone, clone), want: nil},
			{do: call(store.CloneCount, g1), want: 1},
			{do: call(store.ForgetClone, clone), want: nil},
			{do: call(store.CloneCount, g1), want: 0},
		},
	}, {
		name: "voice moves report the channel left and whether it emptied",
		steps: []step{
			{do: at("u1", "hub"), want: seat{}},
			{do: at("u2", "hub"), want: seat{}},
			{do: at("u1", "clone-1"), want: seat{Left: "hub"}},
			{do: at("u2", ""), want: seat{Left: "hub", Empty: true}},
		},
	}, {
		name:  "a same-channel voice update is not a leave",
		steps: []step{{do: at("u1", "hub"), want: seat{}}, {do: at("u1", "hub"), want: seat{Left: "hub"}}},
	}}
}

func TestTicketOverNamesTheTerminalStates(t *testing.T) {
	for _, status := range []string{discordstore.TicketStatusClosed, discordstore.TicketStatusArchived} {
		if !discordstore.TicketOver(status) {
			t.Fatalf("%q is terminal", status)
		}
	}
	for _, status := range []string{discordstore.TicketStatusOpen, discordstore.TicketStatusClaimed, ""} {
		if discordstore.TicketOver(status) {
			t.Fatalf("%q is not terminal", status)
		}
	}
}

func memCacheScenarios() []scenario {
	long := discordstore.CachedMessage{ID: "m1", GuildID: "g1", ChannelID: "c1", AuthorID: "u1", AuthorName: "Ada", Content: strings.Repeat("é", 2000)}
	clipped := long
	clipped.Content = strings.Repeat("é", 1024)
	role := discordstore.LabelRef{Kind: discordstore.LabelRole, GuildID: "g1", ID: "r1"}
	return []scenario{{
		name: "a remembered message is recalled with its content clipped to 1024 runes",
		steps: []step{
			{do: callFound(store.RecallMessage, discordstore.Message{ID: "m1"}), want: pair(discordstore.CachedMessage{}, false)},
			{do: call(store.RememberMessage, long), want: nil},
			{do: callFound(store.RecallMessage, discordstore.Message{ID: "m1"}), want: pair(clipped, true)},
		},
	}, {
		name: "member roles and labels are recalled after they are remembered",
		steps: []step{
			{do: callFound(store.RecallRoles, u1), want: pair([]string(nil), false)},
			{do: call(store.RememberRoles, discordstore.MemberRoles{Member: u1, Roles: []string{"r1"}}), want: nil},
			{do: callFound(store.RecallRoles, u1), want: pair([]string{"r1"}, true)},
			{do: callFound(store.RecallLabel, role), want: pair("", false)},
			{do: call(store.RememberLabel, discordstore.Label{Ref: role, Name: "Mods"}), want: nil},
			{do: callFound(store.RecallLabel, role), want: pair("Mods", true)},
		},
	}}
}
