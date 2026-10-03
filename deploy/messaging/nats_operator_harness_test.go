// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/jwt/v2"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"github.com/stretchr/testify/require"
)

type aclKeyring struct {
	operator nkeys.KeyPair
	keys     *natsacl.Keys
	accounts map[string]nkeys.KeyPair
	roles    map[string]map[string]nkeys.KeyPair
}

func createKeyPair(t *testing.T, create func() (nkeys.KeyPair, error)) nkeys.KeyPair {
	t.Helper()
	kp, err := create()
	require.NoError(t, err)
	return kp
}

func publicKey(t *testing.T, kp nkeys.KeyPair) string {
	t.Helper()
	pub, err := kp.PublicKey()
	require.NoError(t, err)
	return pub
}

func newACLKeyring(t *testing.T, acl *natsacl.ACL) *aclKeyring {
	t.Helper()
	operator := createKeyPair(t, nkeys.CreateOperator)
	ring := &aclKeyring{
		operator: operator,
		keys: &natsacl.Keys{
			Operator: publicKey(t, operator), Accounts: map[string]string{}, Roles: map[string]map[string]string{},
			Activations: map[string][]natsacl.Activation{},
		},
		accounts: map[string]nkeys.KeyPair{},
		roles:    map[string]map[string]nkeys.KeyPair{},
	}
	for name, spec := range acl.Accounts {
		ring.addAccount(t, name, spec)
	}
	return ring
}

func (r *aclKeyring) addAccount(t *testing.T, name string, spec natsacl.AccountSpec) {
	t.Helper()
	account := createKeyPair(t, nkeys.CreateAccount)
	r.accounts[name] = account
	r.keys.Accounts[name] = publicKey(t, account)
	if len(spec.Roles) == 0 {
		return
	}
	r.roles[name] = map[string]nkeys.KeyPair{}
	r.keys.Roles[name] = map[string]string{}
	for role := range spec.Roles {
		signer := createKeyPair(t, nkeys.CreateAccount)
		r.roles[name][role] = signer
		r.keys.Roles[name][role] = publicKey(t, signer)
	}
}

func (r *aclKeyring) grantActivations(t *testing.T, exporter string, exp natsacl.ExportSpec) {
	t.Helper()
	subject := exportSubject(exp)
	kind := jwt.Service
	if exp.Stream != "" {
		kind = jwt.Stream
	}
	for _, importer := range exp.Accounts {
		claims := jwt.NewActivationClaims(r.keys.Accounts[importer])
		claims.ImportSubject = jwt.Subject(subject)
		claims.ImportType = kind
		token, err := claims.Encode(r.accounts[exporter])
		require.NoError(t, err, "mint activation for %s", subject)
		r.keys.Activations[importer] = append(r.keys.Activations[importer], natsacl.Activation{From: exporter, Subject: subject, Token: token})
	}
}

// signedAccountJWTs mints real activations for acl's restricted exports (Compile fails with
// ErrMissingActivation without them), compiles acl, and signs every account claim with the operator key.
func signedAccountJWTs(t *testing.T, acl *natsacl.ACL, ring *aclKeyring) map[string]string {
	t.Helper()
	for exporter, spec := range acl.Accounts {
		for _, exp := range spec.Exports {
			if len(exp.Accounts) > 0 {
				ring.grantActivations(t, exporter, exp)
			}
		}
	}
	claims, err := natsacl.Compile(acl, ring.keys)
	require.NoError(t, err)

	tokens := make(map[string]string, len(claims))
	for _, c := range claims {
		token, err := c.Encode(ring.operator)
		require.NoError(t, err, "encode account %s", c.Name)
		tokens[c.Name] = token
	}
	return tokens
}

// newRoleUser mints a JWT+seed for a fresh user under account's role, signed
// by that role's scoped signing key exactly as a real deployment would.
func newRoleUser(t *testing.T, ring *aclKeyring, account, role string) (userJWT, userSeed string) {
	t.Helper()
	user := createKeyPair(t, nkeys.CreateUser)
	seed, err := user.Seed()
	require.NoError(t, err)
	claims := jwt.NewUserClaims(publicKey(t, user))
	claims.IssuerAccount = ring.keys.Accounts[account]
	claims.SetScoped(true)
	signer, ok := ring.roles[account][role]
	require.True(t, ok, "no signing key for %s role %s", account, role)
	token, err := claims.Encode(signer)
	require.NoError(t, err)
	return token, string(seed)
}

func signedOperatorJWT(t *testing.T, ring *aclKeyring) string {
	t.Helper()
	token, err := jwt.NewOperatorClaims(publicKey(t, ring.operator)).Encode(ring.operator)
	require.NoError(t, err)
	return token
}

type testNode struct {
	name        string
	clientPort  int
	clusterPort int
	storeDir    string
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func buildTopology(t *testing.T, n int) []testNode {
	t.Helper()
	nodes := make([]testNode, n)
	for i := range nodes {
		nodes[i] = testNode{name: fmt.Sprintf("node%d", i), clientPort: freeTCPPort(t), clusterPort: freeTCPPort(t), storeDir: t.TempDir()}
	}
	return nodes
}

func startNodeFromConf(t *testing.T, n testNode, conf string) *server.Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), n.name+".conf")
	require.NoError(t, os.WriteFile(path, []byte(conf), 0o600))
	opts, err := server.ProcessConfigFile(path)
	require.NoError(t, err, "process conf for %s", n.name)
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	require.NoError(t, err, "new server %s", n.name)
	s.Start()
	return s
}

func startCluster(t *testing.T, nodes []testNode, confFor func(testNode) string) (clientURL string) {
	t.Helper()
	servers := make([]*server.Server, len(nodes))
	for i, n := range nodes {
		servers[i] = startNodeFromConf(t, n, confFor(n))
	}
	t.Cleanup(func() {
		for _, s := range servers {
			s.Shutdown()
		}
		for _, s := range servers {
			s.WaitForShutdown()
		}
	})
	for _, s := range servers {
		require.True(t, s.ReadyForConnections(10*time.Second), "server did not become ready for connections")
	}
	return fmt.Sprintf("nats://127.0.0.1:%d", nodes[0].clientPort)
}

// waitForJetStreamReady polls until the account's JetStream API answers: ReadyForConnections only proves
// the listener is up, not that the clustered meta group has finished its first leader election.
func waitForJetStreamReady(t *testing.T, nc *nats.Conn, domain string) {
	t.Helper()
	subject := "$JS." + domain + ".API.INFO"
	require.Eventually(t, func() bool {
		_, err := nc.Request(subject, nil, 500*time.Millisecond)
		return err == nil
	}, 20*time.Second, 200*time.Millisecond, "jetstream did not become ready on %s", subject)
}

type operatorFixture struct {
	ring        *aclKeyring
	operatorJWT string
	busJWT      string
	sysJWT      string
}

func newOperatorFixture(t *testing.T) *operatorFixture {
	t.Helper()
	acl := &natsacl.ACL{
		SystemAccount: "SYS",
		Accounts: map[string]natsacl.AccountSpec{
			"SYS": {Roles: map[string]natsacl.RoleSpec{"sys": {}}},
			"BUS": {
				JetStream: &natsacl.JetStreamSpec{ClusterTraffic: "owner"},
				Mappings:  "hub-domain",
				Roles:     map[string]natsacl.RoleSpec{"bus": {}},
			},
		},
	}
	ring := newACLKeyring(t, acl)
	tokens := signedAccountJWTs(t, acl, ring)
	return &operatorFixture{ring: ring, operatorJWT: signedOperatorJWT(t, ring), busJWT: tokens["BUS"], sysJWT: tokens["SYS"]}
}

func (op *operatorFixture) clusterConf(n testNode, nodes []testNode) string {
	routes := make([]string, len(nodes))
	for i, peer := range nodes {
		routes[i] = fmt.Sprintf("%q", fmt.Sprintf("nats-route://127.0.0.1:%d", peer.clusterPort))
	}
	bus, sys := op.ring.keys.Accounts["BUS"], op.ring.keys.Accounts["SYS"]
	return fmt.Sprintf(`
server_name: %s
listen: 127.0.0.1:%d
cluster {
  name: operator-test
  listen: 127.0.0.1:%d
  accounts: [%q]
  routes: [%s]
}
jetstream {
  store_dir: %q
  domain: hub
}
operator: %q
system_account: %q
resolver: MEMORY
resolver_preload: {
  %s: %q
  %s: %q
}
`, n.name, n.clientPort, n.clusterPort, bus, strings.Join(routes, ", "), n.storeDir,
		op.operatorJWT, sys, bus, op.busJWT, sys, op.sysJWT)
}

type roleEndpoint struct {
	account string
	role    string
}

type crossAccountProbe struct {
	responder roleEndpoint
	requester roleEndpoint
	subject   string
}

type liveACLServer struct {
	url  string
	ring *aclKeyring
}

func newLiveACLServer(t *testing.T, acl *natsacl.ACL) liveACLServer {
	t.Helper()
	ring := newACLKeyring(t, acl)
	tokens := signedAccountJWTs(t, acl, ring)
	var preload strings.Builder
	for name, pub := range ring.keys.Accounts {
		preload.WriteString(fmt.Sprintf("  %s: %q\n", pub, tokens[name]))
	}
	node := buildTopology(t, 1)[0]
	conf := fmt.Sprintf("\nserver_name: %s\nlisten: 127.0.0.1:%d\noperator: %q\nsystem_account: %q\nresolver: MEMORY\nresolver_preload: {\n%s}\n",
		node.name, node.clientPort, signedOperatorJWT(t, ring), ring.keys.Accounts["SYS"], preload.String())
	s := startNodeFromConf(t, node, conf)
	t.Cleanup(s.Shutdown)
	require.True(t, s.ReadyForConnections(5*time.Second), "live ACL server did not become ready")
	return liveACLServer{url: s.ClientURL(), ring: ring}
}

func (s liveACLServer) connectRole(t *testing.T, ep roleEndpoint, opts ...nats.Option) *nats.Conn {
	t.Helper()
	userJWT, userSeed := newRoleUser(t, s.ring, ep.account, ep.role)
	nc, err := nats.Connect(s.url, append([]nats.Option{nats.UserJWTAndSeed(userJWT, userSeed), nats.Timeout(5 * time.Second)}, opts...)...)
	require.NoError(t, err, "connect as %s/%s", ep.account, ep.role)
	t.Cleanup(nc.Close)
	return nc
}

// publishOutcome returns the async error the broker raised for publishing subject as ep, or nil
// when none arrived within wait.
func (s liveACLServer) publishOutcome(t *testing.T, ep roleEndpoint, subject string, wait time.Duration) error {
	t.Helper()
	asyncErr := make(chan error, 1)
	nc := s.connectRole(t, ep, nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
		select {
		case asyncErr <- err:
		default:
		}
	}))
	require.NoError(t, nc.Publish(subject, nil), "%s/%s publish %s", ep.account, ep.role, subject)
	require.NoError(t, nc.FlushTimeout(500*time.Millisecond), "%s/%s flush after publish %s", ep.account, ep.role, subject)
	select {
	case err := <-asyncErr:
		return err
	case <-time.After(wait):
		return nil
	}
}

// assertCrossAccountRequest proves a real request crosses an account import: a responder on the exporting
// side and a requester on the importing side, so a restricted export's activation token is actually exercised.
func (s liveACLServer) assertCrossAccountRequest(t *testing.T, probe crossAccountProbe) {
	t.Helper()
	responder := s.connectRole(t, probe.responder)
	_, err := responder.Subscribe(probe.subject, func(msg *nats.Msg) { _ = msg.Respond([]byte("ok")) })
	require.NoError(t, err)
	require.NoError(t, responder.Flush())

	reply, err := s.connectRole(t, probe.requester).Request(probe.subject, nil, 3*time.Second)

	require.NoError(t, err, "%s/%s request %s", probe.requester.account, probe.requester.role, probe.subject)
	require.Equal(t, "ok", string(reply.Data))
}
