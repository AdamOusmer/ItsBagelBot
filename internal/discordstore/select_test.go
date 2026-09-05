// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func observed(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.WarnLevel)
	return zap.New(core), logs
}

func TestSelectFallsBackToValkeyWithAWarn(t *testing.T) {
	cases := []struct {
		name string
		sel  Selection
		want string
	}{
		{"disabled", Selection{Enabled: false}, "discord-data is disabled"},
		{"enabled without a connection", Selection{Enabled: true}, "no RPC connection"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			log, logs := observed(t)
			sel := tc.sel
			sel.Log = log

			store := Select(sel)

			// A nil valkey client yields the in-memory double, which is what
			// makes the fallback observable without a live Valkey.
			if _, ok := store.(*Mem); !ok {
				t.Fatalf("store = %T, want the pure-Valkey store", store)
			}
			entries := logs.All()
			if len(entries) != 1 || !strings.Contains(entries[0].Message, tc.want) {
				t.Fatalf("warnings = %+v, want one mentioning %q", entries, tc.want)
			}
		})
	}
}

func TestSelectNilLoggerDoesNotPanic(t *testing.T) {
	if got := Select(Selection{}); got == nil {
		t.Fatal("Select must always return a usable store")
	}
}

func TestDefaultRPCPrefixIsTheDataServicesSubject(t *testing.T) {
	if DefaultRPCPrefix != "bagel.rpc.discord-data" {
		t.Fatalf("DefaultRPCPrefix = %q", DefaultRPCPrefix)
	}
	if got := prefixOr(""); got != DefaultRPCPrefix {
		t.Fatalf("prefixOr(\"\") = %q", got)
	}
	if got := prefixOr("bagel.rpc.other"); got != "bagel.rpc.other" {
		t.Fatalf("prefixOr = %q", got)
	}
}

func TestMemEnforcesTheOpenLimitAndNumbersTickets(t *testing.T) {
	m := NewMem()
	ctx := context.Background()
	opener := Member{GuildID: "g1", UserID: "u1"}

	first, err := m.TrackTicket(ctx, TicketOpen{GuildID: "g1", ChannelID: "c1", OpenerID: "u1", OpenLimit: 2, PanelMessageID: "m1"})
	if err != nil || first.AtLimit || first.TicketID == 0 || first.OpenCount != 1 {
		t.Fatalf("first = %+v, err = %v", first, err)
	}
	if got := m.OpenTicketCount(ctx, opener); got != 1 {
		t.Fatalf("count = %d", got)
	}

	second, _ := m.TrackTicket(ctx, TicketOpen{GuildID: "g1", ChannelID: "c2", OpenerID: "u1", OpenLimit: 2})
	if second.AtLimit {
		t.Fatal("the second open is still inside a limit of 2")
	}

	third, _ := m.TrackTicket(ctx, TicketOpen{GuildID: "g1", ChannelID: "c3", OpenerID: "u1", OpenLimit: 2})
	if !third.AtLimit || third.OpenCount != 2 {
		t.Fatalf("third = %+v, want a refusal carrying the held count", third)
	}
	if _, ok := m.Ticket(ctx, Channel{ID: "c3"}); ok {
		t.Fatal("a refused open must not leave a row")
	}
}

func TestMemClaimAndCloseMoveTheTicketOn(t *testing.T) {
	m := NewMem()
	ctx := context.Background()
	if _, err := m.TrackTicket(ctx, TicketOpen{GuildID: "g1", ChannelID: "c1", OpenerID: "u1", PanelMessageID: "m1"}); err != nil {
		t.Fatalf("TrackTicket: %v", err)
	}

	if err := m.ClaimTicket(ctx, TicketClaim{GuildID: "g1", ChannelID: "c1", StaffID: "mod1"}); err != nil {
		t.Fatalf("ClaimTicket: %v", err)
	}
	got, _ := m.Ticket(ctx, Channel{ID: "c1"})
	if got.ClaimedBy != "mod1" || got.Status != TicketStatusClaimed || got.PanelMessageID != "m1" {
		t.Fatalf("ticket = %+v", got)
	}

	if err := m.PutTranscript(ctx, Transcript{TicketID: got.ID, Body: "body", MessageCount: 2}); err != nil {
		t.Fatalf("PutTranscript: %v", err)
	}
	stored, ok := m.Transcript(got.ID)
	if !ok || stored.Body != "body" || stored.MessageCount != 2 {
		t.Fatalf("transcript = %+v, %v", stored, ok)
	}

	if err := m.CloseTicket(ctx, TicketClose{GuildID: "g1", ChannelID: "c1", ClosedBy: "mod1"}); err != nil {
		t.Fatalf("CloseTicket: %v", err)
	}
	if _, ok := m.Ticket(ctx, Channel{ID: "c1"}); ok {
		t.Fatal("a closed ticket leaves the live index")
	}
}

func TestMemDeskRemembersThePanelMessage(t *testing.T) {
	m := NewMem()
	ctx := context.Background()

	if _, ok := m.Desk(ctx, Guild{ID: "g1"}); ok {
		t.Fatal("no desk yet")
	}
	if err := m.RememberDesk(ctx, DeskPanel{GuildID: "g1", ChannelID: "c1", MessageID: "m1"}); err != nil {
		t.Fatalf("RememberDesk: %v", err)
	}
	got, ok := m.Desk(ctx, Guild{ID: "g1"})
	if !ok || got.ChannelID != "c1" || got.MessageID != "m1" {
		t.Fatalf("desk = %+v, %v", got, ok)
	}
	if m.ClaimDesk(ctx, Guild{ID: "g1"}) {
		t.Fatal("a remembered desk is already claimed")
	}
}

func TestParseDeskValueReadsTheLegacyClaim(t *testing.T) {
	got, ok := parseDeskValue("g1", deskClaimed)
	if !ok || got.MessageID != "" || got.GuildID != "g1" {
		t.Fatalf("legacy claim = %+v, %v", got, ok)
	}
	got, ok = parseDeskValue("g1", "c1|m1")
	if !ok || got.ChannelID != "c1" || got.MessageID != "m1" {
		t.Fatalf("panel = %+v, %v", got, ok)
	}
}

func TestParseTicketValueReadsBothWidths(t *testing.T) {
	// The pipe-joined value gained two fields; a value written by an older
	// build must still parse, with the new halves empty.
	old, ok := parseTicketValue("c1", "g1|u1")
	if !ok || old.GuildID != "g1" || old.OpenerID != "u1" || old.ClaimedBy != "" || old.Status != TicketStatusOpen {
		t.Fatalf("legacy value = %+v, %v", old, ok)
	}
	full, ok := parseTicketValue("c1", ticketValue(Ticket{GuildID: "g1", OpenerID: "u1", ClaimedBy: "mod1", PanelMessageID: "m1"}))
	if !ok || full.ClaimedBy != "mod1" || full.PanelMessageID != "m1" || full.Status != TicketStatusClaimed {
		t.Fatalf("value = %+v, %v", full, ok)
	}
	if _, ok := parseTicketValue("c1", "garbage"); ok {
		t.Fatal("a value with no separator is not a ticket")
	}
}
