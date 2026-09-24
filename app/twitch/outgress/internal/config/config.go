// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package config

import (
	"time"

	"ItsBagelBot/pkg/env"
	"ItsBagelBot/pkg/svcboot"
)

type Config struct {
	svcboot.Infra

	PremiumSubject  string
	StandardSubject string
	SystemSubject   string
	RPCPrefix       string

	MinRoutines    int
	MaxRoutines    int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	SystemWorkers int

	TwitchClientID     string
	TwitchClientSecret string

	TwitchConduitID string

	ConduitSubject string

	TwitchBotUserID string

	TwitchBotRefreshToken string

	TokensSubjectPrefix string

	CacheInvalidatePrefix string

	LiveTTL time.Duration

	StreamLaneSubject string

	AuthzGrantedSubject    string
	AuthzRevokedSubject    string
	AuthzSubRevokedSubject string

	NotifySendSubject  string
	UsersStateSubject  string
	UsersActiveSubject string

	RateRegion          string
	LeaseEpoch          time.Duration
	LeaseGuard          time.Duration
	LeaseMinMembers     int
	LeaseReplicas       int
	LeaseReplicaTimeout time.Duration
}

func Load() *Config {
	return &Config{
		Infra:                  svcboot.LoadInfra(),
		PremiumSubject:         env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		StandardSubject:        env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),
		SystemSubject:          env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),
		RPCPrefix:              env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),
		TwitchClientID:         env.MustGet("TWITCH_CLIENT_ID"),
		TwitchClientSecret:     env.MustGet("TWITCH_CLIENT_SECRET"),
		TwitchConduitID:        env.Get("TWITCH_CONDUIT_ID", ""),
		ConduitSubject:         env.Get("NATS_CONDUIT_SUBJECT", "bagel.rpc.ingress.conduit.get"),
		TwitchBotUserID:        env.Get("TWITCH_BOT_USER_ID", ""),
		TwitchBotRefreshToken:  env.Get("TWITCH_BOT_REFRESH_TOKEN", ""),
		TokensSubjectPrefix:    env.Get("NATS_INTERNAL_TOKENS_SUBJECT_PREFIX", "bagel.rpc.internal.tokens"),
		CacheInvalidatePrefix:  env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),
		LiveTTL:                env.GetDuration("WORKER_LIVE_TTL", 12*time.Hour),
		StreamLaneSubject:      env.Get("NATS_SUBJECT_LANE_STREAM", "twitch.ingress.event.stream"),
		AuthzGrantedSubject:    env.Get("NATS_SUBJECT_AUTHZ_GRANTED", "twitch.ingress.status.authz.granted"),
		AuthzRevokedSubject:    env.Get("NATS_SUBJECT_AUTHZ_REVOKED", "twitch.ingress.status.authz.revoked"),
		AuthzSubRevokedSubject: env.Get("NATS_SUBJECT_AUTHZ_SUBREVOKED", "twitch.ingress.status.authz.subrevoked"),
		NotifySendSubject:      env.Get("NATS_NOTIFY_SEND_SUBJECT", "bagel.rpc.admin.notifications.send"),
		UsersStateSubject:      env.Get("NATS_USERS_STATE_SUBJECT", "bagel.rpc.dashboard.state_get"),
		UsersActiveSubject:     env.Get("NATS_USERS_ACTIVE_SUBJECT", "bagel.rpc.dashboard.active_set"),
		MinRoutines:            env.GetInt("OUTGRESS_MIN_ROUTINES", 2),
		MaxRoutines:            env.GetInt("OUTGRESS_MAX_ROUTINES", 8),
		MaxConsumers:           env.GetInt("OUTGRESS_MAX_CONSUMERS", 3),
		ScaleUpAfter:           env.GetDuration("OUTGRESS_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter:         env.GetDuration("OUTGRESS_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve:         env.GetInt("OUTGRESS_PREMIUM_RESERVE_PERCENT", 25),
		SystemWorkers:          env.GetInt("OUTGRESS_SYSTEM_WORKERS", 2),
		RateRegion:             env.Get("OUTGRESS_REGION", "local"),
		LeaseEpoch:             env.GetDuration("OUTGRESS_LEASE_EPOCH", 30*time.Second),
		LeaseGuard:             env.GetDuration("OUTGRESS_LEASE_GUARD", 250*time.Millisecond),
		LeaseMinMembers:        env.GetInt("OUTGRESS_LEASE_MIN_MEMBERS", 1),
		LeaseReplicas:          env.GetInt("OUTGRESS_LEASE_REPLICAS", 0),
		LeaseReplicaTimeout:    env.GetDuration("OUTGRESS_LEASE_REPLICA_TIMEOUT", 2*time.Second),
	}
}
