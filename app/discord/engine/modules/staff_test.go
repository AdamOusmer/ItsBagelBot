// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package modules_test

import (
	"testing"

	"ItsBagelBot/app/discord/engine/internal/decode"
	"ItsBagelBot/app/discord/engine/modules"
	"ItsBagelBot/internal/discordapi"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func hasCommand(cmds []ddiscord.Command, kind string) bool {
	for _, c := range cmds {
		if c.Type == kind {
			return true
		}
	}
	return false
}

func TestModerationAndTicketDeskGates(t *testing.T) {
	cfg := baseConfig()
	cfg.OwnerRoleID, cfg.LeadModRoleID = "o", "l"
	cases := []struct {
		name       string
		perms      string
		roles      []string
		moderates  bool
		claimsDesk bool
	}{
		{"neither a permission nor a role", "0", []string{"stranger"}, false, false},
		{"a moderation permission alone", "8", nil, true, true},
		{"a bagel role alone", "0", []string{"l"}, true, true},
		{"both a permission and a role", "8", []string{"m"}, true, true},
		{"unparseable permissions fall back to roles", "", []string{"o"}, true, true},
		{"unparseable permissions and no role", "", nil, false, false},
		{"TestDeskStaffIsNotModStaff: a ticket desk helper passes the desk gate only", "0", []string{"helper"}, false, true},
		{"TestDeskStaffIsNotModStaff: mod staff passes both gates", "0", []string{"m"}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			member := actor{id: "u1", name: "fan", perms: tc.perms, roles: tc.roles}
			kick := member.inSupport()
			kick.Data.Options = []decode.InteractionOption{userOption("u2")}
			d := newDesk(t, cfg)
			d.open(ada)

			kicked := hasCommand(runHandler(t, modules.Moderation(nil, zap.NewNop()).Slash["kick"], cfg, kick), ddiscord.TypeKickMember)
			claim := d.button(discordapi.CustomTicketClaim, member.inTicket())

			require.Equal(t, tc.moderates, kicked, "moderation gate")
			require.Equal(t, tc.claimsDesk, claim == "Claimed.", "desk gate")
		})
	}
}
