// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsclaims

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/deployer/internal/ports"
)

const clusterConfigTmpl = `
listen: 127.0.0.1:-1
server_name: %s
operator: %s
system_account: %s
resolver: {
	type: full
	dir: '%s'
}
cluster {
	name: clust
	listen: 127.0.0.1:-1
	no_advertise: true
	%s
}
`

type opChain struct {
	opKp   nkeys.KeyPair
	opJWT  string
	sysKp  nkeys.KeyPair
	sysPub string
}

func publicKey(t *testing.T, kp nkeys.KeyPair) string {
	t.Helper()
	pub, err := kp.PublicKey()
	require.NoError(t, err)
	return pub
}

func accountJWT(t *testing.T, signer nkeys.KeyPair, pub, name string, edit func(*jwt.AccountClaims)) string {
	t.Helper()
	claims := jwt.NewAccountClaims(pub)
	claims.Name = name
	if edit != nil {
		edit(claims)
	}
	token, err := claims.Encode(signer)
	require.NoError(t, err)
	return token
}

func buildOpChain(t *testing.T) opChain {
	t.Helper()
	opKp, err := nkeys.CreateOperator()
	require.NoError(t, err)
	opJWT, err := jwt.NewOperatorClaims(publicKey(t, opKp)).Encode(opKp)
	require.NoError(t, err)
	sysKp, err := nkeys.CreateAccount()
	require.NoError(t, err)
	return opChain{opKp: opKp, opJWT: opJWT, sysKp: sysKp, sysPub: publicKey(t, sysKp)}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

type nodeSpec struct {
	name    string
	seedDir string
	routes  string
}

func startNode(t *testing.T, chain opChain, spec nodeSpec) *server.Server {
	t.Helper()
	dir := t.TempDir()
	if spec.seedDir != "" {
		copyDir(t, spec.seedDir, dir)
	}
	cfgPath := filepath.Join(t.TempDir(), spec.name+".conf")
	writeFile(t, cfgPath, fmt.Sprintf(clusterConfigTmpl, spec.name, chain.opJWT, chain.sysPub, dir, spec.routes))
	opts, err := server.ProcessConfigFile(cfgPath)
	require.NoError(t, err)
	opts.NoLog, opts.NoSigs = true, true
	srv, err := server.NewServer(opts)
	require.NoError(t, err)
	srv.Start()
	t.Cleanup(srv.Shutdown)
	require.True(t, srv.ReadyForConnections(5*time.Second), "server %s never became ready", spec.name)
	return srv
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		require.NoError(t, err)
		writeFile(t, filepath.Join(dst, e.Name()), string(data))
	}
}

// Route pooling means NumRoutes() is a multiple of peer count, not the peer
// count itself, so "at least one route per peer" is what formation means.
func converged(s *server.Server, peers int, accountPub string) bool {
	_, err := s.LookupAccount(accountPub)
	return s.NumRoutes() >= peers && err == nil
}

func waitConverged(t *testing.T, accountPub string, servers ...*server.Server) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, s := range servers {
			if !converged(s, len(servers)-1, accountPub) {
				return false
			}
		}
		return true
	}, 10*time.Second, 50*time.Millisecond, "cluster did not form and sync the account in time")
}

func clearNATSTLSEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NATS_CA_PEM", "")
	t.Setenv("NATS_CLIENT_CERT_FILE", "")
	t.Setenv("NATS_CLIENT_KEY_FILE", "")
}

func startCluster(t *testing.T, chain opChain, accountPub, accountToken string) *server.Server {
	t.Helper()
	seed := t.TempDir()
	writeFile(t, filepath.Join(seed, accountPub+".jwt"), accountToken)

	srvA := startNode(t, chain, nodeSpec{name: "srv-A", seedDir: seed})
	route := fmt.Sprintf("routes: [nats-route://127.0.0.1:%d]", srvA.ClusterAddr().Port)
	srvB := startNode(t, chain, nodeSpec{name: "srv-B", routes: route})
	srvC := startNode(t, chain, nodeSpec{name: "srv-C", routes: route})
	waitConverged(t, accountPub, srvA, srvB, srvC)
	return srvA
}

func connectAdapter(t *testing.T, url string, chain opChain) *Adapter {
	t.Helper()
	sysUserKp, err := nkeys.CreateUser()
	require.NoError(t, err)
	sysUserSeed, err := sysUserKp.Seed()
	require.NoError(t, err)
	sysUserJWT, err := jwt.NewUserClaims(publicKey(t, sysUserKp)).Encode(chain.sysKp)
	require.NoError(t, err)

	adapter, err := New(Config{HubURL: url, LeafURL: url, SysJWT: sysUserJWT, SysSeed: string(sysUserSeed)})
	require.NoError(t, err)
	t.Cleanup(adapter.Close)
	return adapter
}

func TestAdapterAgainstEmbeddedCluster(t *testing.T) {
	clearNATSTLSEnv(t)
	chain := buildOpChain(t)
	accountKp, err := nkeys.CreateAccount()
	require.NoError(t, err)
	accountPub := publicKey(t, accountKp)
	srvA := startCluster(t, chain, accountPub, accountJWT(t, chain.opKp, accountPub, "TESTACC", nil))
	adapter := connectAdapter(t, srvA.ClientURL(), chain)

	t.Run("Servers counts every node in the cluster", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		names, err := adapter.Servers(ctx, ports.ClusterHub)
		require.NoError(t, err)
		assert.Len(t, names, 3)
	})

	t.Run("Push reaches every server with code 200 and Lookup returns the pushed claims", func(t *testing.T) {
		pushJWT := accountJWT(t, chain.opKp, accountPub, "TESTACC", func(c *jwt.AccountClaims) {
			c.Exports = jwt.Exports{{Subject: "svc.>", Type: jwt.Service}}
		})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		replies, err := adapter.Push(ctx, ports.ClusterHub, pushJWT, 3)
		require.NoError(t, err)
		require.Len(t, replies, 3)
		for _, r := range replies {
			assert.Equal(t, 200, r.Code, "reply from %s: %s", r.Server, r.Message)
			assert.NotEmpty(t, r.Server)
		}

		live, err := adapter.Lookup(ctx, ports.ClusterHub, accountPub)
		require.NoError(t, err)
		decoded, err := jwt.DecodeAccountClaims(live)
		require.NoError(t, err)
		assert.Len(t, decoded.Exports, 1, "Lookup returns the pushed export")
	})
}
