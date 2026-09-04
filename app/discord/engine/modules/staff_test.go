// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules

import (
	"testing"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"
)

func interactionBy(permissions string, roles []string) decode.InteractionEvent {
	var in decode.InteractionEvent
	in.GuildID = "g1"
	in.Member.User = decode.UserRef{ID: "u1", Username: "fan"}
	in.Member.Permissions = permissions
	in.Member.Roles = roles
	return in
}

// The gate is role OR permission. Before this, a Lead Mod whose role carried
// no moderation bit was refused by every slash command in the bot, while the
// same person could see every ticket channel.
func TestIsStaffOrMod(t *testing.T) {
	cfg := ddiscord.Config{OwnerRoleID: "o", LeadModRoleID: "l", ModsRoleID: "m"}
	cases := []struct {
		name  string
		perms string
		roles []string
		want  bool
	}{
		{"neither", "0", []string{"stranger"}, false},
		{"permission only", "8", nil, true},
		{"bagel role only", "0", []string{"l"}, true},
		{"both", "8", []string{"m"}, true},
		{"unparseable permissions fall back to roles", "", []string{"o"}, true},
		{"unparseable permissions and no role", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStaffOrMod(cfg, interactionBy(tc.perms, tc.roles)); got != tc.want {
				t.Fatalf("isStaffOrMod = %v, want %v", got, tc.want)
			}
		})
	}
}

// A ticket is closable by its opener, by a permission-bearing mod, and now
// by a Bagel staff role holder who has neither.
func TestCanCloseTicket(t *testing.T) {
	cfg := ddiscord.Config{ModsRoleID: "m"}
	ticket := discordstore.Ticket{ChannelID: "c1", OpenerID: "opener"}
	cases := []struct {
		name string
		in   decode.InteractionEvent
		want bool
	}{
		{"stranger", interactionBy("0", []string{"x"}), false},
		{"mods role", interactionBy("0", []string{"m"}), true},
		{"permission bit", interactionBy("8", nil), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := canCloseTicket(ticket, tc.in, cfg); got != tc.want {
				t.Fatalf("canCloseTicket = %v, want %v", got, tc.want)
			}
		})
	}
	opener := interactionBy("0", nil)
	opener.Member.User.ID = "opener"
	if !canCloseTicket(ticket, opener, cfg) {
		t.Fatal("the opener must always be able to close their own ticket")
	}
}

// The ticket desk staff list, when set, is what gates a ticket channel's
// overwrites -- not the Owner/Lead Mod/Mods trio.
func TestTicketOverwritesUseTheDeskStaffList(t *testing.T) {
	cfg := ddiscord.Config{OwnerRoleID: "o", ModsRoleID: "m", TicketStaffRoles: "helper"}
	got := ticketOverwrites(cfg, interactionBy("0", nil))

	var ids []string
	for _, o := range got {
		ids = append(ids, o.ID)
	}
	// @everyone deny, opener allow, then exactly the desk list.
	if len(ids) != 3 || ids[2] != "helper" {
		t.Fatalf("overwrite targets = %v, want the desk staff list", ids)
	}
}
