// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package conf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultSubjectsAreStable(t *testing.T) {
	internal, viaProjector, lanes := LoadProjection(), LoadProjectionViaProjector(), LoadLanes()

	got := map[string]string{
		"internal users":         internal.ProjectionUsersSubject,
		"internal modules":       internal.ProjectionModulesSubject,
		"internal commands":      internal.ProjectionCommandsSubject,
		"internal invalidation":  internal.CacheInvalidationPrefix,
		"projector users":        viaProjector.ProjectionUsersSubject,
		"projector modules":      viaProjector.ProjectionModulesSubject,
		"projector commands":     viaProjector.ProjectionCommandsSubject,
		"projector invalidation": viaProjector.CacheInvalidationPrefix,
		"ingress premium lane":   lanes.PremiumSubject,
		"ingress standard lane":  lanes.StandardSubject,
		"outgress premium lane":  lanes.OutgressPremiumSubject,
		"outgress standard lane": lanes.OutgressStandardSubject,
	}
	want := map[string]string{
		"internal users":         "rpc.internal.projection.users.get",
		"internal modules":       "rpc.internal.projection.modules.get",
		"internal commands":      "rpc.internal.projection.commands.get",
		"internal invalidation":  "cache.invalidate",
		"projector users":        "rpc.internal.projection.users.get",
		"projector modules":      "rpc.projector.dashboard.modules.get",
		"projector commands":     "rpc.projector.dashboard.commands.get",
		"projector invalidation": "cache.invalidate",
		"ingress premium lane":   "twitch.ingress.event.premium",
		"ingress standard lane":  "twitch.ingress.event.standard",
		"outgress premium lane":  "twitch.outgress.premium",
		"outgress standard lane": "twitch.outgress.standard",
	}
	assert.Equal(t, want, got)
}

func TestSharedSubjectDefaultsAreStable(t *testing.T) {
	got := map[string]string{}
	for _, s := range []Subject{
		UsersGetSubject, UsersEmailSubject, TokensSubjectPrefix, BillingApplySubject,
		CommandsPrefix, FetchKeyPrefix, ModulesPrefix, GoveeKeyPrefix, SpotifyKeyPrefix,
		LoyaltyPrefix, GossipPrefix, BroadcasterLive, AdminNotifications, NotificationsCleanup,
		StreamLaneSubject, OutgressSystemSubject, OutgressRPCPrefix,
	} {
		got[s.key] = s.Get()
	}
	assert.Equal(t, map[string]string{
		"NATS_INTERNAL_USERS_GET_SUBJECT":          "rpc.internal.users.get",
		"NATS_INTERNAL_USERS_EMAIL_SUBJECT":        "rpc.internal.users.email.get",
		"NATS_INTERNAL_TOKENS_SUBJECT_PREFIX":      "rpc.internal.tokens",
		"NATS_INTERNAL_BILLING_SUBJECT":            "rpc.internal.billing.apply",
		"NATS_COMMANDS_SUBJECT_PREFIX":             "rpc.commands",
		"NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX":   "rpc.internal.commands.fetchkey",
		"NATS_MODULES_SUBJECT_PREFIX":              "rpc.modules",
		"NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX":   "rpc.internal.govee.key",
		"NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX": "rpc.internal.spotify.key",
		"NATS_LOYALTY_SUBJECT_PREFIX":              "rpc.loyalty",
		"NATS_GOSSIP_SUBJECT_PREFIX":               "rpc.gossip",
		"NATS_BROADCASTER_LIVE_SUBJECT":            "rpc.broadcaster.live.get",
		"NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX":  "rpc.admin.notifications",
		"NATS_NOTIFICATIONS_CLEANUP_SUBJECT":       "rpc.internal.notifications.cleanup",
		"NATS_SUBJECT_LANE_STREAM":                 "twitch.ingress.event.stream",
		"NATS_OUTGRESS_SYSTEM_SUBJECT":             "twitch.outgress.system",
		"NATS_OUTGRESS_RPC_PREFIX":                 "rpc.outgress",
	}, got)
}
