package module

import "ItsBagelBot/internal/domain/event/lane"

// Role is a chatter's effective permission tier, ordered low to high so a gate
// is a simple comparison. Twitch ships the lead-moderator badge instead of the
// moderator badge, so lead_moderator ranks above moderator and satisfies any
// gate a plain moderator would.
type Role int

const (
	RoleEveryone Role = iota
	RoleSubscriber
	RoleVIP
	RoleModerator
	RoleLeadModerator
	RoleBroadcaster
)

// badgeRoles maps a Twitch badge set_id onto the role it grants. Founders are
// subscribers; sub-gifter and other cosmetic badges grant nothing.
var badgeRoles = map[string]Role{
	"broadcaster":    RoleBroadcaster,
	"lead_moderator": RoleLeadModerator,
	"moderator":      RoleModerator,
	"vip":            RoleVIP,
	"subscriber":     RoleSubscriber,
	"founder":        RoleSubscriber,
}

// permRoles maps a command's stored perm string onto the minimum role required.
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

// ParseRole resolves the chatter's highest role from the event badges, plus the
// broadcaster shortcut: the channel owner always resolves to RoleBroadcaster
// even if the badge is absent. Unknown badges contribute nothing.
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

// ParsePerm resolves a command's perm string to the minimum role required.
// An empty or unknown value defaults to RoleEveryone.
func ParsePerm(perm string) Role {
	if perm == "" {
		return RoleEveryone
	}
	if r, ok := permRoles[perm]; ok {
		return r
	}
	return RoleEveryone
}

// Allows reports whether this role meets or exceeds the required role.
func (r Role) Allows(required Role) bool { return r >= required }
