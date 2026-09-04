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
	if err := m.BindGuild(context.Background(), g, Broadcaster{ID: "42"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	got, ok := m.Broadcaster(context.Background(), g)
	if !ok || got.ID != "42" {
		t.Fatalf("broadcaster = %+v, %v", got, ok)
	}
	if err := m.UnbindGuild(context.Background(), g); err != nil {
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
	_ = m.RememberDesk(context.Background(), Guild{ID: "g2"})
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
