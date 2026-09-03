// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package store

import (
	"context"
	"testing"
)

func TestMemXPDailyAndClones(t *testing.T) {
	m := NewMem()
	m.PutGuild("g1", "42")
	id, ok := m.Broadcaster(context.Background(), "g1")
	if !ok || id != "42" {
		t.Fatalf("guild = %s %v", id, ok)
	}
	xp, leveled, level := m.AddXP(context.Background(), "g1", "u1")
	if xp != xpPerMessage || leveled || level != 0 {
		t.Fatalf("first xp = %d leveled=%v level=%d", xp, leveled, level)
	}
	xp2, _, _ := m.AddXP(context.Background(), "g1", "u1")
	if xp2 != xp {
		t.Fatalf("cooldown should skip, got %d", xp2)
	}
	ok, total := m.ClaimDaily(context.Background(), "g1", "u1")
	if !ok || total != xp+dailyXP {
		t.Fatalf("daily = %v %d", ok, total)
	}
	ok, _ = m.ClaimDaily(context.Background(), "g1", "u1")
	if ok {
		t.Fatal("second daily")
	}
	_ = m.TrackClone(context.Background(), Clone{ChannelID: "c1", GuildID: "g1", OwnerID: "u1"})
	if m.CloneCount(context.Background(), "g1") != 1 {
		t.Fatal("clone count")
	}
	_ = m.ForgetClone(context.Background(), Clone{ChannelID: "c1", GuildID: "g1"})
	if m.CloneCount(context.Background(), "g1") != 0 {
		t.Fatal("clone forgotten")
	}
}

func TestLevelOf(t *testing.T) {
	if levelOf(0) != 0 {
		t.Fatal("zero")
	}
	if levelOf(99) != 0 {
		t.Fatal("below 100")
	}
	if levelOf(100) != 1 {
		t.Fatal("level 1")
	}
	if levelOf(400) != 2 {
		t.Fatal("level 2")
	}
}
