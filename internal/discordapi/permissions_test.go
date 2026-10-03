// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi_test

import (
	"strconv"
	"testing"

	api "ItsBagelBot/internal/discordapi"
)

func perm(v uint64) string { return strconv.FormatUint(v, 10) }

func TestChannelPermissionsResolution(t *testing.T) {
	const send = api.PermViewChannel | api.PermSendMessages
	roles := []api.Snowflake{
		{ID: "g", Permissions: perm(send)},
		{ID: "r-open", Permissions: perm(0)},
		{ID: "r-admin", Permissions: perm(api.PermAdministrator)},
		{ID: "r-mute", Permissions: perm(0)},
	}
	cases := []struct {
		name       string
		memberRole []string
		overwrites []api.PermissionOverwrite
		wantSend   bool
	}{
		{"base only", nil, nil, true},
		{"everyone deny send", nil, []api.PermissionOverwrite{{ID: "g", Type: 0, Deny: perm(api.PermSendMessages)}}, false},
		{"role allow beats everyone deny", []string{"r-open"}, []api.PermissionOverwrite{
			{ID: "g", Type: 0, Deny: perm(api.PermSendMessages)},
			{ID: "r-open", Type: 0, Allow: perm(api.PermSendMessages)},
		}, true},
		{"role deny beats base", []string{"r-mute"}, []api.PermissionOverwrite{
			{ID: "r-mute", Type: 0, Deny: perm(api.PermSendMessages)},
		}, false},
		{"member allow beats role deny", []string{"r-mute"}, []api.PermissionOverwrite{
			{ID: "r-mute", Type: 0, Deny: perm(api.PermSendMessages)},
			{ID: "bot", Type: 1, Allow: perm(api.PermSendMessages)},
		}, true},
		{"member deny beats role allow", []string{"r-open"}, []api.PermissionOverwrite{
			{ID: "r-open", Type: 0, Allow: perm(api.PermSendMessages)},
			{ID: "bot", Type: 1, Deny: perm(api.PermSendMessages)},
		}, false},
		{"administrator ignores every deny", []string{"r-admin"}, []api.PermissionOverwrite{
			{ID: "g", Type: 0, Deny: perm(send)},
			{ID: "bot", Type: 1, Deny: perm(send)},
		}, true},
		{"view denied hides channel", nil, []api.PermissionOverwrite{{ID: "g", Type: 0, Deny: perm(api.PermViewChannel)}}, false},
		{"overwrite of another member ignored", nil, []api.PermissionOverwrite{
			{ID: "someone", Type: 1, Deny: perm(api.PermSendMessages)},
		}, true},
		{"overwrite type mismatch ignored", nil, []api.PermissionOverwrite{
			{ID: "bot", Type: 0, Deny: perm(api.PermSendMessages)},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := api.MemberPermissions{GuildID: "g", UserID: "bot", Roles: tc.memberRole}
			got := api.ChannelPermissions(m, api.BasePermissions(m, roles), tc.overwrites)
			if api.HasPermissions(got, send) != tc.wantSend {
				t.Fatalf("can send = %v, want %v (perms %d)", !tc.wantSend, tc.wantSend, got)
			}
		})
	}
}

func TestParsePermissions(t *testing.T) {
	cases := map[string]uint64{
		"":                     0,
		"x":                    0,
		"2048":                 api.PermSendMessages,
		"9223372036854775808":  1 << 63,
		"18446744073709551615": ^uint64(0),
	}
	for in, want := range cases {
		if got := api.ParsePermissions(in); got != want {
			t.Fatalf("ParsePermissions(%q) = %d, want %d", in, got, want)
		}
	}
}
