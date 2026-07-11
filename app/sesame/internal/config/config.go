// Package config loads sesame's runtime settings from the environment.
//
// sesame sits between ingress and outgress: it drains the ingress premium and
// standard lanes, runs each event through the engine pipeline, and publishes the
// resulting actions onto the outgress lanes. Every knob is a plain env var with a
// development-friendly default.
//
// Secret-provided vars (VALKEY_*, NATS_CACHE_INVALIDATION_PREFIX,
// TWITCH_SPECIAL_USER_IDS, TWITCH_BOT_USER_ID) keep the exact names the worker
// used, so the same Doppler config supplies them unchanged. Only the pod-tuning
// knobs are renamed WORKER_* -> SESAME_* (set in sesame's own manifest).
package config

import (
	"strings"
	"time"

	"ItsBagelBot/pkg/env"
)

type Config struct {
	NATSURL    string
	NATSRPCURL string

	// ConsumerName is the JetStream durable/queue group the subscriber binds. It
	// defaults to "worker" so sesame reuses the worker's existing lane consumer:
	// the lane consumers are DeliverAll, so a fresh durable would replay the whole
	// stream, and reusing the group means rollout overlap load-balances across the
	// shared DeliverGroup instead of double-processing. It is a genuine drop-in on
	// the same lanes and the same pkg/bus consumer.
	ConsumerName string

	// Ingress lanes sesame consumes: premium and standard. Both carry every
	// actionable event laned by broadcaster status, including the live
	// (stream.online/offline) events ingress dual-publishes onto them.
	PremiumSubject  string
	StandardSubject string

	// The one consumer drains both lanes into a shared, autoscaling pool of
	// pipeline routines. PremiumReserve keeps a slice of the pool for premium so a
	// standard flood never starves premium broadcasters.
	MinRoutines    int
	MaxRoutines    int
	MaxConsumers   int
	ScaleUpAfter   time.Duration
	ScaleDownAfter time.Duration
	PremiumReserve int

	// DrainTimeout bounds how long shutdown waits for handlers already dispatched
	// to finish after SIGTERM stops the consumer pulling. Keep it below the pod's
	// terminationGracePeriodSeconds so the drain completes before the kubelet
	// SIGKILLs the process. A handler that outlives the deadline is abandoned and
	// its event redelivered (the dedup claim is released on the nack path).
	DrainTimeout time.Duration

	// Outgress lane subjects the pipeline publishes onto, chosen from the event's
	// regress status (premium vs standard).
	OutgressPremiumSubject  string
	OutgressStandardSubject string
	// OutgressSystemSubject is the outgress system lane (off the chat budget); the
	// live key-expiry re-check publishes its Twitch Get Streams job here.
	OutgressSystemSubject string

	// ProjectionLiveSubject is the projector RPC the live store asks when its
	// shared live key is cold.
	ProjectionLiveSubject string

	// SpecialUserIDs is the comma-separated list of special (bagel-crew) Twitch
	// user ids, the same Doppler secret ingress uses to lane them premium.
	SpecialUserIDs string

	// BotUserID is the bot's own Twitch user id; the engine skips the bot's own
	// chat messages so it never reacts to itself.
	BotUserID string

	// AutomodEnforce arms the automod: false (default) runs it in shadow mode
	// (verdicts are logged, no action taken); true emits the ban/timeout actions.
	AutomodEnforce bool

	// ShieldEnabled lets a confirmed mass-raid escalate to channel-level Shield
	// Mode. It is stricter than AutomodEnforce (Shield Mode is aggressive and
	// broadcaster-visible) and only takes effect when AutomodEnforce is also on.
	// Off by default.
	ShieldEnabled bool

	// EmotesEnabled starts the background third-party emote-set refresher (BTTV,
	// FFZ, 7TV global sets) that feeds the automod's caps-heuristic false-positive
	// suppression. On by default; the endpoints are small, public and unauthenticated.
	EmotesEnabled bool

	// LiveTTL bounds how long a live key survives without a refresh.
	LiveTTL time.Duration

	// Projection RPC subjects: the cold-key fallbacks behind the Valkey
	// settings projection. Modules and commands ask the PROJECTOR's dashboard
	// get verbs — the projector owns Valkey, so its miss path hydrates the
	// projection and the next read is a plain Valkey hit; sesame never asks
	// the modules/commands services directly. Users still asks the users
	// service's projection verb (the projector exposes no user-shaped read).
	ProjectionUsersSubject    string
	ProjectionModulesSubject  string
	ProjectionCommandsSubject string

	// CacheInvalidationPrefix is the NATS subject prefix sesame subscribes to for
	// push invalidation of its in-process projection cache.
	CacheInvalidationPrefix string

	// CommandsDashboardPrefix is the NATS subject prefix the commands service
	// dashboard RPC subscribes to; sesame appends ".upsert" / ".delete" to
	// manage custom commands from chat (the !cmd module).
	CommandsDashboardPrefix string

	// ModulesRPCPrefix is the NATS subject prefix the modules service RPC
	// subscribes to; the quotes module appends ".quote.<verb>" to read and
	// write the channel quote book.
	ModulesRPCPrefix string

	// PublicBaseURL is the origin of the public console pages. The !cmd module
	// builds the channel command-page link from it as "<base>/user/<broadcaster
	// id>". Stored without a trailing slash.
	PublicBaseURL string

	// GatewayRPCPrefix is the NATS subject prefix the gateway service (external
	// API proxy + cache) subscribes to; the urchin/mcsr modules append
	// ".<provider>.<endpoint>".
	GatewayRPCPrefix string

	// LoyaltyRPCPrefix is the NATS subject prefix the loyalty service
	// subscribes to; the loyalty store appends its balance/counter verbs.
	LoyaltyRPCPrefix string

	// OutgressRPCPrefix is the outgress management RPC prefix; the loyalty
	// watch tick appends ".chatters.get" to list a live channel's chatters.
	OutgressRPCPrefix string

	// Valkey holds the settings projection (user tier + modules) sesame reads on
	// the hot path.
	ValkeyAddr     string
	ValkeyPassword string

	ListenAddr string
}

func Load() *Config {
	natsURL := env.Get("NATS_URL", "nats://127.0.0.1:4222")
	return &Config{
		NATSURL:    natsURL,
		NATSRPCURL: env.Get("NATS_RPC_URL", natsURL),

		ConsumerName: env.Get("SESAME_CONSUMER_NAME", "worker"),

		PremiumSubject:  env.Get("NATS_INGRESS_PREMIUM_SUBJECT", "twitch.ingress.event.premium"),
		StandardSubject: env.Get("NATS_INGRESS_STANDARD_SUBJECT", "twitch.ingress.event.standard"),

		MinRoutines:    env.GetInt("SESAME_MIN_ROUTINES", 2),
		MaxRoutines:    env.GetInt("SESAME_MAX_ROUTINES", 8),
		MaxConsumers:   env.GetInt("SESAME_MAX_CONSUMERS", 3),
		ScaleUpAfter:   env.GetDuration("SESAME_SCALE_UP_AFTER", 5*time.Second),
		ScaleDownAfter: env.GetDuration("SESAME_SCALE_DOWN_AFTER", 30*time.Second),
		PremiumReserve: env.GetInt("SESAME_PREMIUM_RESERVE_PERCENT", 25),

		DrainTimeout: env.GetDuration("SESAME_DRAIN_TIMEOUT", 25*time.Second),

		OutgressPremiumSubject:  env.Get("NATS_OUTGRESS_PREMIUM_SUBJECT", "twitch.outgress.premium"),
		OutgressStandardSubject: env.Get("NATS_OUTGRESS_STANDARD_SUBJECT", "twitch.outgress.standard"),
		OutgressSystemSubject:   env.Get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),

		ProjectionLiveSubject: env.Get("NATS_BROADCASTER_LIVE_SUBJECT", "bagel.rpc.broadcaster.live.get"),

		SpecialUserIDs: env.Get("TWITCH_SPECIAL_USER_IDS", ""),

		BotUserID: env.Get("TWITCH_BOT_USER_ID", ""),

		AutomodEnforce: env.Get("SESAME_AUTOMOD_ENFORCE", "false") == "true",
		ShieldEnabled:  env.Get("SESAME_AUTOMOD_SHIELD", "false") == "true",
		EmotesEnabled:  env.Get("SESAME_AUTOMOD_EMOTES", "true") == "true",

		LiveTTL: env.GetDuration("SESAME_LIVE_TTL", 12*time.Hour),

		ProjectionUsersSubject:    env.Get("NATS_INTERNAL_PROJECTION_USERS_SUBJECT", "bagel.rpc.internal.projection.users.get"),
		ProjectionModulesSubject:  env.Get("NATS_INTERNAL_PROJECTION_MODULES_SUBJECT", "bagel.rpc.projector.dashboard.modules.get"),
		ProjectionCommandsSubject: env.Get("NATS_INTERNAL_PROJECTION_COMMANDS_SUBJECT", "bagel.rpc.projector.dashboard.commands.get"),

		CacheInvalidationPrefix: env.Get("NATS_CACHE_INVALIDATION_PREFIX", "bagel.cache.invalidate"),

		CommandsDashboardPrefix: env.Get("NATS_COMMANDS_DASHBOARD_PREFIX", "bagel.rpc.commands"),

		ModulesRPCPrefix: env.Get("NATS_MODULES_SUBJECT_PREFIX", "bagel.rpc.modules"),

		PublicBaseURL: strings.TrimRight(env.Get("SESAME_PUBLIC_BASE_URL", "https://dashboard.itsbagelbot.com"), "/"),

		GatewayRPCPrefix: env.Get("NATS_GATEWAY_SUBJECT_PREFIX", "bagel.rpc.gateway"),

		LoyaltyRPCPrefix: env.Get("NATS_LOYALTY_SUBJECT_PREFIX", "bagel.rpc.loyalty"),

		OutgressRPCPrefix: env.Get("NATS_OUTGRESS_RPC_PREFIX", "bagel.rpc.outgress"),

		ValkeyAddr:     env.Get("VALKEY_ADDR", "127.0.0.1:6379"),
		ValkeyPassword: env.Get("VALKEY_PASSWORD", ""),

		ListenAddr: env.Get("LISTEN_ADDR", ":8080"),
	}
}
