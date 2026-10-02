// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestConnectPrefersTheLeafOverTheGivenURL(t *testing.T) {
	leaf, given := coreServer(t, "leaf"), coreServer(t, "given")

	for _, tc := range []struct {
		name    string
		leafURL string
		want    string
	}{
		{"a configured leaf wins", leaf.ClientURL(), "leaf"},
		{"without a leaf the given URL is used", "", "given"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NATS_LEAF_URL", tc.leafURL)

			nc, err := Connect(given.ClientURL(), "connect-test")

			require.NoError(t, err)
			defer nc.Close()
			assert.Equal(t, tc.want, nc.ConnectedServerName())
		})
	}
}

func TestRPCURLFollowsTheOverride(t *testing.T) {
	t.Setenv("NATS_RPC_URL", "")
	assert.Equal(t, "nats://nats-rpc:4222", RPCURL("nats://nats-rpc:4222"))

	t.Setenv("NATS_RPC_URL", "nats://leaf-rpc:4222")
	assert.Equal(t, "nats://leaf-rpc:4222", RPCURL("nats://nats-rpc:4222"))
}

func TestBusClientsPreferTheHubThenTheLeafThenTheGivenURL(t *testing.T) {
	hub, leaf, given := coreServer(t, "hub"), coreServer(t, "leaf"), coreServer(t, "given")

	for _, tc := range []struct {
		name    string
		hubURL  string
		leafURL string
		want    *struct{ servers [3]int }
	}{
		{"the hub wins when it is set", hub.ClientURL(), leaf.ClientURL(), &struct{ servers [3]int }{[3]int{1, 0, 0}}},
		{"the leaf is used without a hub", "", leaf.ClientURL(), &struct{ servers [3]int }{[3]int{0, 1, 0}}},
		{"the given URL is used without a hub or a leaf", "", "", &struct{ servers [3]int }{[3]int{0, 0, 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NATS_HUB_URL", tc.hubURL)
			t.Setenv("NATS_LEAF_URL", tc.leafURL)
			t.Setenv("NATS_HUB_PUBLISH_URL", "")
			before := [3]int{hub.NumClients(), leaf.NumClients(), given.NumClients()}

			pub, err := NewPublisher(given.ClientURL(), zap.NewNop())
			require.NoError(t, err)
			defer pub.Close()

			connected := [3]int{hub.NumClients() - before[0], leaf.NumClients() - before[1], given.NumClients() - before[2]}
			assert.Equal(t, [3]bool{tc.want.servers[0] > 0, tc.want.servers[1] > 0, tc.want.servers[2] > 0},
				[3]bool{connected[0] > 0, connected[1] > 0, connected[2] > 0})
		})
	}
}

func TestLeafFailbackReconnectsOnlyWhenTheLocalLeafIsHealthyAndNotAlreadyConnected(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer healthy.Close()
	unhealthy := httptest.NewServer(http.NotFoundHandler())
	defer unhealthy.Close()

	for _, tc := range []struct {
		name       string
		serverName string
		healthURL  string
		reconnects bool
	}{
		{"a remote connection with a healthy local leaf fails back", "remote", healthy.URL, true},
		{"an unready local leaf keeps the connection", "remote", unhealthy.URL, false},
		{"an unreachable local leaf keeps the connection", "remote", "http://127.0.0.1:1/healthz", false},
		{"a connection already on the local leaf stays put", "node1--nats-leaf-abc", healthy.URL, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := coreServer(t, tc.serverName)
			t.Setenv("NODE_NAME", "node1")
			t.Setenv("NATS_LOCAL_LEAF_HEALTH_URL", tc.healthURL)
			t.Setenv("NATS_FAILBACK_INTERVAL", "30ms")
			t.Setenv("NATS_FAILBACK_SUCCESSES", "2")
			t.Setenv("NATS_FAILBACK_PROBE_TIMEOUT", "50ms")
			t.Setenv("NATS_LEAF_URL", "")
			nc, err := Connect(s.ClientURL(), "failback-test")
			require.NoError(t, err)
			defer nc.Close()

			deadline := time.Now().Add(500 * time.Millisecond)
			for nc.Stats().Reconnects == 0 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}

			assert.Equal(t, tc.reconnects, nc.Stats().Reconnects > 0)
		})
	}
}
