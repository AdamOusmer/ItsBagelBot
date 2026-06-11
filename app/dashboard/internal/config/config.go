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
	TwitchConduitID    string // EventSub subscriptions are bound to this conduit
	BotScopes          string // space-separated scopes for the bot-enable consent

	DBDSN string

	NATSURL                  string
	BroadcasterStatusSubject string
	StatusSubjectPrefix      string

	AEADKey []byte // 32 bytes, base64 in env
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
		ListenAddr:               get("DASHBOARD_LISTEN_ADDR", ":8080"),
		BaseURL:                  get("DASHBOARD_BASE_URL", "http://localhost:8080"),
		// channel:bot authorizes the bot on the channel; user:read:chat plus
		// user:bot are required on the chatting user for app-token
		// channel.chat.message subscriptions.
		BotScopes:                get("DASHBOARD_BOT_SCOPES", "channel:bot user:read:chat user:bot"),
		BroadcasterStatusSubject: get("NATS_BROADCASTER_STATUS_SUBJECT", "bagel.rpc.broadcaster.status.get"),
		StatusSubjectPrefix:      get("NATS_STATUS_SUBJECT_PREFIX", "twitch.ingress.status"),
	}

	var err error
	if c.TwitchClientID, err = need("TWITCH_CLIENT_ID"); err != nil {
		return nil, err
	}
	if c.TwitchClientSecret, err = need("TWITCH_CLIENT_SECRET"); err != nil {
		return nil, err
	}
	if c.TwitchConduitID, err = need("TWITCH_CONDUIT_ID"); err != nil {
		return nil, err
	}

	c.NATSURL = fmt.Sprintf("nats://%s:%s", get("NATS_HOST", "127.0.0.1"), get("NATS_PORT", "4222"))

	c.DBDSN = fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&tls=%s",
		get("DB_USER", "dashboard_svc"), os.Getenv("DB_PASSWORD"),
		get("DB_HOST", "127.0.0.1"), get("DB_PORT", "3306"),
		get("DB_NAME", "dashboard"),
		// HeatWave presents an OCI-internal CA; skip-verify still encrypts and
		// the user is REQUIRE SSL server-side. Pin a CA via custom config if
		// the fleet ever distributes one.
		get("DB_TLS_MODE", "skip-verify"))

	rawKey, err := need("DASHBOARD_AEAD_KEY")
	if err != nil {
		return nil, err
	}
	c.AEADKey, err = base64.StdEncoding.DecodeString(rawKey)
	if err != nil || len(c.AEADKey) != 32 {
		return nil, fmt.Errorf("DASHBOARD_AEAD_KEY must be base64 of exactly 32 bytes")
	}

	return c, nil
}
