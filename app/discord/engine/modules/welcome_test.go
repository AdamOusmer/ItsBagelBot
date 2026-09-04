// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"context"
	"testing"

	"ItsBagelBot/app/discord/engine/module"
	ddiscord "ItsBagelBot/internal/domain/discord"
	"ItsBagelBot/pkg/codec"

	"go.uber.org/zap"
)

// fakeTiers is a TierReader with one canned answer. Production wires nil
// here today (no Discord-to-Twitch viewer link exists yet, see TierReader),
// so this double is what exercises the linked half at all.
type fakeTiers struct {
	tier   string
	linked bool
	asked  []string
}

func (f *fakeTiers) Tier(_ context.Context, _ string, userID string) (string, bool) {
	f.asked = append(f.asked, userID)
	return f.tier, f.linked
}

func autoRoleConfig() ddiscord.Config {
	return ddiscord.Config{
		GuildID: "g1", MemberRoleID: "mem",
		SubscriberRoleID: "sub", VIPRoleID: "vip", RegularsRoleID: "reg",
		// Welcome and logs off: this test is about roles only.
		WelcomeEnabled: "off", LogsEnabled: "off",
	}
}

func memberAddEvent(t *testing.T, roles []string) *module.Context {
	t.Helper()
	raw, err := codec.Marshal(map[string]any{
		"guild_id": "g1",
		"roles":    roles,
		"user":     map[string]any{"id": "u1", "username": "fan"},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return &module.Context{
		Event:         ddiscord.Event{Type: "GUILD_MEMBER_ADD", GuildID: "g1", Raw: raw},
		Config:        autoRoleConfig(),
		BroadcasterID: "999",
		Log:           zap.NewNop(),
	}
}

func runMemberAdd(t *testing.T, c *module.Context, tiers TierReader) []ddiscord.Command {
	t.Helper()
	var got []ddiscord.Command
	h := welcomeModule{tiers: tiers}
	if err := h.onMemberAdd(context.Background(), c, func(cmd ddiscord.Command) { got = append(got, cmd) }); err != nil {
		t.Fatalf("onMemberAdd: %v", err)
	}
	return got
}

func roleCommands(cmds []ddiscord.Command) map[string]string {
	out := map[string]string{}
	for _, c := range cmds {
		if c.Type != ddiscord.TypeAddRole && c.Type != ddiscord.TypeRemoveRole {
			continue
		}
		var p ddiscord.RolePayload
		_ = codec.Unmarshal(c.Payload, &p)
		out[p.RoleID] = c.Type
	}
	return out
}

// With no tier reader wired (production today) a join grants the member
// role and touches nothing else.
func TestAutoRoleGrantsMemberRoleWithoutATierReader(t *testing.T) {
	got := roleCommands(runMemberAdd(t, memberAddEvent(t, nil), nil))

	if len(got) != 1 || got["mem"] != ddiscord.TypeAddRole {
		t.Fatalf("commands = %v, want a single member add", got)
	}
}

// An unlinked member must keep tier roles they already hold: stripping a
// VIP role from someone whose link merely is not readable is the one
// mistake a streamer would notice and could not undo.
func TestAutoRoleNeverStripsTierRolesForAnUnlinkedMember(t *testing.T) {
	tiers := &fakeTiers{linked: false}
	got := roleCommands(runMemberAdd(t, memberAddEvent(t, []string{"mem", "vip"}), tiers))

	if len(got) != 0 {
		t.Fatalf("commands = %v, want none", got)
	}
	if len(tiers.asked) != 1 || tiers.asked[0] != "u1" {
		t.Fatalf("tier reader asked = %v, want [u1]", tiers.asked)
	}
}

func TestAutoRoleAppliesLinkedTierAndRemovesTheRest(t *testing.T) {
	tiers := &fakeTiers{tier: ddiscord.TierVIP, linked: true}
	got := roleCommands(runMemberAdd(t, memberAddEvent(t, []string{"sub", "reg"}), tiers))

	want := map[string]string{
		"mem": ddiscord.TypeAddRole,
		"vip": ddiscord.TypeAddRole,
		"sub": ddiscord.TypeRemoveRole,
		"reg": ddiscord.TypeRemoveRole,
	}
	if len(got) != len(want) {
		t.Fatalf("commands = %v, want %v", got, want)
	}
	for role, typ := range want {
		if got[role] != typ {
			t.Fatalf("role %q = %q, want %q", role, got[role], typ)
		}
	}
}

// The switch is default-ON, so only an explicit "off" stops it.
func TestAutoRoleOffEmitsNothing(t *testing.T) {
	c := memberAddEvent(t, nil)
	cfg := c.Config
	cfg.AutoRoleEnabled = "off"
	c.Config = cfg

	if got := roleCommands(runMemberAdd(t, c, &fakeTiers{tier: ddiscord.TierVIP, linked: true})); len(got) != 0 {
		t.Fatalf("commands = %v, want none with autorole off", got)
	}
}

// Role commands must ride the DEFAULT lane: they are not moderation, and
// putting them on the mod lane would let a join storm crowd out timeouts.
func TestAutoRoleCommandsRideTheDefaultLane(t *testing.T) {
	for _, c := range runMemberAdd(t, memberAddEvent(t, nil), nil) {
		if ddiscord.Lane(c.Type) != ddiscord.LaneDefault {
			t.Fatalf("%s rides %q, want the default lane", c.Type, ddiscord.Lane(c.Type))
		}
	}
}
