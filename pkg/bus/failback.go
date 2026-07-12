package bus

import (
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/pkg/env"
)

const (
	defaultFailbackInterval  = 30 * time.Second
	defaultFailbackSuccesses = 3
	defaultFailbackTimeout   = time.Second
)

type failbackConfig struct {
	nodeName  string
	healthURL string
	interval  time.Duration
	successes int
	timeout   time.Duration
}

func leafFailbackOption() nats.Option {
	cfg := loadFailbackConfig()
	if cfg.nodeName == "" {
		return nil // Local development and tests do not need topology failback.
	}
	return nats.ConnectHandler(func(nc *nats.Conn) {
		go runLeafFailback(nc, cfg)
	})
}

func loadFailbackConfig() failbackConfig {
	return failbackConfig{
		nodeName:  env.Get("NODE_NAME", ""),
		healthURL: env.Get("NATS_LOCAL_LEAF_HEALTH_URL", "http://nats-leaf-local:8222/healthz"),
		interval:  durationEnv("NATS_FAILBACK_INTERVAL", defaultFailbackInterval),
		successes: positiveIntEnv("NATS_FAILBACK_SUCCESSES", defaultFailbackSuccesses),
		timeout:   durationEnv("NATS_FAILBACK_PROBE_TIMEOUT", defaultFailbackTimeout),
	}
}

func runLeafFailback(nc *nats.Conn, cfg failbackConfig) {
	// Spread checks from replicas that were started by the same rollout.
	initial := time.NewTimer(time.Duration(rand.Int64N(int64(cfg.interval))))
	defer initial.Stop()
	<-initial.C

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	consecutive := 0
	for range ticker.C {
		if nc.IsClosed() {
			return
		}
		if nc.Status() != nats.CONNECTED || isLocalLeaf(nc.ConnectedServerName(), cfg.nodeName) {
			consecutive = 0
			continue
		}

		if !localLeafReady(cfg.healthURL, cfg.timeout) {
			consecutive = 0
			continue
		}
		consecutive++
		if consecutive < cfg.successes {
			continue
		}

		// ForceReconnect preserves subscriptions and buffers new publishes using
		// the normal NATS reconnect machinery. Reset first so a slow reconnect
		// cannot trigger repeatedly.
		consecutive = 0
		_ = nc.ForceReconnect()
	}
}

func isLocalLeaf(serverName, nodeName string) bool {
	return nodeName != "" && strings.HasPrefix(serverName, nodeName+"--")
}

func localLeafReady(healthURL string, timeout time.Duration) bool {
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := env.Get(key, "")
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func positiveIntEnv(key string, fallback int) int {
	v := env.Get(key, "")
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
