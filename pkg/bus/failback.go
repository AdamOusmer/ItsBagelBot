// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"crypto/tls"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"

	"ItsBagelBot/pkg/env"
)

const (
	defaultFailbackInterval  = 30 * time.Second
	defaultFailbackSuccesses = 3
	defaultFailbackTimeout   = time.Second
	maxFailbackBackoffShift  = 4
)

type failbackConfig struct {
	nodeName    string
	healthURL   string
	localHubURL string
	interval    time.Duration
	successes   int
	timeout     time.Duration
}

type locality int

const (
	localityUnknown locality = iota
	localityLocal
	localityDisplaced
)

func leafFailbackOption() nats.Option {
	cfg := loadFailbackConfig()
	if cfg.nodeName == "" {
		return nil
	}
	return nats.ConnectHandler(func(nc *nats.Conn) {
		go runFailback(nc, cfg, func(nc *nats.Conn) locality { return leafLocality(nc, cfg) })
	})
}

func hubFailbackOption(identity connectionIdentity) nats.Option {
	cfg := loadFailbackConfig()
	if cfg.nodeName == "" || env.Get("NATS_HUB_URL", "") == "" {
		return nil
	}
	return nats.ConnectHandler(func(nc *nats.Conn) {
		go runFailback(nc, cfg, func(nc *nats.Conn) locality { return localHub.locality(nc, cfg, identity) })
	})
}

func loadFailbackConfig() failbackConfig {
	return failbackConfig{
		nodeName: env.Get("NODE_NAME", ""),
		// Keep the .messaging qualifier: other namespaces cannot resolve the short name.
		healthURL:   env.Get("NATS_LOCAL_LEAF_HEALTH_URL", "https://nats-leaf-local.messaging:8222/healthz"),
		localHubURL: env.Get("NATS_LOCAL_HUB_URL", "tls://nats-local.messaging:4222"),
		interval:    durationEnv("NATS_FAILBACK_INTERVAL", defaultFailbackInterval),
		successes:   positiveIntEnv("NATS_FAILBACK_SUCCESSES", defaultFailbackSuccesses),
		timeout:     durationEnv("NATS_FAILBACK_PROBE_TIMEOUT", defaultFailbackTimeout),
	}
}

func runFailback(nc *nats.Conn, cfg failbackConfig, place func(*nats.Conn) locality) {
	initial := time.NewTimer(time.Duration(rand.Int64N(int64(cfg.interval))))
	defer initial.Stop()
	<-initial.C

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	consecutive, failbacks := 0, 0
	for range ticker.C {
		if nc.IsClosed() {
			return
		}
		if nc.Status() != nats.CONNECTED {
			consecutive = 0
			continue
		}

		switch place(nc) {
		case localityLocal:
			consecutive, failbacks = 0, 0
		case localityUnknown:
			consecutive = 0
		case localityDisplaced:
			consecutive++
			// A failback that lands remote again backs off instead of recycling every few intervals.
			if consecutive < cfg.successes<<min(failbacks, maxFailbackBackoffShift) {
				continue
			}
			consecutive = 0
			failbacks++
			_ = nc.ForceReconnect()
		}
	}
}

func leafLocality(nc *nats.Conn, cfg failbackConfig) locality {
	if isLocalLeaf(nc.ConnectedServerName(), cfg.nodeName) {
		return localityLocal
	}
	if !localLeafReady(cfg.healthURL, cfg.timeout) {
		return localityUnknown
	}
	return localityDisplaced
}

func isLocalLeaf(serverName, nodeName string) bool {
	return nodeName != "" && strings.HasPrefix(serverName, nodeName+"--")
}

func localLeafReady(healthURL string, timeout time.Duration) bool {
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		return false
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: natsCAPool(), MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	resp, err := (&http.Client{Timeout: timeout, Transport: transport}).Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

var localHub = &hubProbe{}

type hubProbe struct {
	mu      sync.Mutex
	name    string
	checked time.Time
}

func (h *hubProbe) locality(nc *nats.Conn, cfg failbackConfig, identity connectionIdentity) locality {
	connected := nc.ConnectedServerName()
	name := h.lookup(connected, cfg, identity)
	switch {
	case name == "":
		return localityUnknown
	case name == connected:
		return localityLocal
	default:
		return localityDisplaced
	}
}

// The hub's monitor port stays closed to app namespaces, so locality is read from a client handshake.
func (h *hubProbe) lookup(connected string, cfg failbackConfig, identity connectionIdentity) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if connected != "" && connected == h.name {
		return h.name
	}
	if time.Since(h.checked) < cfg.interval {
		return h.name
	}

	h.checked = time.Now()
	h.name = ""
	identity.name = "hub-failback-probe"
	opts := append(baseOptions(identity), nats.NoReconnect(), nats.RetryOnFailedConnect(false), nats.Timeout(cfg.timeout))
	probe, err := nats.Connect(cfg.localHubURL, opts...)
	if err != nil {
		return ""
	}
	h.name = probe.ConnectedServerName()
	probe.Close()
	return h.name
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
