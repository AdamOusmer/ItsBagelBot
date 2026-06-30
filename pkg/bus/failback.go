package bus

import (
	"math/rand/v2"
	"net"
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
	localAddr string
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
		localAddr: env.Get("NATS_LOCAL_LEAF_ADDR", "nats-leaf-local:4222"),
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

		if !localLeafReady(cfg.localAddr, cfg.timeout) {
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

func localLeafReady(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
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
