// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package discordapi

import "strconv"

const (
	PermAdministrator uint64 = 1 << 3
	PermViewChannel   uint64 = 1 << 10
	PermSendMessages  uint64 = 1 << 11
	PermEmbedLinks    uint64 = 1 << 14

	allPermissions uint64 = ^uint64(0)

	overwriteTypeMember = 1
)

type MemberPermissions struct {
	GuildID string
	UserID  string
	Roles   []string
}

func ParsePermissions(s string) uint64 {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

func BasePermissions(m MemberPermissions, guildRoles []Snowflake) uint64 {
	held := make(map[string]bool, len(m.Roles)+1)
	held[m.GuildID] = true
	for _, id := range m.Roles {
		held[id] = true
	}
	var perms uint64
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

func ChannelPermissions(m MemberPermissions, base uint64, overwrites []PermissionOverwrite) uint64 {
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

func applyOverwrite(perms uint64, o *PermissionOverwrite) uint64 {
	if o == nil {
		return perms
	}
	return perms&^ParsePermissions(o.Deny) | ParsePermissions(o.Allow)
}

func roleOverwrites(m MemberPermissions, overwrites []PermissionOverwrite) (allow, deny uint64) {
	for _, id := range m.Roles {
		if o := findOverwrite(overwrites, id, 0); o != nil {
			allow |= ParsePermissions(o.Allow)
			deny |= ParsePermissions(o.Deny)
		}
	}
	return allow, deny
}

func HasPermissions(perms, want uint64) bool {
	return perms&want == want
}
