// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package module

import "ItsBagelBot/internal/domain/event/lane"

type Role int

const (
	RoleEveryone Role = iota
	RoleSubscriber
	RoleVIP
	RoleModerator
	RoleLeadModerator
	RoleBroadcaster
)

var badgeRoles = map[string]Role{
	"broadcaster":    RoleBroadcaster,
	"lead_moderator": RoleLeadModerator,
	"moderator":      RoleModerator,
	"vip":            RoleVIP,
	"subscriber":     RoleSubscriber,
	"founder":        RoleSubscriber,
}

var permRoles = map[string]Role{
	"everyone":    RoleEveryone,
	"sub":         RoleSubscriber,
	"subscriber":  RoleSubscriber,
	"vip":         RoleVIP,
	"mod":         RoleModerator,
	"moderator":   RoleModerator,
	"lead_mod":    RoleLeadModerator,
	"broadcaster": RoleBroadcaster,
}

func ParseRole(env lane.Envelope) Role {
	role := RoleEveryone
	if env.ChatterUserID != "" && env.ChatterUserID == env.BroadcasterUserID {
		role = RoleBroadcaster
	}
	for _, b := range env.Badges {
		if r, ok := badgeRoles[b.SetID]; ok && r > role {
			role = r
		}
	}
	return role
}

func ParsePerm(perm string) Role {
	if perm == "" {
		return RoleEveryone
	}
	if r, ok := permRoles[perm]; ok {
		return r
	}
	return RoleEveryone
}

func (r Role) Allows(required Role) bool { return r >= required }
