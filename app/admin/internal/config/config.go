// Package config reads the environment once at boot. Every value the service
// needs arrives as an env var. The shard/user RPC plumbing carries no
// credentials, so the manifest sets it directly; the Twitch OAuth secrets for
// the bot-account consent (TWITCH_CLIENT_ID / TWITCH_CLIENT_SECRET) are
// injected from Doppler at runtime and are required.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	ListenAddr string

	NATSURL             string
	AdminSubject        string // request-reply subject answered by Ingress.AdminRpc
	StatusSubjectPrefix string // shard up/down events for the live feed
	UserSubjectPrefix   string // admin user verbs owned by broadcaster-data

	// BaseURL is the admin's own tailnet origin (e.g.
	// https://admin.tail451e6d.ts.net). The bot-account OAuth redirect URI is
	// derived from it, so it must match the redirect registered in the Twitch
	// app console and be reachable from the operator's tailnet browser.
	BaseURL string

	TwitchClientID     string // Doppler, required: the fleet's Twitch app client id
	TwitchClientSecret string // Doppler, required: the fleet's Twitch app secret
	BotScopes          string // space-separated scopes carried by the bot consent

	// BotUserID, when set, is the Twitch user id the bot account MUST resolve
	// to. The callback refuses to store a token whose owner differs, guarding
	// against authenticating the wrong account. Empty disables the check.
	BotUserID string
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
		ListenAddr:          get("ADMIN_LISTEN_ADDR", ":8080"),
		NATSURL:             fmt.Sprintf("nats://%s:%s", get("NATS_HOST", "127.0.0.1"), get("NATS_PORT", "4222")),
		AdminSubject:        get("NATS_ADMIN_SUBJECT", "twitch.ingress.admin.shards.get"),
		StatusSubjectPrefix: get("NATS_STATUS_SUBJECT_PREFIX", "twitch.ingress.status"),
		UserSubjectPrefix:   get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user"),
		BaseURL:             get("ADMIN_BASE_URL", "http://localhost:8080"),
		// Same scopes string the dashboard's bot consent carries, so the bot
		// account is minted with everything the fleet's services expect.
		BotScopes: get("ADMIN_BOT_SCOPES", "channel:bot user:read:chat user:bot channel:read:subscriptions bits:read moderator:read:followers"),
		BotUserID: get("ADMIN_BOT_USER_ID", ""),
	}

	var err error
	if c.TwitchClientID, err = need("TWITCH_CLIENT_ID"); err != nil {
		return nil, err
	}
	if c.TwitchClientSecret, err = need("TWITCH_CLIENT_SECRET"); err != nil {
		return nil, err
	}

	return c, nil
}
