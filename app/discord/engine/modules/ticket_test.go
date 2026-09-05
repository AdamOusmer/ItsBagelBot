// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"errors"
	"strings"
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

// stubTickets is the outgress side of the desk, scripted. Every orchestration
// records its request so a test can assert what the engine DECIDED, which is
// the whole of the engine's job here -- the REST shapes are pinned on the
// outgress side (app/discord/outgress/internal/rpc/ticket_rpc_test.go).
type stubTickets struct {
	openReply  discordoutgress.TicketOpenReply
	openErr    error
	closeReply discordoutgress.TicketCloseReply

	opened  []discordoutgress.TicketOpenRequest
	claimed []discordoutgress.TicketClaimRequest
	closed  []discordoutgress.TicketCloseRequest
	added   []discordoutgress.TicketMemberAddRequest
	deleted []string
}

func newStubTickets() *stubTickets {
	return &stubTickets{openReply: discordoutgress.TicketOpenReply{ChannelID: "c-new", MessageID: "m-new"}}
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

// deskFixture is one module under test with its store and stub.
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

// press replays one button press (or slash sub-command) through the module.
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

func opener(userID, username string) decode.InteractionEvent {
	var in decode.InteractionEvent
	in.GuildID = "g1"
	in.ChannelID = "support"
	in.Token = "tok"
	in.Member.User = decode.UserRef{ID: userID, Username: username}
	return in
}

// followupText is the ephemeral answer the desk gave, which every path owes
// the presser.
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

func TestTicketOpenCreatesRecordsAndAnswers(t *testing.T) {
	f := newDesk(t, baseConfig())

	cmds := f.press(t, f.mod.open, opener("u1", "Ada"))

	if len(f.tickets.opened) != 1 {
		t.Fatalf("open requests = %+v", f.tickets.opened)
	}
	req := f.tickets.opened[0]
	if req.Name != "ticket-ada-1" {
		t.Fatalf("channel name = %q", req.Name)
	}
	if req.ParentID != "cat" {
		t.Fatalf("parent = %q", req.ParentID)
	}
	if len(req.Buttons) != 2 || req.Buttons[0].CustomID != discordapi.CustomTicketClaim ||
		req.Buttons[1].CustomID != discordapi.CustomTicketClose {
		t.Fatalf("buttons = %+v", req.Buttons)
	}
	stored, ok := f.store.Ticket(context.Background(), discordstore.Channel{ID: "c-new"})
	if !ok || stored.OpenerID != "u1" || stored.PanelMessageID != "m-new" {
		t.Fatalf("stored ticket = %+v, %v", stored, ok)
	}
	if got := followupText(t, cmds); !strings.Contains(got, "<#c-new>") {
		t.Fatalf("followup = %q, want the channel mention", got)
	}
}

func TestTicketOpenNumbersTheSecondChannel(t *testing.T) {
	cfg := baseConfig()
	cfg.TicketOpenLimit = "3"
	f := newDesk(t, cfg)

	f.press(t, f.mod.open, opener("u1", "Ada"))
	f.tickets.openReply = discordoutgress.TicketOpenReply{ChannelID: "c-2", MessageID: "m-2"}
	f.press(t, f.mod.open, opener("u1", "Ada"))

	if got := f.tickets.opened[1].Name; got != "ticket-ada-2" {
		t.Fatalf("second channel = %q, want the opener's nth ticket", got)
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

	cmds := f.createTicketPress(t, opener("u1", "Ada"), 1)

	if len(f.tickets.deleted) != 1 || f.tickets.deleted[0] != "c-new" {
		t.Fatalf("rollback deletes = %v", f.tickets.deleted)
	}
	if got := followupText(t, cmds); !strings.Contains(got, "already have 2") {
		t.Fatalf("followup = %q", got)
	}
}

// createTicket takes the held count as a parameter; press only passes the
// handler shape, so this adapts it.
func (f *deskFixture) createTicketPress(t *testing.T, in decode.InteractionEvent, held int) []ddiscord.Command {
	t.Helper()
	return f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		return f.mod.createTicket(ctx, c, in, held, emit)
	}, in)
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
	if _, ok := f.store.Ticket(context.Background(), discordstore.Channel{ID: "c-new"}); ok {
		t.Fatal("a failed open must not leave a row")
	}
	if got := followupText(t, cmds); got != "Could not open a ticket right now." {
		t.Fatalf("followup = %q", got)
	}
}

// openOneTicket puts a live ticket in the store and returns it.
func (f *deskFixture) openOneTicket(t *testing.T) discordstore.Ticket {
	t.Helper()
	f.press(t, f.mod.open, opener("u1", "Ada"))
	got, ok := f.store.Ticket(context.Background(), discordstore.Channel{ID: "c-new"})
	if !ok {
		t.Fatal("expected a stored ticket")
	}
	return got
}

func inTicket(userID string, roles []string) decode.InteractionEvent {
	in := opener(userID, "someone")
	in.ChannelID = "c-new"
	in.Channel.Name = "ticket-ada-1"
	in.Member.Roles = roles
	in.Member.Permissions = "0"
	return in
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
	stored, _ := f.store.Ticket(context.Background(), discordstore.Channel{ID: "c-new"})
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
			if _, ok := f.store.Ticket(context.Background(), discordstore.Channel{ID: "c-new"}); ok {
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

func (f *deskFixture) closeReplyFor(transcript bool, archived string) {
	f.tickets.closeReply = discordoutgress.TicketCloseReply{ArchivedChannelID: archived}
	if transcript {
		f.tickets.closeReply.TranscriptBody = "rendered"
		f.tickets.closeReply.MessageCount = 7
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
	if _, ok := f.store.Ticket(context.Background(), discordstore.Channel{ID: "c-new"}); !ok {
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

	cmds := f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		return f.mod.add(ctx, c, in, sub, emit)
	}, in)

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

	cmds := f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		return f.mod.add(ctx, c, in, sub, emit)
	}, in)

	if len(f.tickets.added) != 0 {
		t.Fatalf("add requests = %+v", f.tickets.added)
	}
	if got := followupText(t, cmds); got != "Only the opener or ticket staff can add someone." {
		t.Fatalf("followup = %q", got)
	}
}

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
