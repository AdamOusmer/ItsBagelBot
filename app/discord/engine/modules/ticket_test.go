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
	// The create cannot know the row id yet (the row is keyed on the channel
	// this call is creating), so it goes out unnumbered and is renamed once
	// the row exists.
	if req.Name != "ticket-ada" {
		t.Fatalf("channel name = %q", req.Name)
	}
	if len(f.tickets.renamed) != 1 || f.tickets.renamed[0].Name != "ticket-ada-1" {
		t.Fatalf("rename = %+v, want the row id", f.tickets.renamed)
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

// createTicketPress drives the create half directly, past the pre-checks open
// performs, so a test can reach the record-and-roll-back path.
func (f *deskFixture) createTicketPress(t *testing.T, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	return f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		return f.mod.createTicket(ctx, c, in, emit)
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

// panelPress drives /ticket panel, which takes the decoded interaction the
// slash router already parsed rather than re-reading it from the event.
func (f *deskFixture) panelPress(t *testing.T, in decode.InteractionEvent) []ddiscord.Command {
	t.Helper()
	return f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
		return f.mod.panel(ctx, c, in, emit)
	}, in)
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

// The ticket keyspace is keyed on the channel id alone, so a row written for
// one guild answers a lookup made from any other. Without the guild check a
// member of guild B could claim, close or add people to guild A's private
// support channel.
func TestTicketActionsRefuseAnotherGuildsTicket(t *testing.T) {
	const refusal = "This is not a ticket in this server."
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
			return f.press(t, func(ctx context.Context, c *module.Context, emit module.Emit) error {
				return f.mod.add(ctx, c, in, decode.InteractionOption{Name: "add"}, emit)
			}, in)
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
	if _, ok := f.store.Ticket(ctx, discordstore.Channel{ID: "c-new"}); ok {
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

// fallbackStore is the memory double answering TicketsDurable the way the
// pure-Valkey store does.
type fallbackStore struct{ discordstore.Store }

func (fallbackStore) TicketsDurable(context.Context) bool { return false }
