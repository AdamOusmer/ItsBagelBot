// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

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
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/require"
)

var shippedResolver = regexp.MustCompile(`(?m)^resolver: (\S+)$`)

type reloadAccount struct {
	name string
	kp   nkeys.KeyPair
	pub  string
}

type reloadCluster struct {
	operator   nkeys.KeyPair
	opFile     string
	confFile   string
	resolver   string
	system     reloadAccount
	exporter   reloadAccount
	importer   reloadAccount
	withImport bool
}

func newReloadAccount(t *testing.T, name string) reloadAccount {
	kp, err := nkeys.CreateAccount()
	require.NoError(t, err)
	pub, err := kp.PublicKey()
	require.NoError(t, err)
	return reloadAccount{name: name, kp: kp, pub: pub}
}

func (c *reloadCluster) sign(t *testing.T, a reloadAccount) string {
	claims := jwt.NewAccountClaims(a.pub)
	claims.Name = a.name
	if c.withImport && a.pub == c.exporter.pub {
		claims.Exports.Add(&jwt.Export{Subject: "svc.ping", Type: jwt.Service})
	}
	if c.withImport && a.pub == c.importer.pub {
		claims.Imports.Add(&jwt.Import{Subject: "svc.ping", Account: c.exporter.pub, Type: jwt.Service})
	}
	token, err := claims.Encode(c.operator)
	require.NoError(t, err)
	return token
}

func (c *reloadCluster) writeConfig(t *testing.T) {
	preload := ""
	for _, a := range []reloadAccount{c.system, c.exporter, c.importer} {
		preload += fmt.Sprintf("  %s: %q\n", a.pub, c.sign(t, a))
	}
	conf := fmt.Sprintf("listen: 127.0.0.1:-1\noperator: %q\nsystem_account: %s\nresolver: %s\nresolver_preload: {\n%s}\n",
		c.opFile, c.system.pub, c.resolver, preload)
	require.NoError(t, os.WriteFile(c.confFile, []byte(conf), 0o600))
}

func newReloadCluster(t *testing.T, resolver string) *reloadCluster {
	operator, err := nkeys.CreateOperator()
	require.NoError(t, err)
	opPub, err := operator.PublicKey()
	require.NoError(t, err)
	c := &reloadCluster{operator: operator, resolver: resolver, system: newReloadAccount(t, "SYS"),
		exporter: newReloadAccount(t, "EXPORTER"), importer: newReloadAccount(t, "IMPORTER")}
	opClaims := jwt.NewOperatorClaims(opPub)
	opClaims.SystemAccount = c.system.pub
	opJWT, err := opClaims.Encode(operator)
	require.NoError(t, err)
	dir := t.TempDir()
	c.opFile, c.confFile = filepath.Join(dir, "operator.jwt"), filepath.Join(dir, "nats.conf")
	require.NoError(t, os.WriteFile(c.opFile, []byte(opJWT), 0o600))
	c.writeConfig(t)
	return c
}

func (c *reloadCluster) start(t *testing.T) *server.Server {
	opts, err := server.ProcessConfigFile(c.confFile)
	require.NoError(t, err)
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	require.NoError(t, err)
	go s.Start()
	require.True(t, s.ReadyForConnections(5*time.Second))
	t.Cleanup(s.Shutdown)
	return s
}

func connectAs(t *testing.T, s *server.Server, a reloadAccount) *nats.Conn {
	userKP, err := nkeys.CreateUser()
	require.NoError(t, err)
	userPub, err := userKP.PublicKey()
	require.NoError(t, err)
	token, err := jwt.NewUserClaims(userPub).Encode(a.kp)
	require.NoError(t, err)
	nc, err := nats.Connect(s.ClientURL(), nats.UserJWT(
		func() (string, error) { return token, nil },
		func(nonce []byte) ([]byte, error) { return userKP.Sign(nonce) }))
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

// The config reloader sidecar is the only path that applies account changes.
func TestReloadedAccountChangesGoLiveWithoutARestart(t *testing.T) {
	for _, file := range []string{"nats-server.conf", "nats-leaf-server.conf"} {
		t.Run(file, func(t *testing.T) {
			shipped := shippedResolver.FindStringSubmatch(readText(t, file))
			require.NotNil(t, shipped, "%s declares no single-line resolver", file)
			c := newReloadCluster(t, shipped[1])
			s := c.start(t)
			responder := connectAs(t, s, c.exporter)
			_, err := responder.Subscribe("svc.ping", func(m *nats.Msg) { _ = m.Respond([]byte("pong")) })
			require.NoError(t, err)
			require.NoError(t, responder.Flush())
			caller := connectAs(t, s, c.importer)

			time.Sleep(1100 * time.Millisecond)
			c.withImport = true
			c.writeConfig(t)
			require.NoError(t, s.Reload())

			reply, err := caller.Request("svc.ping", nil, 2*time.Second)
			require.NoError(t, err, "the reloaded import must reach the exporter on the open connection")
			require.Equal(t, "pong", string(reply.Data))
		})
	}
}
