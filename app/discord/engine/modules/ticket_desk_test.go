// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"
)

func TestEnsureDeskClaimsOnceAndUsesTheStreamersCopy(t *testing.T) {
	store := discordstore.NewMem()
	cfg := baseConfig()
	cfg.TicketPanelTitle = "Need a hand?"
	cfg.TicketPanelButton = "Contact staff"

	var emitted []ddiscord.Command
	emit := func(c ddiscord.Command) { emitted = append(emitted, c) }
	EnsureDesk(context.Background(), store, cfg, emit)
	EnsureDesk(context.Background(), store, cfg, emit)

	if len(emitted) != 1 {
		t.Fatalf("panels = %d, want exactly one per guild", len(emitted))
	}
	var panel ddiscord.EmbedPayload
	if err := codec.Unmarshal(emitted[0].Payload, &panel); err != nil {
		t.Fatalf("decode panel: %v", err)
	}
	if panel.Embed.Title != "Need a hand?" {
		t.Fatalf("panel embed = %+v", panel.Embed)
	}
	if len(panel.Buttons) != 1 || panel.Buttons[0].Label != "Contact staff" {
		t.Fatalf("panel buttons = %+v", panel.Buttons)
	}
	if panel.Buttons[0].CustomID != discordapi.CustomTicketOpen {
		t.Fatalf("panel button id = %q", panel.Buttons[0].CustomID)
	}
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
			count := 0
			EnsureDesk(context.Background(), tc.store, tc.cfg, func(ddiscord.Command) { count++ })
			if count != 0 {
				t.Fatalf("emitted %d commands", count)
			}
		})
	}
}

// Posting the desk panel is a staff action. Ungated, any member could paste a
// second real "Open a ticket" button into any channel they can run a slash
// command in.
func TestTicketPanelRefusesNonStaff(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.panelPress(t, inTicket("u2", []string{"stranger"}))

	if got := followupText(t, cmds); got != "Only ticket staff can post the panel." {
		t.Fatalf("followup = %q", got)
	}
	if len(f.tickets.panels) != 0 {
		t.Fatalf("a refused panel must not post: %+v", f.tickets.panels)
	}
}

func TestTicketPanelRemembersTheRealMessageID(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.panelPress(t, inTicket("u1", []string{"helper"}))

	if got := followupText(t, cmds); got != "Ticket panel posted." {
		t.Fatalf("followup = %q", got)
	}
	if len(f.tickets.panels) != 1 || f.tickets.panels[0].ChannelID != "support" {
		t.Fatalf("panel requests = %+v", f.tickets.panels)
	}
	desk, ok := f.store.Desk(context.Background(), discordstore.Guild{ID: "g1"})
	if !ok || desk.MessageID != "m-panel" {
		t.Fatalf("desk pointer = %+v, %v, want the posted message id", desk, ok)
	}
}

// A panel that never posted must not leave a pointer behind, and must answer
// the interaction rather than leaving it spinning on "thinking".
func TestTicketPanelAnswersWhenThePostFails(t *testing.T) {
	f := newDesk(t, baseConfig())
	f.tickets.panelReply = discordoutgress.TicketPanelReply{Error: "forbidden", Code: "forbidden"}

	cmds := f.panelPress(t, inTicket("u1", []string{"helper"}))

	if got := followupText(t, cmds); got != "Could not post the ticket panel right now." {
		t.Fatalf("followup = %q", got)
	}
	if _, ok := f.store.Desk(context.Background(), discordstore.Guild{ID: "g1"}); ok {
		t.Fatal("a failed post must not leave a desk pointer")
	}
}

// A prior panel's id survives a claim-shaped remember: the repost path needs
// something to delete, or it stacks a second live panel under the first.
func TestTicketPanelDoesNotEraseAPriorPointer(t *testing.T) {
	f := newDesk(t, baseConfig())
	ctx := context.Background()
	if err := f.store.RememberDesk(ctx, discordstore.DeskPanel{GuildID: "g1", ChannelID: "support", MessageID: "m-old"}); err != nil {
		t.Fatalf("seed the pointer: %v", err)
	}
	f.tickets.panelReply = discordoutgress.TicketPanelReply{Error: "rate limited"}

	f.panelPress(t, inTicket("u1", []string{"helper"}))

	desk, _ := f.store.Desk(ctx, discordstore.Guild{ID: "g1"})
	if desk.MessageID != "m-old" {
		t.Fatalf("desk pointer = %+v, want the prior panel kept", desk)
	}
}
