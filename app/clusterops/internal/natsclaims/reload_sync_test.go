// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package natsclaims

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"ItsBagelBot/app/clusterops/internal/aclpush"
)

type accountPair struct {
	exporter, importer nkeys.KeyPair
}

func newAccountPair(t *testing.T) accountPair {
	t.Helper()
	exporter, err := nkeys.CreateAccount()
	require.NoError(t, err)
	importer, err := nkeys.CreateAccount()
	require.NoError(t, err)
	return accountPair{exporter: exporter, importer: importer}
}

func (p accountPair) claims(t *testing.T, subjects ...string) []*jwt.AccountClaims {
	t.Helper()
	exporterPub, importerPub := publicKey(t, p.exporter), publicKey(t, p.importer)
	exporter, importer := jwt.NewAccountClaims(exporterPub), jwt.NewAccountClaims(importerPub)
	exporter.Name, importer.Name = "EXPORTER", "IMPORTER"
	for _, s := range subjects {
		exporter.Exports.Add(&jwt.Export{Subject: jwt.Subject(s), Type: jwt.Service})
		importer.Imports.Add(&jwt.Import{Subject: jwt.Subject(s), Account: exporterPub, Type: jwt.Service})
	}
	return []*jwt.AccountClaims{exporter, importer}
}

func preloadConf(t *testing.T, chain opChain, claims []*jwt.AccountClaims) string {
	t.Helper()
	var conf strings.Builder
	conf.WriteString("resolver_preload: {\n")
	for _, c := range append(claims, jwt.NewAccountClaims(chain.sysPub)) {
		token, err := c.Encode(chain.opKp)
		require.NoError(t, err)
		fmt.Fprintf(&conf, "  %s: %q\n", c.Subject, token)
	}
	conf.WriteString("}\n")
	return conf.String()
}

type reloadableServer struct {
	*server.Server
	accountsPath string
}

func (s reloadableServer) rollOut(t *testing.T, accounts string) {
	t.Helper()
	writeFile(t, s.accountsPath, accounts)
	require.NoError(t, s.Reload())
}

func startPreloaded(t *testing.T, chain opChain, accounts string) reloadableServer {
	t.Helper()
	dir := t.TempDir()
	accountsPath := filepath.Join(dir, "accounts.conf")
	writeFile(t, accountsPath, accounts)
	cfgPath := filepath.Join(dir, "server.conf")
	writeFile(t, cfgPath, fmt.Sprintf(clusterConfigTmpl, "srv", chain.opJWT, chain.sysPub, filepath.Join(dir, "resolver"), "")+"include \"accounts.conf\"\n")
	opts, err := server.ProcessConfigFile(cfgPath)
	require.NoError(t, err)
	opts.NoLog, opts.NoSigs = true, true
	srv, err := server.NewServer(opts)
	require.NoError(t, err)
	srv.Start()
	t.Cleanup(srv.Shutdown)
	require.True(t, srv.ReadyForConnections(5*time.Second))
	return reloadableServer{Server: srv, accountsPath: accountsPath}
}

func connectAccount(t *testing.T, srv reloadableServer, account nkeys.KeyPair) *nats.Conn {
	t.Helper()
	user, err := nkeys.CreateUser()
	require.NoError(t, err)
	seed, err := user.Seed()
	require.NoError(t, err)
	token, err := jwt.NewUserClaims(publicKey(t, user)).Encode(account)
	require.NoError(t, err)
	nc, err := nats.Connect(srv.ClientURL(), nats.UserJWTAndSeed(token, string(seed)))
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

func runningExports(t *testing.T, srv reloadableServer, account nkeys.KeyPair) int {
	t.Helper()
	got, err := srv.Accountz(&server.AccountzOptions{Account: publicKey(t, account)})
	require.NoError(t, err)
	require.NotNil(t, got.Account)
	return len(got.Account.Claim.Exports)
}

// A config reload rewrites the resolver store but leaves running accounts on old claims.
func TestSyncLandsClaimsAReloadOnlyStored(t *testing.T) {
	clearNATSTLSEnv(t)
	chain := buildOpChain(t)
	pair := newAccountPair(t)
	initial := preloadConf(t, chain, pair.claims(t, "rpc.one"))
	hub, leaf := startPreloaded(t, chain, initial), startPreloaded(t, chain, initial)

	service := connectAccount(t, leaf, pair.exporter)
	_, err := service.Subscribe("rpc.>", func(m *nats.Msg) { _ = m.Respond(nil) })
	require.NoError(t, err)
	require.NoError(t, service.Flush())
	client := connectAccount(t, leaf, pair.importer)
	_, err = client.Request("rpc.one", nil, time.Second)
	require.NoError(t, err)

	// iat has one second resolution and the store only takes newer tokens.
	time.Sleep(1100 * time.Millisecond)
	compiled := pair.claims(t, "rpc.one", "rpc.two")
	updated := preloadConf(t, chain, compiled)
	hub.rollOut(t, updated)
	leaf.rollOut(t, updated)
	_, err = client.Request("rpc.two", nil, 300*time.Millisecond)
	require.ErrorIs(t, err, nats.ErrNoResponders, "the reload must not have reached the running account for this test to prove anything")
	require.Equal(t, 1, runningExports(t, leaf, pair.exporter))

	adapter := adapterFor(t, Config{HubURL: hub.ClientURL(), LeafURL: leaf.ClientURL()}, chain)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	syncer := aclpush.NewSyncer(adapter, chain.opKp)

	first, err := syncer.Sync(ctx, compiled)
	require.NoError(t, err)
	assert.NotEmpty(t, first)
	assert.Equal(t, 2, runningExports(t, leaf, pair.exporter))
	_, err = client.Request("rpc.two", nil, time.Second)
	require.NoError(t, err, "an open connection reaches the import after sync")

	second, err := syncer.Sync(ctx, compiled)
	require.NoError(t, err)
	assert.Empty(t, second, "a settled cluster takes no further pushes")
}
