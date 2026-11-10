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
	CacheInvalidationPrefix   = Subject{"NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"}
	ProjectionUsersSubject    = Subject{"NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"}
	ProjectionModulesSubject  = Subject{"NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.internal.projection.modules.get"}
	ProjectionCommandsSubject = Subject{"NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.internal.projection.commands.get"}
	ProjectorDashboardPrefix  = Subject{"NATS_PROJECTOR_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.projector.dashboard"}

	IngressPremiumSubject   = Subject{"NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"}
	IngressStandardSubject  = Subject{"NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"}
	StreamLaneSubject       = Subject{"NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"}
	OutgressPremiumSubject  = Subject{"NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"}
	OutgressStandardSubject = Subject{"NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"}
	OutgressSystemSubject   = Subject{"NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"}
	OutgressRPCPrefix       = Subject{"NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"}

	UsersGetSubject      = Subject{"NATS_INTERNAL_USERS_GET_SUBJECT", "bagel.rpc.internal.users.get"}
	UsersEmailSubject    = Subject{"NATS_INTERNAL_USERS_EMAIL_SUBJECT", "bagel.rpc.internal.users.email.get"}
	TokensSubjectPrefix  = Subject{"NATS_INTERNAL_TOKENS_SUBJECT_PREFIX", "bagel.rpc.internal.tokens"}
	BillingApplySubject  = Subject{"NATS_INTERNAL_BILLING_SUBJECT", "bagel.rpc.internal.billing.apply"}
	CommandsPrefix       = Subject{"NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands"}
	FetchKeyPrefix       = Subject{"NATS_INTERNAL_FETCH_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.commands.fetchkey"}
	ModulesPrefix        = Subject{"NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules"}
	GoveeKeyPrefix       = Subject{"NATS_INTERNAL_GOVEE_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.govee.key"}
	SpotifyKeyPrefix     = Subject{"NATS_INTERNAL_SPOTIFY_KEY_SUBJECT_PREFIX", "bagel.rpc.internal.spotify.key"}
	LoyaltyPrefix        = Subject{"NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty"}
	GossipPrefix         = Subject{"NATS_GOSSIP_SUBJECT_PREFIX", "bagel.rpc.gossip"}
	BroadcasterLive      = Subject{"NATS_BROADCASTER_LIVE_SUBJECT", "bagel.rpc.broadcaster.live.get"}
	AdminNotifications   = Subject{"NATS_ADMIN_NOTIFICATIONS_SUBJECT_PREFIX", "bagel.rpc.admin.notifications"}
	NotificationsCleanup = Subject{"NATS_NOTIFICATIONS_CLEANUP_SUBJECT", "bagel.rpc.internal.notifications.cleanup"}
)
