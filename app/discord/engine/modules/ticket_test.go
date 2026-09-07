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

func TestTicketOpenCreatesRecordsAndAnswers(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if len(f.tickets.opened) != 1 {
		t.Fatalf("open requests = %+v", f.tickets.opened)
	}
	wantOpenRequest(t, f.tickets.opened[0])
	wantRenamedTo(t, f.tickets.renamed, "ticket-ada-1")
	f.wantStoredTicket(t, "u1", "m-new")
	if got := followupText(t, cmds); !strings.Contains(got, "<#c-new>") {
		t.Fatalf("followup = %q, want the channel mention", got)
	}
}

// A reopened ticket is numbered from the ROW id, not from how many the opener
// currently holds. The old count-based name gave this second channel
// "ticket-ada-1" all over again, and after archiving the guild had two
// indistinguishable "closed-ticket-ada-1" channels.
func TestTicketChannelNumberComesFromTheRowNotTheCount(t *testing.T) {
	f := newDesk(t, baseConfig()) // limit 1: the first must be closed first
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
	f := newDesk(t, baseConfig()) // default limit is 1
	f.press(t, f.mod.open, opener("u1", "Ada"))

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if len(f.tickets.opened) != 1 {
		t.Fatalf("a refused open must not create a channel: %+v", f.tickets.opened)
	}
	if got := followupText(t, cmds); got != "You already have 1 open ticket." {
		t.Fatalf("followup = %q", got)
	}
}
func TestTicketOpenRollsTheChannelBackWhenTheRowLoses(t *testing.T) {
	cfg := baseConfig()
	cfg.TicketOpenLimit = "2"
	f := newDesk(t, cfg)
	// Two rows already exist for this opener, so the pre-check passes only
	// because it ran before them: this is the racing-presses backstop.
	ctx := context.Background()
	_, _ = f.store.TrackTicket(ctx, discordstore.TicketOpen{GuildID: "g1", ChannelID: "old1", OpenerID: "u1"})
	_, _ = f.store.TrackTicket(ctx, discordstore.TicketOpen{GuildID: "g1", ChannelID: "old2", OpenerID: "u1"})

	cmds := f.createTicketPress(t, opener("u1", "Ada"))

	if len(f.tickets.deleted) != 1 || f.tickets.deleted[0] != "c-new" {
		t.Fatalf("rollback deletes = %v", f.tickets.deleted)
	}
	if got := followupText(t, cmds); !strings.Contains(got, "already have 2") {
		t.Fatalf("followup = %q", got)
	}
}
func TestTicketOpenSaysSoWhenTicketsAreOff(t *testing.T) {
	cfg := baseConfig()
	cfg.TicketsEnabled = "off"
	f := newDesk(t, cfg)

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if got := followupText(t, cmds); got != "Tickets are off." {
		t.Fatalf("followup = %q", got)
	}
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
	if _, ok := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"}); ok {
		t.Fatal("a failed open must not leave a row")
	}
	if got := followupText(t, cmds); got != "Could not open a ticket right now." {
		t.Fatalf("followup = %q", got)
	}
}
func TestTicketClaimRefusesNonStaff(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)

	cmds := f.press(t, f.mod.claim, inTicket("u2", []string{"stranger"}))

	if got := followupText(t, cmds); got != "Only ticket staff can claim this." {
		t.Fatalf("followup = %q", got)
	}
	if len(f.tickets.claimed) != 0 {
		t.Fatal("a refused claim must not edit the card")
	}
}

func TestTicketClaimRecordsAndEditsTheCardOnce(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)

	cmds := f.press(t, f.mod.claim, inTicket("mod1", []string{"helper"}))

	if got := followupText(t, cmds); got != "Claimed." {
		t.Fatalf("followup = %q", got)
	}
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

	// A second claim is refused rather than silently moving the ticket over.
	cmds = f.press(t, f.mod.claim, inTicket("mod2", []string{"helper"}))
	if got := followupText(t, cmds); !strings.Contains(got, "already claimed by <@mod1>") {
		t.Fatalf("second claim followup = %q", got)
	}
	if len(f.tickets.claimed) != 1 {
		t.Fatalf("claim requests = %d, want the first one only", len(f.tickets.claimed))
	}
}

func TestTicketClaimOnANonTicketChannel(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.press(t, f.mod.claim, inTicket("mod1", []string{"helper"}))

	if got := followupText(t, cmds); got != "This is not a ticket." {
		t.Fatalf("followup = %q", got)
	}
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
				if got := followupText(t, cmds); got != "Only the opener or ticket staff can close this." {
					t.Fatalf("followup = %q", got)
				}
				if len(f.tickets.closed) != 0 {
					t.Fatal("a refused close must not reach outgress")
				}
				return
			}
			if got := followupText(t, cmds); got != "Ticket closed." {
				t.Fatalf("followup = %q", got)
			}
			if len(f.tickets.closed) != 1 {
				t.Fatalf("close requests = %+v", f.tickets.closed)
			}
			if _, ok := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"}); ok {
				t.Fatal("a closed ticket leaves the live index")
			}
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

			req := f.tickets.closed[0]
			if req.Transcript != tc.wantTranscript {
				t.Fatalf("transcript flag = %v", req.Transcript)
			}
			if req.ArchiveCategoryID != tc.wantArchive {
				t.Fatalf("archive category = %q", req.ArchiveCategoryID)
			}
			if req.LogChannelID != "log1" || req.ChannelName != "ticket-ada-1" {
				t.Fatalf("close request = %+v", req)
			}
			stored, ok := f.store.Transcript(ticket.ID)
			if tc.wantTranscript != ok {
				t.Fatalf("transcript stored = %v, want %v", ok, tc.wantTranscript)
			}
			if ok && stored.Body != "rendered" {
				t.Fatalf("transcript = %+v", stored)
			}
		})
	}
}
func TestTicketCloseKeepsTheRowWhenOutgressFails(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)
	f.tickets.closeReply = discordoutgress.TicketCloseReply{Error: "missing permissions"}

	cmds := f.press(t, f.mod.close, inTicket("u1", nil))

	if got := followupText(t, cmds); got != "Could not close this ticket right now." {
		t.Fatalf("followup = %q", got)
	}
	if _, ok := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"}); !ok {
		t.Fatal("a ticket Discord still shows must still be a ticket here")
	}
}
func TestTicketAddGrantsAccess(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)
	sub := decode.InteractionOption{Name: "add", Options: []decode.InteractionOption{
		{Name: "user", Type: 6, Value: codec.RawMessage(`"u7"`)},
	}}
	in := inTicket("u1", nil)

	cmds := f.deskPress(t, in, func(ctx context.Context, call deskCall) error {
		return f.mod.add(ctx, call, sub)
	})

	if len(f.tickets.added) != 1 || f.tickets.added[0].UserID != "u7" {
		t.Fatalf("add requests = %+v", f.tickets.added)
	}
	if got := followupText(t, cmds); !strings.Contains(got, "<@u7>") {
		t.Fatalf("followup = %q", got)
	}
}

func TestTicketAddRefusesAStranger(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.openOneTicket(t)
	sub := decode.InteractionOption{Name: "add", Options: []decode.InteractionOption{
		{Name: "user", Type: 6, Value: codec.RawMessage(`"u7"`)},
	}}
	in := inTicket("u9", []string{"nobody"})

	cmds := f.deskPress(t, in, func(ctx context.Context, call deskCall) error {
		return f.mod.add(ctx, call, sub)
	})

	if len(f.tickets.added) != 0 {
		t.Fatalf("add requests = %+v", f.tickets.added)
	}
	if got := followupText(t, cmds); got != "Only the opener or ticket staff can add someone." {
		t.Fatalf("followup = %q", got)
	}
}

// rpcFailed treats a transport error and an error string in the reply the
// same way; the desk relies on that, so it is pinned here.
func TestRPCFailedCoversBothShapes(t *testing.T) {
	if !rpcFailed(errors.New("boom"), "") || !rpcFailed(nil, "missing permissions") {
		t.Fatal("both failure shapes must count")
	}
	if rpcFailed(nil, "") {
		t.Fatal("a clean reply is not a failure")
	}
}

// The ticket keyspace is keyed on the channel id alone, so an unscoped row
// written for one guild answers a lookup made from any other. Without the
// guild scope a member of guild B could claim, close or add people to guild
// A's private support channel. The scope lives in the Store call now, so the
// other guild's row simply does not resolve and the desk answers the same way
// it does for any non-ticket channel.
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

			in := inTicket("u1", []string{"helper"}) // opener AND staff: only the guild refuses
			cmds := tc.run(f, in)

			if got := followupText(t, cmds); got != refusal {
				t.Fatalf("followup = %q, want %q", got, refusal)
			}
			if len(f.tickets.claimed)+len(f.tickets.closed)+len(f.tickets.added) != 0 {
				t.Fatal("a cross-guild action must not reach outgress")
			}
		})
	}
}

// The close button stays pressable on a ticket that is already done: the row
// survives the close and an archived channel keeps its buttons. Running the
// sequence again would re-page a channel that may not exist and post a second
// summary.
func TestTicketCloseRefusesAnAlreadyClosedTicket(t *testing.T) {
	for _, status := range []string{discordstore.TicketStatusClosed, discordstore.TicketStatusArchived} {
		t.Run(status, func(t *testing.T) {
			f := newDesk(t, baseConfig())
			f.store.SeedTicket(discordstore.Ticket{
				ID: 1, ChannelID: "c-new", GuildID: "g1", OpenerID: "u1", Status: status,
			})

			cmds := f.press(t, f.mod.close, inTicket("u1", []string{"helper"}))

			if got := followupText(t, cmds); got != "This ticket is already closed." {
				t.Fatalf("followup = %q", got)
			}
			if len(f.tickets.closed) != 0 {
				t.Fatalf("a second close must not run the sequence: %+v", f.tickets.closed)
			}
		})
	}
}

// A close Discord performed but the row never took is retried on the next
// interaction that touches the channel. Without it the row says "open"
// forever and the opener's count never comes back down.
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
	if _, ok := f.store.Ticket(ctx, discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"}); ok {
		t.Fatal("the retry must record the close")
	}
	if len(f.tickets.closed) != 0 {
		t.Fatalf("the retry is a store write, not a second Discord close: %+v", f.tickets.closed)
	}
}

// The pure-Valkey fallback cannot number, cap or transcribe a ticket, so the
// desk refuses rather than handing out a channel it can never manage.
func TestTicketOpenRefusesWithoutADurableStore(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.mod.store = fallbackStore{Store: f.store}

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if got := followupText(t, cmds); !strings.Contains(got, "unavailable") {
		t.Fatalf("followup = %q, want the desk to say it is unavailable", got)
	}
	if len(f.tickets.opened) != 0 {
		t.Fatal("a refused open must not create a channel")
	}
}
