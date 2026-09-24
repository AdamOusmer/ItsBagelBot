// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/module"
	"ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
	discordoutgress "ItsBagelBot/internal/domain/rpc/discordoutgress"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

type stubTickets struct {
	openReply  discordoutgress.TicketOpenReply
	openErr    error
	closeReply discordoutgress.TicketCloseReply

	panelReply discordoutgress.TicketPanelReply
	panelErr   error

	opened  []discordoutgress.TicketOpenRequest
	panels  []discordoutgress.TicketPanelRequest
	renamed []discordoutgress.ChannelModifyRequest
	claimed []discordoutgress.TicketClaimRequest
	closed  []discordoutgress.TicketCloseRequest
	added   []discordoutgress.TicketMemberAddRequest
	deleted []string
}

func newStubTickets() *stubTickets {
	return &stubTickets{
		openReply:  discordoutgress.TicketOpenReply{ChannelID: "c-new", MessageID: "m-new"},
		panelReply: discordoutgress.TicketPanelReply{MessageID: "m-panel"},
	}
}

func (s *stubTickets) TicketPanel(_ context.Context, req discordoutgress.TicketPanelRequest) (discordoutgress.TicketPanelReply, error) {
	s.panels = append(s.panels, req)
	return s.panelReply, s.panelErr
}

func (s *stubTickets) ModifyChannel(_ context.Context, req discordoutgress.ChannelModifyRequest) (discordoutgress.ChannelModifyReply, error) {
	s.renamed = append(s.renamed, req)
	return discordoutgress.ChannelModifyReply{}, nil
}

func (s *stubTickets) TicketOpen(_ context.Context, req discordoutgress.TicketOpenRequest) (discordoutgress.TicketOpenReply, error) {
	s.opened = append(s.opened, req)
	return s.openReply, s.openErr
}

func (s *stubTickets) TicketClaim(_ context.Context, req discordoutgress.TicketClaimRequest) (discordoutgress.TicketClaimReply, error) {
	s.claimed = append(s.claimed, req)
	return discordoutgress.TicketClaimReply{}, nil
}

func (s *stubTickets) TicketClose(_ context.Context, req discordoutgress.TicketCloseRequest) (discordoutgress.TicketCloseReply, error) {
	s.closed = append(s.closed, req)
	return s.closeReply, nil
}

func (s *stubTickets) TicketAddMember(_ context.Context, req discordoutgress.TicketMemberAddRequest) (discordoutgress.TicketMemberAddReply, error) {
	s.added = append(s.added, req)
	return discordoutgress.TicketMemberAddReply{}, nil
}

func (s *stubTickets) DeleteChannel(_ context.Context, req discordoutgress.ChannelDeleteRequest) (discordoutgress.ChannelDeleteReply, error) {
	s.deleted = append(s.deleted, req.ChannelID)
	return discordoutgress.ChannelDeleteReply{}, nil
}

type deskFixture struct {
	mod     ticketModule
	store   *discordstore.Mem
	tickets *stubTickets
	cfg     ddiscord.Config
}

func newDesk(t *testing.T, cfg ddiscord.Config) *deskFixture {
	t.Helper()
	store := discordstore.NewMem()
	tickets := newStubTickets()
	return &deskFixture{
		mod:   ticketModule{store: store, tickets: tickets, log: zap.NewNop()},
		store: store, tickets: tickets, cfg: cfg,
	}
}

func (f *deskFixture) press(t *testing.T, handler func(context.Context, *module.Context, module.Emit) error, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	raw, err := codec.Marshal(in)
	if err != nil {
		t.Fatalf("marshal interaction: %v", err)
	}
	var emitted []ddiscord.Command
	c := &module.Context{Event: ddiscord.Event{Raw: raw}, Config: f.cfg, Log: zap.NewNop()}
	if err := handler(context.Background(), c, func(cmd ddiscord.Command) { emitted = append(emitted, cmd) }); err != nil {
		t.Fatalf("handler: %v", err)
	}
	return emitted
}

func (f *deskFixture) deskPress(t *testing.T, in decode.InteractionEvent, run func(context.Context, deskCall) error) []ddiscord.Command {
	t.Helper()
	return f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		return run(ctx, deskCall{mod: c, in: in, emit: emit})
	}, in)
}

func opener(userID, username string) decode.InteractionEvent {
	var in decode.InteractionEvent
	in.GuildID = "g1"
	in.ChannelID = "support"
	in.Token = "tok"
	in.Member.User = decode.UserRef{ID: userID, Username: username}
	return in
}
func inTicket(userID string, roles []string) decode.InteractionEvent {
	in := opener(userID, "someone")
	in.ChannelID = "c-new"
	in.Channel.Name = "ticket-ada-1"
	in.Member.Roles = roles
	in.Member.Permissions = "0"
	return in
}

func followupText(t *testing.T, cmds []ddiscord.Command) string {
	t.Helper()
	for _, c := range cmds {
		if c.Type != ddiscord.TypeInteractionFollowup {
			continue
		}
		var payload ddiscord.FollowupPayload
		if err := codec.Unmarshal(c.Payload, &payload); err != nil {
			t.Fatalf("decode followup: %v", err)
		}
		if !payload.Ephemeral {
			t.Fatalf("desk answers must be ephemeral: %+v", payload)
		}
		return payload.Content
	}
	t.Fatalf("no ephemeral followup in %+v", cmds)
	return ""
}
func baseConfig() ddiscord.Config {
	return ddiscord.Config{
		GuildID: "g1", TicketChannelID: "support", TicketCategoryID: "cat",
		ModsRoleID: "m", TicketStaffRoles: "helper", TicketLogChannelID: "log1",
	}
}

func (f *deskFixture) openOneTicket(t *testing.T) discordstore.Ticket {
	t.Helper()
	f.press(t, f.mod.open, opener("u1", "Ada"))
	got, ok := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"})
	if !ok {
		t.Fatal("expected a stored ticket")
	}
	return got
}

func (f *deskFixture) createTicketPress(t *testing.T, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	return f.deskPress(t, in, f.mod.createTicket)
}

func (f *deskFixture) closeReplyFor(transcript bool, archived string) {
	f.tickets.closeReply = discordoutgress.TicketCloseReply{ArchivedChannelID: archived}
	if transcript {
		f.tickets.closeReply.TranscriptBody = "rendered"
		f.tickets.closeReply.MessageCount = 7
	}
}

func (f *deskFixture) panelPress(t *testing.T, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	return f.deskPress(t, in, f.mod.panel)
}

func wantOpenRequest(t *testing.T, req discordoutgress.TicketOpenRequest) {
	t.Helper()
	if req.Name != "ticket-ada" {
		t.Fatalf("channel name = %q, want the unnumbered name", req.Name)
	}
	if req.ParentID != "cat" {
		t.Fatalf("parent = %q, want the configured category", req.ParentID)
	}
	if len(req.Buttons) != 2 {
		t.Fatalf("buttons = %+v, want claim and close", req.Buttons)
	}
	if req.Buttons[0].CustomID != discordapi.CustomTicketClaim {
		t.Fatalf("first button = %q, want claim", req.Buttons[0].CustomID)
	}
	if req.Buttons[1].CustomID != discordapi.CustomTicketClose {
		t.Fatalf("second button = %q, want close", req.Buttons[1].CustomID)
	}
}

func wantRenamedTo(t *testing.T, renames []discordoutgress.ChannelModifyRequest, name string) {
	t.Helper()
	if len(renames) != 1 {
		t.Fatalf("renames = %+v, want exactly one", renames)
	}
	if renames[0].Name != name {
		t.Fatalf("rename = %q, want %q", renames[0].Name, name)
	}
}

func (f *deskFixture) wantStoredTicket(t *testing.T, openerID, panelMessageID string) {
	t.Helper()
	stored, ok := f.store.Ticket(context.Background(), discordstore.Guild{ID: "g1"}, discordstore.Channel{ID: "c-new"})
	if !ok {
		t.Fatal("no ticket row for the created channel")
	}
	if stored.OpenerID != openerID {
		t.Fatalf("stored opener = %q, want %q", stored.OpenerID, openerID)
	}
	if stored.PanelMessageID != panelMessageID {
		t.Fatalf("stored panel message id = %q, want %q", stored.PanelMessageID, panelMessageID)
	}
}

type fallbackStore struct{ discordstore.Store }

func (fallbackStore) TicketsDurable(context.Context) bool { return false }
