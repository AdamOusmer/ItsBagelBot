// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGeneratedFilesBootAnOperatorModeServer(t *testing.T) {
	h := newHarness(t, "")
	h.apply(t)
	url := startOperatorServer(t, h.cfg)

	t.Run("BusRoleUserPublishesAndSubscribes", func(t *testing.T) {
		nc := connectCreds(t, url, h.secret("users", "NATS_JWT"), h.secret("users", "NATS_NKEY_SEED"))
		sub, err := nc.SubscribeSync("data.users.probe")
		require.NoError(t, err)
		require.NoError(t, nc.Publish("data.users.probe", []byte("ok")))

		_, err = sub.NextMsg(2 * time.Second)

		assert.NoError(t, err, "users_bus must receive its own subject")
	})

	t.Run("DeployerSysUserLooksUpPreloadedClaims", func(t *testing.T) {
		nc := connectCreds(t, url, h.secret(deployerProject, deploySysJWTKey), h.secret(deployerProject, deploySysSeedKey))
		subject := fmt.Sprintf("$SYS.REQ.ACCOUNT.%s.CLAIMS.LOOKUP", h.keys(t).Accounts["BUS"])

		msg, err := nc.Request(subject, nil, 2*time.Second)

		require.NoError(t, err)
		assert.NotEmpty(t, msg.Data)
	})
}

var preloadAccountEntry = regexp.MustCompile(`(?m)^\s*(A[A-Z2-7]{55}):\s*"([^"]+)"`)

func preloadedExports(t *testing.T, path, accountPub string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	for _, entry := range preloadAccountEntry.FindAllStringSubmatch(string(data), -1) {
		if entry[1] == accountPub {
			claims, err := jwt.DecodeAccountClaims(entry[2])
			require.NoError(t, err)
			return len(claims.Exports)
		}
	}
	return -1
}

func TestPreloadRewritesOnlyWhenAnAccountChanges(t *testing.T) {
	const exportingBus = "system_account: SYS\naccounts:\n  SYS: {}\n  BUS:\n    exports:\n      - service: bagel.rpc.probe\n"
	h := newHarness(t, systemACL+"  BUS: {}\n")
	h.apply(t)
	bus := h.keys(t).Accounts["BUS"]
	h.backdatePublicFiles(t)
	before := h.publicFiles(t)

	h.apply(t)
	assert.Equal(t, before, h.publicFiles(t), "unchanged accounts must not re-sign the preload")
	assert.Zero(t, preloadedExports(t, h.cfg.PreloadPath, bus))

	h.setACL(t, exportingBus)
	h.apply(t)

	assert.Equal(t, 1, preloadedExports(t, h.cfg.PreloadPath, bus), "the changed account must be re-signed with its new export")
}

func startOperatorServer(t *testing.T, cfg Config) string {
	t.Helper()
	dir := filepath.Dir(cfg.PreloadPath)
	conf := fmt.Sprintf("listen: \"127.0.0.1:-1\"\noperator: %q\nresolver: {type: full, dir: %q}\ninclude %q\n",
		cfg.OperatorJWTPath, filepath.Join(dir, "resolver"), filepath.Base(cfg.PreloadPath))
	confPath := filepath.Join(dir, "server.conf")
	require.NoError(t, os.WriteFile(confPath, []byte(conf), 0o600))
	opts, err := server.ProcessConfigFile(confPath)
	require.NoError(t, err, "parse operator-mode config")
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	require.NoError(t, err, "start operator-mode server")
	go s.Start()
	t.Cleanup(s.Shutdown)
	require.True(t, s.ReadyForConnections(5*time.Second), "operator-mode server not ready")
	return s.ClientURL()
}

func connectCreds(t *testing.T, url, userJWT, seed string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url, nats.UserJWTAndSeed(userJWT, seed), nats.Timeout(2*time.Second))
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}
