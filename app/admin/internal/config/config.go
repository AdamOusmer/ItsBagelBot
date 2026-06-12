// Package config reads the environment once at boot. Every value the service
// needs arrives as an env var; nothing here is secret (the admin tool carries
// no credentials), so the manifest sets these directly.
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
}

func get(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Load() *Config {
	return &Config{
		ListenAddr:          get("ADMIN_LISTEN_ADDR", ":8080"),
		NATSURL:             fmt.Sprintf("nats://%s:%s", get("NATS_HOST", "127.0.0.1"), get("NATS_PORT", "4222")),
		AdminSubject:        get("NATS_ADMIN_SUBJECT", "twitch.ingress.admin.shards.get"),
		StatusSubjectPrefix: get("NATS_STATUS_SUBJECT_PREFIX", "twitch.ingress.status"),
		UserSubjectPrefix:   get("NATS_ADMIN_USER_SUBJECT_PREFIX", "bagel.rpc.admin.user"),
	}
}
