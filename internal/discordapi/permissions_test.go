// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import (
	"strconv"
	"testing"
)

func perm(v uint64) string { return strconv.FormatUint(v, 10) }

func TestChannelPermissionsResolution(t *testing.T) {
	const send = PermViewChannel | PermSendMessages
	roles := []Snowflake{
		{ID: "g", Permissions: perm(send)},
		{ID: "r-open", Permissions: perm(0)},
		{ID: "r-admin", Permissions: perm(PermAdministrator)},
		{ID: "r-mute", Permissions: perm(0)},
	}
	cases := []struct {
		name       string
		memberRole []string
		overwrites []PermissionOverwrite
		wantSend   bool
	}{
		{"base only", nil, nil, true},
		{"everyone deny send", nil, []PermissionOverwrite{{ID: "g", Type: 0, Deny: perm(PermSendMessages)}}, false},
		{"role allow beats everyone deny", []string{"r-open"}, []PermissionOverwrite{
			{ID: "g", Type: 0, Deny: perm(PermSendMessages)},
			{ID: "r-open", Type: 0, Allow: perm(PermSendMessages)},
		}, true},
		{"role deny beats base", []string{"r-mute"}, []PermissionOverwrite{
			{ID: "r-mute", Type: 0, Deny: perm(PermSendMessages)},
		}, false},
		{"member allow beats role deny", []string{"r-mute"}, []PermissionOverwrite{
			{ID: "r-mute", Type: 0, Deny: perm(PermSendMessages)},
			{ID: "bot", Type: 1, Allow: perm(PermSendMessages)},
		}, true},
		{"member deny beats role allow", []string{"r-open"}, []PermissionOverwrite{
			{ID: "r-open", Type: 0, Allow: perm(PermSendMessages)},
			{ID: "bot", Type: 1, Deny: perm(PermSendMessages)},
		}, false},
		{"administrator ignores every deny", []string{"r-admin"}, []PermissionOverwrite{
			{ID: "g", Type: 0, Deny: perm(send)},
			{ID: "bot", Type: 1, Deny: perm(send)},
		}, true},
		{"view denied hides channel", nil, []PermissionOverwrite{{ID: "g", Type: 0, Deny: perm(PermViewChannel)}}, false},
		{"overwrite of another member ignored", nil, []PermissionOverwrite{
			{ID: "someone", Type: 1, Deny: perm(PermSendMessages)},
		}, true},
		{"overwrite type mismatch ignored", nil, []PermissionOverwrite{
			{ID: "bot", Type: 0, Deny: perm(PermSendMessages)},
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := MemberPermissions{GuildID: "g", UserID: "bot", Roles: tc.memberRole}
			got := ChannelPermissions(m, BasePermissions(m, roles), tc.overwrites)
			if HasPermissions(got, send) != tc.wantSend {
				t.Fatalf("can send = %v, want %v (perms %d)", !tc.wantSend, tc.wantSend, got)
			}
		})
	}
}

func TestParsePermissions(t *testing.T) {
	cases := map[string]uint64{
		"":                     0,
		"x":                    0,
		"2048":                 PermSendMessages,
		"9223372036854775808":  1 << 63,
		"18446744073709551615": ^uint64(0),
	}
	for in, want := range cases {
		if got := ParsePermissions(in); got != want {
			t.Fatalf("ParsePermissions(%q) = %d, want %d", in, got, want)
		}
	}
}
