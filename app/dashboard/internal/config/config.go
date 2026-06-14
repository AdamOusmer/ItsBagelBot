// Package config reads the environment once at boot. Every value the service
// needs arrives as an env var, populated from Doppler in all environments.
package config

import (
	"encoding/base64"
	"fmt"
	"os"
)

type Config struct {
	ListenAddr string
	BaseURL    string // public origin, e.g. https://dash.itsbagelbot.com

	TwitchClientID     string
	TwitchClientSecret string
	BotScopes          string // broadcaster-side scopes for the bot-enable consent (channel:bot lets the bot act in the channel; the read scopes unlock subs/cheers/follows events)

	DashboardRPCPrefix       string
	CommandsRPCPrefix        string
	CacheInvalidationSubject string
	OutgressSystemSubject    string // lane carrying EventSub on/off jobs to outgress

	NATSURL                  string
	BroadcasterStatusSubject string
	StatusSubjectPrefix      string

	AEADKey    []byte // 32 bytes, base64 in env for DB encryption
	SessionKey []byte // 32 bytes, base64 in env for Session encryption
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func need(key string) (string, error) {
	if v := os.Getenv(key); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("missing required env %s", key)
}

func Load() (*Config, error) {
	c := &Config{
		ListenAddr: get("DASHBOARD_LISTEN_ADDR", ":8080"),
		BaseURL:    get("DASHBOARD_BASE_URL", "http://localhost:8080"),
		// Broadcaster-side consent only: channel:bot authorizes the bot to act
		// on the channel (so the app-token channel.chat.message subscription,
		// read in the bot's user context, is valid here), and the read scopes
		// unlock the broadcaster events (subs, cheers, follows) the bot
		// receives. The bot's own identity scopes (user:read:chat, user:bot,
		// user:write:chat) live in the bot-account consent, not here.
		BotScopes:                get("DASHBOARD_BOT_SCOPES", "channel:bot channel:read:subscriptions bits:read moderator:read:followers"),
		BroadcasterStatusSubject: get("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
		StatusSubjectPrefix:      get("NATS_STATUS_SUBJECT_PREFIX", "twitch.ingress.status"),
		DashboardRPCPrefix:       get("NATS_DASHBOARD_SUBJECT_PREFIX", "bagel.rpc.dashboard"),
		CommandsRPCPrefix:        get("NATS_COMMANDS_SUBJECT_PREFIX", "bagel.rpc.commands"),
		CacheInvalidationSubject: get("NATS_CACHE_INVALIDATION_SUBJECT", "bagel.cache.invalidate.broadcaster"),
		OutgressSystemSubject:    get("NATS_OUTGRESS_SYSTEM_SUBJECT", "twitch.outgress.system"),
	}

	var err error
	if c.TwitchClientID, err = need("TWITCH_CLIENT_ID"); err != nil {
		return nil, err
	}
	if c.TwitchClientSecret, err = need("TWITCH_CLIENT_SECRET"); err != nil {
		return nil, err
	}

	c.NATSURL = fmt.Sprintf("nats://%s:%s", get("NATS_HOST", "127.0.0.1"), get("NATS_PORT", "4222"))

	rawKey, err := need("DASHBOARD_AEAD_KEY")
	if err != nil {
		return nil, err
	}
	c.AEADKey, err = base64.StdEncoding.DecodeString(rawKey)
	if err != nil || len(c.AEADKey) != 32 {
		return nil, fmt.Errorf("DASHBOARD_AEAD_KEY must be base64 of exactly 32 bytes")
	}

	rawSession, err := need("DASHBOARD_SESSION_KEY")
	if err != nil {
		return nil, err
	}
	c.SessionKey, err = base64.StdEncoding.DecodeString(rawSession)
	if err != nil || len(c.SessionKey) != 32 {
		return nil, fmt.Errorf("DASHBOARD_SESSION_KEY must be base64 of exactly 32 bytes")
	}

	return c, nil
}
