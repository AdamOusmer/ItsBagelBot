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
