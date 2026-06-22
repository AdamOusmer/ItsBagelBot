package module

import (
	"testing"

	"ItsBagelBot/internal/domain/event/lane"

	"github.com/stretchr/testify/assert"
)

func chatEnv(chatterID, broadcasterID string, badges ...lane.Badge) lane.Envelope {
	return lane.Envelope{
		Type:              "channel.chat.message",
		ChatterUserID:     chatterID,
		BroadcasterUserID: broadcasterID,
		Badges:            badges,
	}
}

func TestParseRole(t *testing.T) {
	tests := []struct {
		name string
		env  lane.Envelope
		want Role
	}{
		{"no badges is everyone", chatEnv("1", "2"), RoleEveryone},
		{"broadcaster by id", chatEnv("2", "2"), RoleBroadcaster},
		{"subscriber badge", chatEnv("1", "2", lane.Badge{SetID: "subscriber"}), RoleSubscriber},
		{"founder is subscriber", chatEnv("1", "2", lane.Badge{SetID: "founder"}), RoleSubscriber},
		{"vip badge", chatEnv("1", "2", lane.Badge{SetID: "vip"}), RoleVIP},
		{"moderator badge", chatEnv("1", "2", lane.Badge{SetID: "moderator"}), RoleModerator},
		{"lead moderator badge", chatEnv("1", "2", lane.Badge{SetID: "lead_moderator"}), RoleLeadModerator},
		{"broadcaster badge", chatEnv("1", "2", lane.Badge{SetID: "broadcaster"}), RoleBroadcaster},
		{"highest of many wins", chatEnv("1", "2", lane.Badge{SetID: "subscriber"}, lane.Badge{SetID: "vip"}), RoleVIP},
		{"unknown badge ignored", chatEnv("1", "2", lane.Badge{SetID: "sub-gifter"}), RoleEveryone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseRole(tt.env))
		})
	}
}

func TestLeadModeratorSatisfiesModeratorGate(t *testing.T) {
	// Twitch ships the lead_moderator badge instead of moderator, so a lead mod
	// must clear a "mod" gate.
	role := ParseRole(chatEnv("1", "2", lane.Badge{SetID: "lead_moderator"}))
	assert.True(t, role.Allows(ParsePerm("mod")))
	assert.True(t, role.Allows(ParsePerm("lead_mod")))
	assert.False(t, role.Allows(ParsePerm("broadcaster")))
}

func TestParsePerm(t *testing.T) {
	assert.Equal(t, RoleEveryone, ParsePerm(""))
	assert.Equal(t, RoleEveryone, ParsePerm("nonsense"))
	assert.Equal(t, RoleSubscriber, ParsePerm("sub"))
	assert.Equal(t, RoleVIP, ParsePerm("vip"))
	assert.Equal(t, RoleModerator, ParsePerm("mod"))
	assert.Equal(t, RoleLeadModerator, ParsePerm("lead_mod"))
	assert.Equal(t, RoleBroadcaster, ParsePerm("broadcaster"))
}

func TestAllowsOrdering(t *testing.T) {
	assert.True(t, RoleBroadcaster.Allows(RoleEveryone))
	assert.True(t, RoleModerator.Allows(RoleSubscriber))
	assert.True(t, RoleSubscriber.Allows(RoleSubscriber))
	assert.False(t, RoleSubscriber.Allows(RoleModerator))
	assert.False(t, RoleEveryone.Allows(RoleSubscriber))
}
