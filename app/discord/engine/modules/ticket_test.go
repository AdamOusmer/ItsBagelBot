// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/require"
)

type openRequestShape struct {
	Name       string
	ParentID   string
	Buttons    []string
	Overwrites []string
}

func TestTicketOpenCreatesRecordsAndAnswers(t *testing.T) {
	d := newDesk(t, baseConfig())

	reply := d.open(ada)

	require.Equal(t, "Ticket opened: <#c-new>", reply)
	req := d.rpc.opened[0]
	require.Equal(t, openRequestShape{
		Name: "ticket-ada", ParentID: "cat",
		Buttons:    []string{discordapi.CustomTicketClaim, discordapi.CustomTicketClose},
		Overwrites: []string{"g1", "u1", "helper"},
	}, openRequestShape{
		Name: req.Name, ParentID: req.ParentID,
		Buttons:    []string{req.Buttons[0].CustomID, req.Buttons[1].CustomID},
		Overwrites: []string{req.Overwrites[0].ID, req.Overwrites[1].ID, req.Overwrites[2].ID},
	})
	require.Equal(t, rpcTally{Opened: 1, Renamed: []string{"ticket-ada-1"}}, d.rpc.tally)
	stored, ok := d.ticket()
	require.True(t, ok)
	require.Equal(t, discordstore.Ticket{
		ID: 1, ChannelID: "c-new", GuildID: "g1", OpenerID: "u1", Status: discordstore.TicketStatusOpen, PanelMessageID: "m-new",
	}, stored)
}

func TestTicketOpenRefusals(t *testing.T) {
	cases := []struct {
		name      string
		cfg       func(*ddiscord.Config)
		held      []string
		faults    storeFaults
		openReply discordoutgress.TicketOpenReply
		openErr   error
		wantReply string
		wantTally rpcTally
	}{
		{name: "refuses at the limit without touching Discord", held: []string{"old1"},
			wantReply: "You already have 1 open ticket."},
		{name: "says so when tickets are off", cfg: func(c *ddiscord.Config) { c.TicketsEnabled = "off" },
			wantReply: "Tickets are off."},
		{name: "TestTicketOpenRefusesWithoutADurableStore", faults: storeFaults{nonDurable: true},
			wantReply: "The ticket desk is unavailable right now. Try again shortly."},
		{name: "rolls the channel back when the card fails", openReply: discordoutgress.TicketOpenReply{ChannelID: "c-new", Error: "missing permissions"},
			wantReply: "Could not open a ticket right now.", wantTally: rpcTally{Opened: 1, Deleted: []string{"c-new"}}},
		{name: "rolls the channel back when the open call fails", openErr: context.DeadlineExceeded,
			wantReply: "Could not open a ticket right now.", wantTally: rpcTally{Opened: 1, Deleted: []string{"c-new"}}},
		{name: "rolls the channel back when the row loses the race", cfg: func(c *ddiscord.Config) { c.TicketOpenLimit = "2" },
			held: []string{"old1", "old2"}, faults: storeFaults{staleCount: true},
			wantReply: "You already have 2 open tickets.", wantTally: rpcTally{Opened: 1, Deleted: []string{"c-new"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			if tc.cfg != nil {
				tc.cfg(&cfg)
			}
			d := newDesk(t, cfg)
			d.useStore(faultyStore{Store: d.mem, faults: tc.faults})
			for _, channel := range tc.held {
				_, err := d.mem.TrackTicket(context.Background(), discordstore.TicketOpen{GuildID: "g1", ChannelID: channel, OpenerID: "u1"})
				require.NoError(t, err)
			}
			if tc.openReply.ChannelID != "" {
				d.rpc.openReply = tc.openReply
			}
			d.rpc.openErr = tc.openErr

			reply := d.open(ada)

			require.Equal(t, tc.wantReply, reply)
			require.Equal(t, tc.wantTally, d.rpc.tally)
			_, row := d.ticket()
			require.False(t, row, "a refused open must not leave a row")
		})
	}
}

func TestTicketChannelNumberComesFromTheRowNotTheCount(t *testing.T) {
	d := newDesk(t, baseConfig())
	ctx := context.Background()

	d.open(ada)
	require.NoError(t, d.mem.CloseTicket(ctx, discordstore.TicketClose{GuildID: "g1", ChannelID: "c-new"}))
	d.rpc.openReply = discordoutgress.TicketOpenReply{ChannelID: "c-2", MessageID: "m-2"}
	d.open(ada)

	require.Equal(t, 1, d.mem.OpenTicketCount(ctx, discordstore.Member{GuildID: "g1", UserID: "u1"}),
		"held+1 would name this ticket -1")
	require.Equal(t, []string{"ticket-ada-1", "ticket-ada-2"}, d.rpc.tally.Renamed)
}

func TestTicketClaim(t *testing.T) {
	cases := []struct {
		name       string
		by         actor
		noTicket   bool
		wantReply  string
		wantClaims int
	}{
		{name: "refuses non staff", by: actor{id: "u2", perms: "0", roles: []string{"stranger"}},
			wantReply: "Only ticket staff can claim this."},
		{name: "answers on a channel that is not a ticket", by: helper, noTicket: true,
			wantReply: "This is not a ticket."},
		{name: "staff claim edits the card", by: helper,
			wantReply: "Claimed.", wantClaims: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(t, baseConfig())
			if !tc.noTicket {
				d.open(ada)
			}

			reply := d.button(discordapi.CustomTicketClaim, tc.by.inTicket())

			require.Equal(t, tc.wantReply, reply)
			require.Equal(t, tc.wantClaims, d.rpc.tally.Claimed)
		})
	}
}

func TestTicketClaimRecordsAndEditsTheCardOnce(t *testing.T) {
	d := newDesk(t, baseConfig())
	d.open(ada)

	d.button(discordapi.CustomTicketClaim, helper.inTicket())
	second := d.button(discordapi.CustomTicketClaim, actor{id: "mod2", perms: "0", roles: []string{"helper"}}.inTicket())

	require.Equal(t, "This ticket is already claimed by <@mod1>.", second)
	require.Len(t, d.rpc.claimed, 1, "the first claim only")
	require.Equal(t, "m-new", d.rpc.claimed[0].MessageID)
	require.Contains(t, d.rpc.claimed[0].Embed.Footer.Text, "Claimed by")
	stored, _ := d.ticket()
	require.Equal(t, "mod1", stored.ClaimedBy)
}

func TestTicketClose(t *testing.T) {
	cases := []struct {
		name       string
		status     string
		by         actor
		reply      discordoutgress.TicketCloseReply
		wantReply  string
		wantClosed int
		wantLive   bool
	}{
		{name: "the opener closes their ticket", by: ada, wantReply: "Ticket closed.", wantClosed: 1},
		{name: "a desk helper closes it", by: helper, wantReply: "Ticket closed.", wantClosed: 1},
		{name: "the mods role closes it", by: actor{id: "mod2", perms: "0", roles: []string{"m"}}, wantReply: "Ticket closed.", wantClosed: 1},
		{name: "a moderation permission bit closes it", by: actor{id: "adm", perms: "8"}, wantReply: "Ticket closed.", wantClosed: 1},
		{name: "a stranger cannot close it", by: stranger,
			wantReply: "Only the opener or ticket staff can close this.", wantLive: true},
		{name: "the row survives when outgress fails", by: ada, reply: discordoutgress.TicketCloseReply{Error: "missing permissions"},
			wantReply: "Could not close this ticket right now.", wantClosed: 1, wantLive: true},
		{name: "TestTicketCloseRefusesAnAlreadyClosedTicket closed", status: discordstore.TicketStatusClosed, by: helper,
			wantReply: "This ticket is already closed.", wantLive: true},
		{name: "TestTicketCloseRefusesAnAlreadyClosedTicket archived", status: discordstore.TicketStatusArchived, by: helper,
			wantReply: "This ticket is already closed.", wantLive: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(t, baseConfig())
			if tc.status == "" {
				d.open(ada)
			} else {
				d.mem.SeedTicket(discordstore.Ticket{ID: 1, ChannelID: "c-new", GuildID: "g1", OpenerID: "u1", Status: tc.status})
			}
			d.rpc.closeReply = tc.reply

			reply := d.button(discordapi.CustomTicketClose, tc.by.inTicket())

			require.Equal(t, tc.wantReply, reply)
			require.Equal(t, tc.wantClosed, d.rpc.tally.Closed)
			_, live := d.ticket()
			require.Equal(t, tc.wantLive, live)
		})
	}
}

func TestTicketCloseCarriesTheGuildsTranscriptAndArchiveSettings(t *testing.T) {
	type closeRequestShape struct {
		Transcript  bool
		Archive     string
		LogChannel  string
		ChannelName string
	}
	cases := []struct {
		name       string
		transcript string
		archive    string
		reply      discordoutgress.TicketCloseReply
		want       closeRequestShape
		wantBody   string
	}{
		{name: "transcript on, archived", archive: "arch1",
			reply: discordoutgress.TicketCloseReply{ArchivedChannelID: "arch1", TranscriptBody: "rendered", MessageCount: 7},
			want:  closeRequestShape{Transcript: true, Archive: "arch1", LogChannel: "log1", ChannelName: "ticket-ada-1"}, wantBody: "rendered"},
		{name: "transcript off, deleted", transcript: "off",
			want: closeRequestShape{LogChannel: "log1", ChannelName: "ticket-ada-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := baseConfig()
			cfg.TicketTranscriptEnabled = tc.transcript
			cfg.TicketArchiveCategoryID = tc.archive
			d := newDesk(t, cfg)
			d.rpc.closeReply = tc.reply
			d.open(ada)
			ticket, _ := d.ticket()

			d.button(discordapi.CustomTicketClose, ada.inTicket())

			req := d.rpc.closed[0]
			require.Equal(t, tc.want, closeRequestShape{
				Transcript: req.Transcript, Archive: req.ArchiveCategoryID, LogChannel: req.LogChannelID, ChannelName: req.ChannelName,
			})
			stored, ok := d.mem.Transcript(ticket.ID)
			require.Equal(t, tc.wantBody != "", ok)
			require.Equal(t, tc.wantBody, stored.Body)
		})
	}
}

func TestTicketPendingCloseIsRetriedOnTheNextInteraction(t *testing.T) {
	d := newDesk(t, baseConfig())
	ctx := context.Background()
	d.mem.SeedTicket(discordstore.Ticket{ID: 1, ChannelID: "c-new", GuildID: "g1", OpenerID: "u1", Status: discordstore.TicketStatusOpen})
	done := discordstore.TicketClose{GuildID: "g1", ChannelID: "c-new", ClosedBy: "u1", ArchivedChannelID: "c-new"}
	require.NoError(t, d.mem.MarkPendingClose(ctx, done))

	d.button(discordapi.CustomTicketClose, actor{id: "u1", perms: "0", roles: []string{"helper"}}.inTicket())

	_, pending := d.mem.PendingClose(ctx, discordstore.Channel{ID: "c-new"})
	_, live := d.ticket()
	require.False(t, pending, "the marker must be cleared once the row takes the close")
	require.False(t, live, "the retry must record the close")
	require.Zero(t, d.rpc.tally.Closed, "the retry is a store write, not a second Discord close")
}

func TestTicketAdd(t *testing.T) {
	cases := []struct {
		name      string
		by        actor
		wantReply string
		wantAdded []string
	}{
		{name: "the opener grants access", by: ada, wantReply: "Added <@u7> to this ticket.", wantAdded: []string{"u7"}},
		{name: "a stranger is refused", by: stranger, wantReply: "Only the opener or ticket staff can add someone."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(t, baseConfig())
			d.open(ada)

			reply := d.slash(subcommand("add", userOption("u7")), tc.by.inTicket())

			require.Equal(t, tc.wantReply, reply)
			require.Equal(t, tc.wantAdded, d.rpc.tally.Added)
		})
	}
}

func TestTicketActionsRefuseAnotherGuildsTicket(t *testing.T) {
	for _, action := range []string{"claim", "close", "add"} {
		t.Run(action, func(t *testing.T) {
			d := newDesk(t, baseConfig())
			d.mem.SeedTicket(discordstore.Ticket{
				ID: 1, ChannelID: "c-new", GuildID: "g-other", OpenerID: "u1", Status: discordstore.TicketStatusOpen,
			})

			reply := d.slash(subcommand(action, userOption("u7")), actor{id: "u1", perms: "0", roles: []string{"helper"}}.inTicket())

			require.Equal(t, "This is not a ticket.", reply)
			require.Equal(t, rpcTally{}, d.rpc.tally, "a cross-guild action must not reach outgress")
		})
	}
}

func TestTicketPanel(t *testing.T) {
	cases := []struct {
		name         string
		by           actor
		panelReply   discordoutgress.TicketPanelReply
		prior        string
		wantReply    string
		wantChannels []string
		wantPointer  string
	}{
		{name: "TestTicketPanelRefusesNonStaff", by: actor{id: "u2", perms: "0", roles: []string{"stranger"}},
			wantReply: "Only ticket staff can post the panel.", wantPointer: "none"},
		{name: "TestTicketPanelRemembersTheRealMessageID", by: helper,
			wantReply: "Ticket panel posted.", wantChannels: []string{"support"}, wantPointer: "m-panel"},
		{name: "TestTicketPanelAnswersWhenThePostFails", by: helper,
			panelReply: discordoutgress.TicketPanelReply{Error: "forbidden", Code: "forbidden"},
			wantReply:  "Could not post the ticket panel right now.", wantChannels: []string{"support"}, wantPointer: "none"},
		{name: "TestTicketPanelDoesNotEraseAPriorPointer", by: helper, prior: "m-old",
			panelReply: discordoutgress.TicketPanelReply{Error: "rate limited"},
			wantReply:  "Could not post the ticket panel right now.", wantChannels: []string{"support"}, wantPointer: "m-old"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDesk(t, baseConfig())
			ctx := context.Background()
			if tc.prior != "" {
				require.NoError(t, d.mem.RememberDesk(ctx, discordstore.DeskPanel{GuildID: "g1", ChannelID: "support", MessageID: tc.prior}))
			}
			if tc.panelReply != (discordoutgress.TicketPanelReply{}) {
				d.rpc.panelReply = tc.panelReply
			}

			reply := d.slash(subcommand("panel"), tc.by.inTicket())

			require.Equal(t, tc.wantReply, reply)
			require.Equal(t, tc.wantChannels, d.rpc.tally.PanelChannels)
			pointer, ok := d.mem.Desk(ctx, discordstore.Guild{ID: "g1"})
			require.Equal(t, tc.wantPointer, deskPointer(pointer, ok))
		})
	}
}

func deskPointer(p discordstore.DeskPanel, ok bool) string {
	if !ok {
		return "none"
	}
	return p.MessageID
}

func TestEnsureDeskClaimsOnceAndUsesTheStreamersCopy(t *testing.T) {
	cfg := baseConfig()
	cfg.TicketPanelTitle = "Need a hand?"
	cfg.TicketPanelButton = "Contact staff"
	store := discordstore.NewMem()
	var emitted []ddiscord.Command
	emit := func(c ddiscord.Command) { emitted = append(emitted, c) }

	modules.EnsureDesk(context.Background(), store, cfg, emit)
	modules.EnsureDesk(context.Background(), store, cfg, emit)

	require.Len(t, emitted, 1, "exactly one panel per guild")
	var panel ddiscord.EmbedPayload
	require.NoError(t, codec.Unmarshal(emitted[0].Payload, &panel))
	require.Equal(t, "Need a hand?", panel.Embed.Title)
	require.Equal(t, []ddiscord.ButtonSpec{{Style: discordapi.ButtonPrimary, Label: "Contact staff", CustomID: discordapi.CustomTicketOpen}}, panel.Buttons)
}

func TestEnsureDeskDoesNothingWhenUnconfigured(t *testing.T) {
	cases := []struct {
		name  string
		store discordstore.Store
		cfg   ddiscord.Config
	}{
		{"tickets off", discordstore.NewMem(), ddiscord.Config{GuildID: "g1", TicketChannelID: "support", TicketsEnabled: "off"}},
		{"no desk channel", discordstore.NewMem(), ddiscord.Config{GuildID: "g1"}},
		{"no store", nil, ddiscord.Config{GuildID: "g1", TicketChannelID: "support"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var emitted []ddiscord.Command

			modules.EnsureDesk(context.Background(), tc.store, tc.cfg, func(c ddiscord.Command) { emitted = append(emitted, c) })

			require.Empty(t, emitted)
		})
	}
}
