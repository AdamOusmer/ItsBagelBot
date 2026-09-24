// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strings"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"
)

func wantFollowup(t *testing.T, cmds []ddiscord.Command, want string) {
	t.Helper()
	if got := followupText(t, cmds); got != want {
		t.Fatalf("followup = %q, want %q", got, want)
	}
}

func wantFollowupContains(t *testing.T, cmds []ddiscord.Command, want string) {
	t.Helper()
	if got := followupText(t, cmds); !strings.Contains(got, want) {
		t.Fatalf("followup = %q, want it to mention %q", got, want)
	}
}

func (f *deskFixture) wantTicketRow(t *testing.T, live bool, why string) {
	t.Helper()
	_, ok := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"})
	if ok != live {
		t.Fatal(why)
	}
}

func (f *deskFixture) addPress(t *testing.T, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	sub := decode.InteractionOption{Name: "add", Options: []decode.InteractionOption{
		{Name: "user", Type: 6, Value: codec.RawMessage(`"u7"`)},
	}}
	return f.deskPress(t, in, func(ctx context.Context, call deskCall) error {
		return f.mod.add(ctx, call, sub)
	})
}

func TestTicketOpenCreatesRecordsAndAnswers(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if len(f.tickets.opened) != 1 {
		t.Fatalf("open requests = %+v", f.tickets.opened)
	}
	wantOpenRequest(t, f.tickets.opened[0])
	wantRenamedTo(t, f.tickets.renamed, "ticket-ada-1")
	f.wantStoredTicket(t, "u1", "m-new")
	wantFollowupContains(t, cmds, "<#c-new>")
}

func TestTicketChannelNumberComesFromTheRowNotTheCount(t *testing.T) {
	f := newDesk(t, baseConfig())
	ctx := context.Background()

	f.press(t, f.mod.open, opener("u1", "Ada"))
	if err := f.store.CloseTicket(ctx, discordstore.TicketClose{GuildID: "g1", ChannelID: "c-new"}); err != nil {
		t.Fatalf("close the first ticket: %v", err)
	}

	f.tickets.openReply = discordoutgress.TicketOpenReply{ChannelID: "c-2", MessageID: "m-2"}
	f.press(t, f.mod.open, opener("u1", "Ada"))

	if got := f.store.OpenTicketCount(ctx, discordstore.Member{GuildID: "g1", UserID: "u1"}); got != 1 {
		t.Fatalf("held tickets = %d, want 1 (so held+1 would name this one -1)", got)
	}
	if len(f.tickets.renamed) != 2 || f.tickets.renamed[1].Name != "ticket-ada-2" {
		t.Fatalf("rename = %+v, want ticket-ada-2", f.tickets.renamed)
	}
}
func TestTicketOpenRefusesAtTheLimitWithoutTouchingDiscord(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.press(t, f.mod.open, opener("u1", "Ada"))

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if len(f.tickets.opened) != 1 {
		t.Fatalf("a refused open must not create a channel: %+v", f.tickets.opened)
	}
	wantFollowup(t, cmds, "You already have 1 open ticket.")
}
func TestTicketOpenRollsTheChannelBackWhenTheRowLoses(t *testing.T) {
	cfg := baseConfig()
	cfg.TicketOpenLimit = "2"
	f := newDesk(t, cfg)
	ctx := context.Background()
	_, _ = f.store.TrackTicket(ctx, discordstore.TicketOpen{GuildID: "g1", ChannelID: "old1", OpenerID: "u1"})
	_, _ = f.store.TrackTicket(ctx, discordstore.TicketOpen{GuildID: "g1", ChannelID: "old2", OpenerID: "u1"})

	cmds := f.createTicketPress(t, opener("u1", "Ada"))

	if len(f.tickets.deleted) != 1 || f.tickets.deleted[0] != "c-new" {
		t.Fatalf("rollback deletes = %v", f.tickets.deleted)
	}
	wantFollowupContains(t, cmds, "already have 2")
}
func TestTicketOpenSaysSoWhenTicketsAreOff(t *testing.T) {
	cfg := baseConfig()
	cfg.TicketsEnabled = "off"
	f := newDesk(t, cfg)

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	wantFollowup(t, cmds, "Tickets are off.")
	if len(f.tickets.opened) != 0 {
		t.Fatal("tickets off must not create a channel")
	}
}
func TestTicketOpenRollsBackWhenTheCardFails(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.tickets.openReply = discordoutgress.TicketOpenReply{ChannelID: "c-new", Error: "missing permissions"}

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if len(f.tickets.deleted) != 1 {
		t.Fatalf("an orphan channel must be rolled back: %v", f.tickets.deleted)
	}
	f.wantTicketRow(t, false, "a failed open must not leave a row")
	wantFollowup(t, cmds, "Could not open a ticket right now.")
}
func TestTicketClaimRefusesNonStaff(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)

	cmds := f.press(t, f.mod.claim, inTicket("u2", []string{"stranger"}))

	wantFollowup(t, cmds, "Only ticket staff can claim this.")
	if len(f.tickets.claimed) != 0 {
		t.Fatal("a refused claim must not edit the card")
	}
}

func TestTicketClaimRecordsAndEditsTheCardOnce(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)

	cmds := f.press(t, f.mod.claim, inTicket("mod1", []string{"helper"}))

	wantFollowup(t, cmds, "Claimed.")
	if len(f.tickets.claimed) != 1 || f.tickets.claimed[0].MessageID != "m-new" {
		t.Fatalf("claim request = %+v", f.tickets.claimed)
	}
	if !strings.Contains(f.tickets.claimed[0].Embed.Footer.Text, "Claimed by") {
		t.Fatalf("claim embed footer = %+v", f.tickets.claimed[0].Embed.Footer)
	}
	stored, _ := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"})
	if stored.ClaimedBy != "mod1" {
		t.Fatalf("stored claim = %+v", stored)
	}

	cmds = f.press(t, f.mod.claim, inTicket("mod2", []string{"helper"}))
	wantFollowupContains(t, cmds, "already claimed by <@mod1>")
	if len(f.tickets.claimed) != 1 {
		t.Fatalf("claim requests = %d, want the first one only", len(f.tickets.claimed))
	}
}

func TestTicketClaimOnANonTicketChannel(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.press(t, f.mod.claim, inTicket("mod1", []string{"helper"}))

	wantFollowup(t, cmds, "This is not a ticket.")
}
func TestTicketCloseByOpenerAndByStaff(t *testing.T) {
	cases := []struct {
		name  string
		by    decode.InteractionEvent
		allow bool
	}{
		{"opener", inTicket("u1", nil), true},
		{"desk helper", inTicket("mod1", []string{"helper"}), true},
		{"stranger", inTicket("u9", []string{"nobody"}), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDesk(t, baseConfig())
			f.openOneTicket(t)

			cmds := f.press(t, f.mod.close, tc.by)

			if !tc.allow {
				wantFollowup(t, cmds, "Only the opener or ticket staff can close this.")
				if len(f.tickets.closed) != 0 {
					t.Fatal("a refused close must not reach outgress")
				}
				return
			}
			wantFollowup(t, cmds, "Ticket closed.")
			if len(f.tickets.closed) != 1 {
				t.Fatalf("close requests = %+v", f.tickets.closed)
			}
			f.wantTicketRow(t, false, "a closed ticket leaves the live index")
		})
	}
}

func TestTicketCloseCarriesTheGuildsTranscriptAndArchiveSettings(t *testing.T) {
	cases := []struct {
		name           string
		transcript     string
		archive        string
		wantTranscript bool
		wantArchive    string
	}{
		{"transcript on, archived", "", "arch1", true, "arch1"},
		{"transcript off, deleted", "off", "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.TicketTranscriptEnabled = tc.transcript
			cfg.TicketArchiveCategoryID = tc.archive
			f := newDesk(t, cfg)
			f.closeReplyFor(tc.wantTranscript, tc.wantArchive)
			ticket := f.openOneTicket(t)

			f.press(t, f.mod.close, inTicket("u1", nil))

			wantCloseRequest(t, f.tickets.closed[0], tc.wantTranscript, tc.wantArchive)
			f.wantTranscriptStored(t, ticket.ID, tc.wantTranscript)
		})
	}
}

func wantCloseRequest(t *testing.T, req discordoutgress.TicketCloseRequest, transcript bool, archive string) {
	t.Helper()
	if req.Transcript != transcript {
		t.Fatalf("transcript flag = %v", req.Transcript)
	}
	if req.ArchiveCategoryID != archive {
		t.Fatalf("archive category = %q", req.ArchiveCategoryID)
	}
	if req.LogChannelID != "log1" {
		t.Fatalf("close request = %+v, want log channel log1", req)
	}
	if req.ChannelName != "ticket-ada-1" {
		t.Fatalf("close request = %+v, want channel name ticket-ada-1", req)
	}
}

func (f *deskFixture) wantTranscriptStored(t *testing.T, ticketID int, want bool) {
	t.Helper()
	stored, ok := f.store.Transcript(ticketID)
	if want != ok {
		t.Fatalf("transcript stored = %v, want %v", ok, want)
	}
	if ok && stored.Body != "rendered" {
		t.Fatalf("transcript = %+v", stored)
	}
}
func TestTicketCloseKeepsTheRowWhenOutgressFails(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)
	f.tickets.closeReply = discordoutgress.TicketCloseReply{Error: "missing permissions"}

	cmds := f.press(t, f.mod.close, inTicket("u1", nil))

	wantFollowup(t, cmds, "Could not close this ticket right now.")
	f.wantTicketRow(t, true, "a ticket Discord still shows must still be a ticket here")
}
func TestTicketAddGrantsAccess(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)
	cmds := f.addPress(t, inTicket("u1", nil))

	if len(f.tickets.added) != 1 {
		t.Fatalf("add requests = %+v, want exactly one", f.tickets.added)
	}
	if f.tickets.added[0].UserID != "u7" {
		t.Fatalf("add request = %+v, want the user the option named", f.tickets.added[0])
	}
	wantFollowupContains(t, cmds, "<@u7>")
}

func TestTicketAddRefusesAStranger(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)
	cmds := f.addPress(t, inTicket("u9", []string{"nobody"}))

	if len(f.tickets.added) != 0 {
		t.Fatalf("add requests = %+v", f.tickets.added)
	}
	wantFollowup(t, cmds, "Only the opener or ticket staff can add someone.")
}

func TestRPCFailedCoversBothShapes(t *testing.T) {
	if !rpcFailed(errors.New("boom"), "") {
		t.Fatal("a transport error must count as a failure")
	}
	if !rpcFailed(nil, "missing permissions") {
		t.Fatal("an error string in the reply must count as a failure")
	}
	if rpcFailed(nil, "") {
		t.Fatal("a clean reply is not a failure")
	}
}

func TestTicketActionsRefuseAnotherGuildsTicket(t *testing.T) {
	const refusal = "This is not a ticket."
	cases := []struct {
		name string
		run  func(f *deskFixture, in decode.InteractionEvent) []ddiscord.Command
	}{
		{name: "claim", run: func(f *deskFixture, in decode.InteractionEvent) []ddiscord.Command {
			return f.press(t, f.mod.claim, in)
		}},
		{name: "close", run: func(f *deskFixture, in decode.InteractionEvent) []ddiscord.Command {
			return f.press(t, f.mod.close, in)
		}},
		{name: "add", run: func(f *deskFixture, in decode.InteractionEvent) []ddiscord.Command {
			return f.deskPress(t, in, func(ctx context.Context, call deskCall) error {
				return f.mod.add(ctx, call, decode.InteractionOption{Name: "add"})
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newDesk(t, baseConfig())
			f.store.SeedTicket(discordstore.Ticket{
				ID: 1, ChannelID: "c-new", GuildID: "g-other", OpenerID: "u1",
				Status: discordstore.TicketStatusOpen,
			})

			in := inTicket("u1", []string{"helper"})
			cmds := tc.run(f, in)

			wantFollowup(t, cmds, refusal)
			if len(f.tickets.claimed)+len(f.tickets.closed)+len(f.tickets.added) != 0 {
				t.Fatal("a cross-guild action must not reach outgress")
			}
		})
	}
}

func TestTicketCloseRefusesAnAlreadyClosedTicket(t *testing.T) {
	for _, status := range []string{discordstore.TicketStatusClosed, discordstore.TicketStatusArchived} {
		t.Run(status, func(t *testing.T) {
			f := newDesk(t, baseConfig())
			f.store.SeedTicket(discordstore.Ticket{
				ID: 1, ChannelID: "c-new", GuildID: "g1", OpenerID: "u1", Status: status,
			})

			cmds := f.press(t, f.mod.close, inTicket("u1", []string{"helper"}))

			wantFollowup(t, cmds, "This ticket is already closed.")
			if len(f.tickets.closed) != 0 {
				t.Fatalf("a second close must not run the sequence: %+v", f.tickets.closed)
			}
		})
	}
}

func TestTicketPendingCloseIsRetriedOnTheNextInteraction(t *testing.T) {
	f := newDesk(t, baseConfig())
	ctx := context.Background()
	f.store.SeedTicket(discordstore.Ticket{
		ID: 1, ChannelID: "c-new", GuildID: "g1", OpenerID: "u1", Status: discordstore.TicketStatusOpen,
	})
	done := discordstore.TicketClose{GuildID: "g1", ChannelID: "c-new", ClosedBy: "u1", ArchivedChannelID: "c-new"}
	if err := f.store.MarkPendingClose(ctx, done); err != nil {
		t.Fatalf("mark: %v", err)
	}

	f.press(t, f.mod.close, inTicket("u1", []string{"helper"}))

	if _, ok := f.store.PendingClose(ctx, discordstore.Channel{ID: "c-new"}); ok {
		t.Fatal("the marker must be cleared once the row takes the close")
	}
	f.wantTicketRow(t, false, "the retry must record the close")
	if len(f.tickets.closed) != 0 {
		t.Fatalf("the retry is a store write, not a second Discord close: %+v", f.tickets.closed)
	}
}

func TestTicketOpenRefusesWithoutADurableStore(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.mod.store = fallbackStore{Store: f.store}

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	wantFollowupContains(t, cmds, "unavailable")
	if len(f.tickets.opened) != 0 {
		t.Fatal("a refused open must not create a channel")
	}
}
