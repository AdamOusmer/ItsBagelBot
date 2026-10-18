// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package setup_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/discord/outgress/internal/setup"
	discapi "ItsBagelBot/internal/discordapi"
	"ItsBagelBot/internal/discordstore"
	ddiscord "ItsBagelBot/internal/domain/discord"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type pinView struct {
	Owner   string
	Mods    string
	VIP     string
	Member  string
	Created []string
	Dropped []string
	Staff   []string
}

func viewPins(got setup.GuildSetupResult, d *fakeDiscord) pinView {
	var staff []string
	for _, o := range d.createdChannels["staff"].PermissionOverwrites {
		staff = append(staff, o.ID)
	}
	return pinView{
		Owner: got.OwnerRoleID, Mods: got.ModsRoleID, VIP: got.VIPRoleID, Member: got.MemberRoleID,
		Created: d.roleNames(), Dropped: got.DroppedPins, Staff: staff,
	}
}

func TestSetupGuildPinnedRoles(t *testing.T) {
	everyRole := []string{ddiscord.RoleOwner, ddiscord.RoleLeadMod, ddiscord.RoleMods, ddiscord.RoleVIP, ddiscord.RoleRegulars, ddiscord.RoleMember}
	defaultStaff := []string{"guild-1", "role-owner", "role-lead mod", "role-mods"}
	cases := []struct {
		name string
		live []string
		pins map[string]string
		want pinView
	}{{
		name: "adopts pinned roles instead of creating them and grants them their channels",
		live: []string{"existing-mods", "existing-member"},
		pins: map[string]string{ddiscord.SlotMods: "existing-mods", ddiscord.SlotMember: "existing-member"},
		want: pinView{
			Owner: "role-owner", Mods: "existing-mods", VIP: "role-vip", Member: "existing-member",
			Created: []string{ddiscord.RoleOwner, ddiscord.RoleLeadMod, ddiscord.RoleVIP, ddiscord.RoleRegulars},
			Staff:   []string{"guild-1", "role-owner", "role-lead mod", "existing-mods"},
		},
	}, {
		name: "ignores a pin for an unknown slot",
		live: []string{"nope"},
		pins: map[string]string{"janitor": "nope"},
		want: pinView{Owner: "role-owner", Mods: "role-mods", VIP: "role-vip", Member: "role-member", Created: everyRole, Staff: defaultStaff},
	}, {
		name: "drops a pin whose role is gone and creates the role instead",
		pins: map[string]string{ddiscord.SlotMods: "deleted-role"},
		want: pinView{
			Owner: "role-owner", Mods: "role-mods", VIP: "role-vip", Member: "role-member", Created: everyRole,
			Dropped: []string{ddiscord.SlotMods}, Staff: defaultStaff,
		},
	}, {
		name: "reports dropped pins sorted and keeps the pins that are live",
		live: []string{"live-vip"},
		pins: map[string]string{
			ddiscord.SlotMods: "gone-1", ddiscord.SlotOwner: "gone-2", ddiscord.SlotMember: "gone-3", ddiscord.SlotVIP: "live-vip",
		},
		want: pinView{
			Owner: "role-owner", Mods: "role-mods", VIP: "live-vip", Member: "role-member",
			Created: []string{ddiscord.RoleOwner, ddiscord.RoleLeadMod, ddiscord.RoleMods, ddiscord.RoleRegulars, ddiscord.RoleMember},
			Dropped: []string{ddiscord.SlotMember, ddiscord.SlotMods, ddiscord.SlotOwner}, Staff: defaultStaff,
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{roles: existingRoles(tc.live)}

			got, err := newWorker(d, discordstore.NewMem()).SetupGuild(context.Background(), setup.GuildSetupRequest{
				GuildID: "guild-1", BroadcasterID: "42", PinnedRoles: tc.pins,
			})

			require.NoError(t, err)
			require.Equal(t, tc.want, viewPins(got, d))
		})
	}
}

func existingRoles(ids []string) []discapi.Snowflake {
	out := make([]discapi.Snowflake, 0, len(ids))
	for _, id := range ids {
		out = append(out, discapi.Snowflake{ID: id, Name: id})
	}
	return out
}

func roleGrants(d *fakeDiscord) map[string]string {
	out := map[string]string{}
	for _, r := range d.createdRoles {
		out[r.Name] = r.Permissions
	}
	return out
}

func TestSetupGuildCreatesRolesWithOnlyThePermissionsTheBotHolds(t *testing.T) {
	cases := []struct {
		name    string
		botPerm uint64
		want    map[string]string
	}{{
		name:    "invite permissions only",
		botPerm: ddiscord.BotPermissions,
		want: map[string]string{
			ddiscord.RoleLeadMod: "",
			ddiscord.RoleMods:    strconv.FormatInt(int64(ddiscord.PermModerator)&ddiscord.BotPermissions, 10),
		},
	}, {
		name:    "administrator",
		botPerm: uint64(ddiscord.PermAdministrator),
		want: map[string]string{
			ddiscord.RoleLeadMod: "8",
			ddiscord.RoleMods:    strconv.FormatInt(int64(ddiscord.PermModerator), 10),
		},
	}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &fakeDiscord{
				roles:    []discapi.Snowflake{{ID: "bot-role", Permissions: strconv.FormatUint(tc.botPerm, 10)}},
				botRoles: []string{"bot-role"},
			}
			w := setup.New(setup.Config{Discord: d, Store: discordstore.NewMem(), Log: zap.NewNop(), BotID: "bot-1"})

			_, err := w.SetupGuild(context.Background(), setup.GuildSetupRequest{GuildID: "guild-1", BroadcasterID: "42"})

			require.NoError(t, err)
			got := roleGrants(d)
			for role, perms := range tc.want {
				require.Equal(t, perms, got[role], role)
			}
		})
	}
}
