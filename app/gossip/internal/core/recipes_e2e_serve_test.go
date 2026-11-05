// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package core_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"testing"

	"ItsBagelBot/app/gossip/internal/core"
	"ItsBagelBot/app/gossip/internal/engine"
	"ItsBagelBot/app/gossip/internal/provider"
	"ItsBagelBot/app/gossip/internal/providers/custom"
	"ItsBagelBot/app/gossip/internal/providers/govee"
	"ItsBagelBot/pkg/ratelimit"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

// Started by app/twitch/sesame/e2e as a separate process, since gossip's packages are internal to it.
func TestRecipesE2EServe(t *testing.T) {
	natsURL := os.Getenv("RECIPES_E2E_NATS_URL")
	if natsURL == "" {
		t.Skip("RECIPES_E2E_NATS_URL is not set")
	}
	core.SetSSRFCheckForTests(false)
	pool := x509.NewCertPool()
	require.True(t, pool.AppendCertsFromPEM([]byte(os.Getenv("RECIPES_E2E_UPSTREAM_CERT"))))
	core.TrustWARPUpstreamForE2E(&tls.Config{RootCAs: pool, ServerName: os.Getenv("RECIPES_E2E_UPSTREAM_SNI"), MinVersion: tls.VersionTLS12})
	core.SetWARPProxyAddrForTests(startPinnedSOCKS(t, os.Getenv("RECIPES_E2E_UPSTREAM_HOST"), os.Getenv("RECIPES_E2E_UPSTREAM_ADDR")))

	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	defer nc.Close()
	vc, err := valkey.NewClient(valkey.ClientOption{InitAddress: []string{os.Getenv("VALKEY_TEST_ADDR")}, DisableCache: true})
	require.NoError(t, err)
	defer vc.Close()

	deps := provider.Deps{Cache: core.NewCache(core.NewValkeyStore(vc)), Limiter: ratelimit.New(vc), Log: zap.NewNop()}
	// Above the default of 6 a minute: the e2e fires more runs in a few seconds than a channel's budget allows.
	active := []provider.Provider{custom.New(custom.Config{ChannelRateLimit: 120, DefRateLimit: 120, HostRateLimit: 120}, deps)}
	if base := os.Getenv("RECIPES_E2E_GOVEE_URL"); base != "" {
		deps.GoveeKeys = staticGoveeKey{}
		active = append(active, govee.New(govee.Config{BaseURL: base}, deps))
	}
	require.NoError(t, engine.Serve(nc, os.Getenv("RECIPES_E2E_GOSSIP_PREFIX"), "gossip-e2e", active, nil, zap.NewNop()))
	fmt.Println("RECIPES_E2E_READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

type staticGoveeKey struct{}

func (staticGoveeKey) Key(context.Context, string) (string, error) { return "e2e-govee-key", nil }

// Stands in for the WARP sidecar: only the named host is reachable, and it lands on the local upstream.
func startPinnedSOCKS(t *testing.T, host, upstream string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go pipeSOCKS(conn, host, upstream)
		}
	}()
	return ln.Addr().String()
}

func pipeSOCKS(conn net.Conn, host, upstream string) {
	defer conn.Close()
	target, err := readSOCKSRequest(conn)
	if err != nil {
		return
	}
	if target != net.JoinHostPort(host, "443") {
		_, _ = conn.Write([]byte{5, 2, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	remote, err := net.Dial("tcp", upstream)
	if err != nil {
		_, _ = conn.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer remote.Close()
	if _, err := conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}); err != nil {
		return
	}
	go func() { _, _ = io.Copy(remote, conn) }()
	_, _ = io.Copy(conn, remote)
}

func readSOCKSRequest(conn net.Conn) (string, error) {
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(conn, greeting); err != nil {
		return "", err
	}
	if _, err := io.CopyN(io.Discard, conn, int64(greeting[1])); err != nil {
		return "", err
	}
	if _, err := conn.Write([]byte{5, 0}); err != nil {
		return "", err
	}
	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return "", err
	}
	name, err := readSOCKSHost(conn, head[3])
	if err != nil {
		return "", err
	}
	port := make([]byte, 2)
	if _, err := io.ReadFull(conn, port); err != nil {
		return "", err
	}
	return net.JoinHostPort(name, strconv.Itoa(int(binary.BigEndian.Uint16(port)))), nil
}

func readSOCKSHost(conn net.Conn, addressType byte) (string, error) {
	const domainName = 3
	if addressType != domainName {
		return "", fmt.Errorf("address type %d is not a host name", addressType)
	}
	length := make([]byte, 1)
	if _, err := io.ReadFull(conn, length); err != nil {
		return "", err
	}
	name := make([]byte, length[0])
	_, err := io.ReadFull(conn, name)
	return string(name), err
}
