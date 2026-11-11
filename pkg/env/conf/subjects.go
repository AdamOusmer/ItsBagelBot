// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package conf

import "ItsBagelBot/pkg/env"

type Subject struct {
	key      string
	fallback string
}

func (s Subject) Get() string { return env.Get(s.key, s.fallback) }

var (
	CacheInvalidationPrefix   = Subject{"NATS_CACHE_INVALIDATION_PREFIX", "cache.invalidate"}
	ProjectionUsersSubject    = Subject{"NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "rpc.internal.projection.users.get"}
	ProjectionModulesSubject  = Subject{"NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "rpc.internal.projection.modules.get"}
	ProjectionCommandsSubject = Subject{"NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "rpc.internal.projection.commands.get"}
	ProjectorDashboardPrefix  = Subject{"NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "rpc.projector.dashboard"}

	IngressPremiumSubject   = Subject{"NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"}
	IngressStandardSubject  = Subject{"NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"}
	StreamLaneSubject       = Subject{"NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"}
	OutgressPremiumSubject  = Subject{"NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"}
	OutgressStandardSubject = Subject{"NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"}
	OutgressSystemSubject   = Subject{"NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"}
	OutgressRPCPrefix       = Subject{"NATS_OUTGRESS_RPC_PREFIX", "rpc.outgress"}

	UsersGetSubject      = Subject{"NATS_INTERNAL_USERS_GET_SUBJECT", "rpc.internal.users.get"}
	UsersEmailSubject    = Subject{"NATS_INTERNAL_USERS_EMAIL_SUBJECT", "rpc.internal.users.email.get"}
	TokensSubjectPrefix  = Subject{"NATS_INTERNAL_TOKENS_SUBJECT_PREFIX", "rpc.internal.tokens"}
	BillingApplySubject  = Subject{"NATS_INTERNAL_BILLING_SUBJECT", "rpc.internal.billing.apply"}
	CommandsPrefix       = Subject{"NATS_COMMANDS_SUBJECT_PREFIX", "rpc.commands"}
	FetchKeyPrefix       = Subject{"NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX", "rpc.internal.commands.fetchkey"}
	ModulesPrefix        = Subject{"NATS_MODULES_SUBJECT_PREFIX", "rpc.modules"}
	GoveeKeyPrefix       = Subject{"NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "rpc.internal.govee.key"}
	SpotifyKeyPrefix     = Subject{"NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX", "rpc.internal.spotify.key"}
	LoyaltyPrefix        = Subject{"NATS_LOYALTY_SUBJECT_PREFIX", "rpc.loyalty"}
	GossipPrefix         = Subject{"NATS_GOSSIP_SUBJECT_PREFIX", "rpc.gossip"}
	BroadcasterLive      = Subject{"NATS_BROADCASTER_LIVE_SUBJECT", "rpc.broadcaster.live.get"}
	AdminNotifications   = Subject{"NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "rpc.admin.notifications"}
	NotificationsCleanup = Subject{"NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "rpc.internal.notifications.cleanup"}
)
