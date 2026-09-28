// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import "strconv"

const (
	PermAdministrator int64 = 1 << 3
	PermViewChannel   int64 = 1 << 10
	PermSendMessages  int64 = 1 << 11
	PermEmbedLinks    int64 = 1 << 14

	allPermissions int64 = -1

	overwriteTypeMember = 1
)

type MemberPermissions struct {
	GuildID string
	UserID  string
	Roles   []string
}

func ParsePermissions(s string) int64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return int64(v)
}

func BasePermissions(m MemberPermissions, guildRoles []Snowflake) int64 {
	held := make(map[string]bool, len(m.Roles)+1)
	held[m.GuildID] = true
	for _, id := range m.Roles {
		held[id] = true
	}
	var perms int64
	for _, r := range guildRoles {
		if held[r.ID] {
			perms |= ParsePermissions(r.Permissions)
		}
	}
	if perms&PermAdministrator != 0 {
		return allPermissions
	}
	return perms
}

func ChannelPermissions(m MemberPermissions, base int64, overwrites []PermissionOverwrite) int64 {
	if base&PermAdministrator != 0 {
		return allPermissions
	}
	perms := base
	perms = applyOverwrite(perms, findOverwrite(overwrites, m.GuildID, 0))
	roleAllow, roleDeny := roleOverwrites(m, overwrites)
	perms = perms&^roleDeny | roleAllow
	return applyOverwrite(perms, findOverwrite(overwrites, m.UserID, overwriteTypeMember))
}

func findOverwrite(overwrites []PermissionOverwrite, id string, kind int) *PermissionOverwrite {
	for i := range overwrites {
		if overwrites[i].ID == id && overwrites[i].Type == kind {
			return &overwrites[i]
		}
	}
	return nil
}

func applyOverwrite(perms int64, o *PermissionOverwrite) int64 {
	if o == nil {
		return perms
	}
	return perms&^ParsePermissions(o.Deny) | ParsePermissions(o.Allow)
}

func roleOverwrites(m MemberPermissions, overwrites []PermissionOverwrite) (allow, deny int64) {
	for _, id := range m.Roles {
		if o := findOverwrite(overwrites, id, 0); o != nil {
			allow |= ParsePermissions(o.Allow)
			deny |= ParsePermissions(o.Deny)
		}
	}
	return allow, deny
}

func HasPermissions(perms, want int64) bool {
	return perms&want == want
}
