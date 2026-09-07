// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordstore

import (
	"context"
	"testing"
)

func TestMemBroadcaster(t *testing.T) {
	m := NewMem()
	g := Guild{ID: "g1"}
	m.PutGuild(g, Broadcaster{ID: "42"})
	got, ok := m.Broadcaster(context.Background(), g)
	if !ok {
		t.Fatal("missing guild")
	}
	if got.ID != "42" {
		t.Fatalf("guild = %s", got.ID)
	}
}

func TestMemBindAndUnbindGuild(t *testing.T) {
	m := NewMem()
	g := Guild{ID: "g1"}
	if err := m.BindGuild(context.Background(), Binding{Guild: g, Broadcaster: Broadcaster{ID: "42"}}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	got, ok := m.Broadcaster(context.Background(), g)
	if !ok || got.ID != "42" {
		t.Fatalf("broadcaster = %+v, %v", got, ok)
	}
	if err := m.UnbindGuild(context.Background(), Binding{Guild: g}); err != nil {
		t.Fatalf("unbind: %v", err)
	}
	if _, ok := m.Broadcaster(context.Background(), g); ok {
		t.Fatal("guild should be unbound")
	}
}

func TestMemXPCooldown(t *testing.T) {
	m := NewMem()
	mem := Member{GuildID: "g1", UserID: "u1"}
	xp, leveled, level := m.AddXP(context.Background(), mem)
	if xp != xpPerMessage {
		t.Fatalf("first xp = %d", xp)
	}
	if leveled {
		t.Fatal("first message must not level")
	}
	if level != 0 {
		t.Fatalf("level = %d", level)
	}
	xp2, _, _ := m.AddXP(context.Background(), mem)
	if xp2 != xp {
		t.Fatalf("cooldown should skip, got %d", xp2)
	}
}

func TestMemDaily(t *testing.T) {
	m := NewMem()
	mem := Member{GuildID: "g1", UserID: "u1"}
	ok, total := m.ClaimDaily(context.Background(), mem)
	if !ok {
		t.Fatal("first daily")
	}
	if total != dailyXP {
		t.Fatalf("daily total = %d", total)
	}
	ok, _ = m.ClaimDaily(context.Background(), mem)
	if ok {
		t.Fatal("second daily")
	}
}

func TestMemClones(t *testing.T) {
	m := NewMem()
	g := Guild{ID: "g1"}
	_ = m.TrackClone(context.Background(), Clone{ChannelID: "c1", GuildID: g.ID, OwnerID: "u1"})
	if m.CloneCount(context.Background(), g) != 1 {
		t.Fatal("clone count")
	}
	_ = m.ForgetClone(context.Background(), Clone{ChannelID: "c1", GuildID: g.ID})
	if m.CloneCount(context.Background(), g) != 0 {
		t.Fatal("clone forgotten")
	}
}

func TestMemDeskClaim(t *testing.T) {
	m := NewMem()
	g := Guild{ID: "g1"}
	if !m.ClaimDesk(context.Background(), g) {
		t.Fatal("first desk claim")
	}
	if m.ClaimDesk(context.Background(), g) {
		t.Fatal("second desk claim")
	}
	_ = m.RememberDesk(context.Background(), DeskPanel{GuildID: "g2"})
	if m.ClaimDesk(context.Background(), Guild{ID: "g2"}) {
		t.Fatal("remembered desk")
	}
}

func TestLevelOf(t *testing.T) {
	cases := []struct {
		xp   int
		want int
	}{
		{0, 0},
		{99, 0},
		{100, 1},
		{400, 2},
	}
	for _, tc := range cases {
		if got := levelOf(tc.xp); got != tc.want {
			t.Fatalf("levelOf(%d) = %d, want %d", tc.xp, got, tc.want)
		}
	}
}

func TestMemVoiceOccupancyJoinAndLeave(t *testing.T) {
	m := NewMem()
	ctx := context.Background()

	left, leftEmpty := m.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "hub"})
	if left != "" || leftEmpty {
		t.Fatalf("first join should report nothing left, got %q/%v", left, leftEmpty)
	}

	// A second user joins the same channel; the first leaving must not
	// report it empty while the second is still there.
	_, _ = m.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u2", ChannelID: "hub"})
	left, leftEmpty = m.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "clone-1"})
	if left != "hub" {
		t.Fatalf("left = %q, want hub", left)
	}
	if leftEmpty {
		t.Fatal("hub still has u2, must not report empty")
	}

	// The last occupant leaving voice entirely reports the channel empty.
	left, leftEmpty = m.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u2", ChannelID: ""})
	if left != "hub" || !leftEmpty {
		t.Fatalf("left = %q, leftEmpty = %v, want hub/true", left, leftEmpty)
	}
}

func TestMemVoiceOccupancySameChannelUpdateIsNotALeave(t *testing.T) {
	m := NewMem()
	ctx := context.Background()
	_, _ = m.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "hub"})

	// A mute/deafen toggle re-delivers the same channel id; must never
	// report the channel as vacated.
	left, leftEmpty := m.UpdateVoiceOccupancy(ctx, VoiceSeat{GuildID: "g1", UserID: "u1", ChannelID: "hub"})
	if leftEmpty {
		t.Fatalf("same-channel update must not report empty (left=%q)", left)
	}
}

// The pure-Valkey store cannot number, cap or transcribe a ticket, and says
// so. The desk refuses to open in that mode rather than handing out a channel
// it can never manage.
func TestTicketsDurableIsFalseOnTheValkeyFallback(t *testing.T) {
	if (valkeyStore{}).TicketsDurable(context.Background()) {
		t.Fatal("the pure-Valkey fallback must not claim durable tickets")
	}
	if !NewMem().TicketsDurable(context.Background()) {
		t.Fatal("the memory double stands in for the durable path")
	}
}

// A remember with no message id is a CLAIM, not an overwrite: letting it win
// would erase the id of a panel that is actually posted, and the repost path
// would then stack a second live panel under the first.
func TestRememberDeskNeverErasesAKnownPanel(t *testing.T) {
	ctx := context.Background()
	m := NewMem()

	if err := m.RememberDesk(ctx, DeskPanel{GuildID: "g1", ChannelID: "c1", MessageID: "m1"}); err != nil {
		t.Fatalf("remember: %v", err)
	}
	if err := m.RememberDesk(ctx, DeskPanel{GuildID: "g1", ChannelID: "c1"}); err != nil {
		t.Fatalf("re-remember: %v", err)
	}

	got, ok := m.Desk(ctx, Guild{ID: "g1"})
	if !ok || got.MessageID != "m1" {
		t.Fatalf("desk = %+v, %v, want the original message id kept", got, ok)
	}
}

func TestPendingCloseRoundTrips(t *testing.T) {
	ctx := context.Background()
	m := NewMem()
	done := TicketClose{GuildID: "g1", ChannelID: "c1", ClosedBy: "u9", ArchivedChannelID: "c1"}

	if _, ok := m.PendingClose(ctx, Channel{ID: "c1"}); ok {
		t.Fatal("nothing was marked yet")
	}
	if err := m.MarkPendingClose(ctx, done); err != nil {
		t.Fatalf("mark: %v", err)
	}
	got, ok := m.PendingClose(ctx, Channel{ID: "c1"})
	if !ok || got != done {
		t.Fatalf("pending = %+v, %v", got, ok)
	}
	if err := m.ClearPendingClose(ctx, Channel{ID: "c1"}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if _, ok := m.PendingClose(ctx, Channel{ID: "c1"}); ok {
		t.Fatal("the marker survived being cleared")
	}
}

// The summary memo is what keeps a retried close from stacking a second card
// and a second transcript upload in the log channel.
func TestClaimSummaryIsOncePerTicket(t *testing.T) {
	ctx := context.Background()
	m := NewMem()

	if !m.ClaimSummary(ctx, 7) {
		t.Fatal("the first close must claim")
	}
	if m.ClaimSummary(ctx, 7) {
		t.Fatal("the retry must not claim")
	}
	if !m.ClaimSummary(ctx, 8) {
		t.Fatal("a different ticket claims independently")
	}
	// The fallback has no row ids; a zero always posts rather than never.
	if !m.ClaimSummary(ctx, 0) || !m.ClaimSummary(ctx, 0) {
		t.Fatal("a ticket with no row id must always post")
	}
}

func TestTicketOverNamesTheTerminalStates(t *testing.T) {
	for _, status := range []string{TicketStatusClosed, TicketStatusArchived} {
		if !TicketOver(status) {
			t.Fatalf("%q is terminal", status)
		}
	}
	for _, status := range []string{TicketStatusOpen, TicketStatusClaimed, ""} {
		if TicketOver(status) {
			t.Fatalf("%q is not terminal", status)
		}
	}
}

// The five tests below came back with the ticket work: they were deleted
// alongside select_test.go when Select and the DISCORD_DATA_ENABLED switch
// went away, but only two of the tests in that file were about Select. These
// cover the memory double's ticket bookkeeping and the two pipe-joined Valkey
// values, neither of which anything else exercises.

// wantOpened holds what an ACCEPTED open must answer with: no error, no
// refusal, a row id (the desk names the channel after it) and the live count
// including this one.
func wantOpened(t *testing.T, got TicketOpenResult, err error, wantCount int) {
	t.Helper()
	if err != nil {
		t.Fatalf("TrackTicket: %v", err)
	}
	if got.AtLimit {
		t.Fatalf("open = %+v, want it accepted", got)
	}
	if got.TicketID == 0 {
		t.Fatal("an accepted open must carry a row id")
	}
	if got.OpenCount != wantCount {
		t.Fatalf("open count = %d, want %d", got.OpenCount, wantCount)
	}
}

// TestMemEnforcesTheOpenLimitAndNumbersTickets: the double enforces the limit
// the Valkey store deliberately does not, which is what the engine's refusal
// path is tested against.
func TestMemEnforcesTheOpenLimitAndNumbersTickets(t *testing.T) {
	m := NewMem()
	ctx := context.Background()
	opener := Member{GuildID: "g1", UserID: "u1"}

	first, err := m.TrackTicket(ctx, TicketOpen{GuildID: "g1", ChannelID: "c1", OpenerID: "u1", OpenLimit: 2, PanelMessageID: "m1"})
	wantOpened(t, first, err, 1)
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
	if _, ok := m.Ticket(ctx, Guild{ID: "g1"}, Channel{ID: "c3"}); ok {
		t.Fatal("a refused open must not leave a row")
	}
}

// wantClaimed holds what a claim must leave behind: the claimant, the status
// move, and the panel message id -- which the close path needs to edit the
// card back, and which a claim that rebuilt the record instead of updating it
// would silently drop.
func wantClaimed(t *testing.T, got Ticket) {
	t.Helper()
	if got.ClaimedBy != "mod1" {
		t.Fatalf("claimed by %q, want mod1", got.ClaimedBy)
	}
	if got.Status != TicketStatusClaimed {
		t.Fatalf("status = %q, want %q", got.Status, TicketStatusClaimed)
	}
	if got.PanelMessageID != "m1" {
		t.Fatalf("panel message id = %q, want m1", got.PanelMessageID)
	}
}

// wantStoredTranscript reads the transcript back out of the store.
func wantStoredTranscript(t *testing.T, m *Mem, ticketID int) {
	t.Helper()
	stored, ok := m.Transcript(ticketID)
	if !ok {
		t.Fatalf("ticket %d has no transcript", ticketID)
	}
	if stored.Body != "body" {
		t.Fatalf("body = %q, want %q", stored.Body, "body")
	}
	if stored.MessageCount != 2 {
		t.Fatalf("message count = %d, want 2", stored.MessageCount)
	}
}

func TestMemClaimAndCloseMoveTheTicketOn(t *testing.T) {
	m := NewMem()
	ctx := context.Background()
	guild := Guild{ID: "g1"}
	if _, err := m.TrackTicket(ctx, TicketOpen{GuildID: "g1", ChannelID: "c1", OpenerID: "u1", PanelMessageID: "m1"}); err != nil {
		t.Fatalf("TrackTicket: %v", err)
	}

	if err := m.ClaimTicket(ctx, TicketClaim{GuildID: "g1", ChannelID: "c1", StaffID: "mod1"}); err != nil {
		t.Fatalf("ClaimTicket: %v", err)
	}
	got, _ := m.Ticket(ctx, guild, Channel{ID: "c1"})
	wantClaimed(t, got)

	if err := m.PutTranscript(ctx, Transcript{TicketID: got.ID, Body: "body", MessageCount: 2}); err != nil {
		t.Fatalf("PutTranscript: %v", err)
	}
	wantStoredTranscript(t, m, got.ID)

	if err := m.CloseTicket(ctx, TicketClose{GuildID: "g1", ChannelID: "c1", ClosedBy: "mod1"}); err != nil {
		t.Fatalf("CloseTicket: %v", err)
	}
	if _, ok := m.Ticket(ctx, guild, Channel{ID: "c1"}); ok {
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
	wantDesk(t, got, ok, DeskPanel{GuildID: "g1", ChannelID: "c1", MessageID: "m1"})
	if m.ClaimDesk(ctx, Guild{ID: "g1"}) {
		t.Fatal("a remembered desk is already claimed")
	}
}

// wantDesk compares a parsed or stored desk panel field by field.
func wantDesk(t *testing.T, got DeskPanel, ok bool, want DeskPanel) {
	t.Helper()
	if !ok {
		t.Fatalf("the desk did not resolve: %+v", got)
	}
	if got.GuildID != want.GuildID {
		t.Fatalf("guild = %q, want %q", got.GuildID, want.GuildID)
	}
	if got.ChannelID != want.ChannelID {
		t.Fatalf("channel = %q, want %q", got.ChannelID, want.ChannelID)
	}
	if got.MessageID != want.MessageID {
		t.Fatalf("message = %q, want %q", got.MessageID, want.MessageID)
	}
}

func TestParseDeskValueReadsTheLegacyClaim(t *testing.T) {
	got, ok := parseDeskValue("g1", deskClaimed)
	wantDesk(t, got, ok, DeskPanel{GuildID: "g1"})
	got, ok = parseDeskValue("g1", "c1|m1")
	wantDesk(t, got, ok, DeskPanel{GuildID: "g1", ChannelID: "c1", MessageID: "m1"})
}

// wantParsedTicket compares a parsed ticket value against the record it must
// decode to, field by field.
func wantParsedTicket(t *testing.T, got Ticket, ok bool, want Ticket) {
	t.Helper()
	if !ok {
		t.Fatalf("the value did not parse: %+v", got)
	}
	if got.GuildID != want.GuildID {
		t.Fatalf("guild = %q, want %q", got.GuildID, want.GuildID)
	}
	if got.OpenerID != want.OpenerID {
		t.Fatalf("opener = %q, want %q", got.OpenerID, want.OpenerID)
	}
	if got.ClaimedBy != want.ClaimedBy {
		t.Fatalf("claimed by = %q, want %q", got.ClaimedBy, want.ClaimedBy)
	}
	if got.PanelMessageID != want.PanelMessageID {
		t.Fatalf("panel message id = %q, want %q", got.PanelMessageID, want.PanelMessageID)
	}
	if got.Status != want.Status {
		t.Fatalf("status = %q, want %q", got.Status, want.Status)
	}
}

func TestParseTicketValueReadsBothWidths(t *testing.T) {
	// The pipe-joined value gained two fields; a value written by an older
	// build must still parse, with the new halves empty.
	old, ok := parseTicketValue("c1", "g1|u1")
	wantParsedTicket(t, old, ok, Ticket{GuildID: "g1", OpenerID: "u1", Status: TicketStatusOpen})
	full, ok := parseTicketValue("c1", ticketValue(Ticket{GuildID: "g1", OpenerID: "u1", ClaimedBy: "mod1", PanelMessageID: "m1"}))
	wantParsedTicket(t, full, ok, Ticket{
		GuildID: "g1", OpenerID: "u1", ClaimedBy: "mod1",
		PanelMessageID: "m1", Status: TicketStatusClaimed,
	})
	if _, ok := parseTicketValue("c1", "garbage"); ok {
		t.Fatal("a value with no separator is not a ticket")
	}
}
