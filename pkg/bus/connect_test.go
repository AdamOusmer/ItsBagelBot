// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
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

func TestOnlyTheRPCPlaneKeepsAnOrderedPoolAndBothPlanesIgnoreDiscoveredServers(t *testing.T) {
	t.Setenv("NATS_CA_PEM", "")

	for _, tc := range []struct {
		name             string
		options          []nats.Option
		ordered          bool
		ignoreDiscovered bool
	}{
		{"the RPC plane walks leaf then hub and dials only configured servers", rpcOptions("rpc-plane"), true, true},
		{"the BUS plane shuffles and dials only the hub Service", busOptions("bus-plane"), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := nats.GetDefaultOptions()
			for _, apply := range tc.options {
				require.NoError(t, apply(&opts))
			}

			assert.Equal(t, tc.ordered, opts.NoRandomize)
			assert.Equal(t, tc.ignoreDiscovered, opts.IgnoreDiscoveredServers)
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

func TestHubFailbackReconnectsOnlyAwayFromARemoteHubWhileTheLocalHubAnswers(t *testing.T) {
	remote, local := coreServer(t, "nats-0"), coreServer(t, "nats-1")

	for _, tc := range []struct {
		name       string
		connectTo  string
		localURL   string
		hubURL     string
		nodeName   string
		reconnects bool
	}{
		{"a connection on a remote hub fails back", remote.ClientURL(), local.ClientURL(), "set", "node1", true},
		{"a connection already on the local hub stays put", local.ClientURL(), local.ClientURL(), "set", "node1", false},
		{"an unreachable local hub keeps the connection", remote.ClientURL(), "nats://127.0.0.1:1", "set", "node1", false},
		{"without a node name the hub plane never fails back", remote.ClientURL(), local.ClientURL(), "set", "", false},
		{"a connection that does not dial the hub never fails back", remote.ClientURL(), local.ClientURL(), "", "node1", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			localHub = &hubProbe{}
			t.Setenv("NATS_CA_PEM", "")
			t.Setenv("NODE_NAME", tc.nodeName)
			t.Setenv("NATS_HUB_URL", tc.hubURL)
			t.Setenv("NATS_LOCAL_HUB_URL", tc.localURL)
			t.Setenv("NATS_FAILBACK_INTERVAL", "30ms")
			t.Setenv("NATS_FAILBACK_SUCCESSES", "2")
			t.Setenv("NATS_FAILBACK_PROBE_TIMEOUT", "50ms")
			nc, err := nats.Connect(tc.connectTo, busOptions("hub-failback-test")...)
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

func TestHubFailbackBacksOffWhenReconnectsKeepLandingRemote(t *testing.T) {
	remote, local := coreServer(t, "nats-0"), coreServer(t, "nats-1")
	localHub = &hubProbe{}
	t.Setenv("NATS_CA_PEM", "")
	t.Setenv("NODE_NAME", "node1")
	t.Setenv("NATS_HUB_URL", "set")
	t.Setenv("NATS_LOCAL_HUB_URL", local.ClientURL())
	t.Setenv("NATS_FAILBACK_INTERVAL", "20ms")
	t.Setenv("NATS_FAILBACK_SUCCESSES", "1")
	t.Setenv("NATS_FAILBACK_PROBE_TIMEOUT", "50ms")
	nc, err := nats.Connect(remote.ClientURL(), busOptions("hub-failback-backoff-test")...)
	require.NoError(t, err)
	defer nc.Close()

	time.Sleep(700 * time.Millisecond)

	// 35 ticks: a fixed cadence would recycle ~35 times, the doubling backoff recycles at ticks 1, 3, 7, 15, 31.
	reconnects := nc.Stats().Reconnects
	assert.GreaterOrEqual(t, reconnects, uint64(2))
	assert.LessOrEqual(t, reconnects, uint64(6))
}

func TestLocalLeafProbeVerifiesTheLeafAgainstTheFleetCA(t *testing.T) {
	leaf := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer leaf.Close()
	fleetCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Certificate().Raw}))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "other"},
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true,
	}, &x509.Certificate{SerialNumber: big.NewInt(1)}, &key.PublicKey, key)
	require.NoError(t, err)
	otherPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))

	for _, tc := range []struct {
		name  string
		caPEM string
		want  bool
	}{
		{"a leaf signed by the fleet CA is ready", fleetCA, true},
		{"a leaf outside the fleet CA is not ready", otherPEM, false},
		{"without a fleet CA the leaf is not ready", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("NATS_CA_PEM", tc.caPEM)

			assert.Equal(t, tc.want, localLeafReady(leaf.URL, time.Second))
		})
	}
}
