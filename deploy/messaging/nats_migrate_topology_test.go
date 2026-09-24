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
)

// migrateNode is one server's slice of a test cluster's topology; its ports
// and store dir stay fixed across restarts, matching an in-place restart.
type migrateNode struct {
	name        string
	clientPort  int
	clusterPort int
	storeDir    string
}

func buildMigrateTopology(t *testing.T, n int) []migrateNode {
	t.Helper()
	nodes := make([]migrateNode, n)
	for i := range nodes {
		nodes[i] = migrateNode{
			name:        fmt.Sprintf("rehearsal%d", i),
			clientPort:  freeTCPPort(t),
			clusterPort: freeTCPPort(t),
			storeDir:    t.TempDir(),
		}
	}
	return nodes
}

func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

func clusterRoutes(nodes []migrateNode) string {
	routes := make([]string, len(nodes))
	for i, n := range nodes {
		routes[i] = fmt.Sprintf("%q", fmt.Sprintf("nats-route://127.0.0.1:%d", n.clusterPort))
	}
	return strings.Join(routes, ", ")
}

// migrateCluster is a running operator-mode test cluster.
type migrateCluster struct {
	nodes   []migrateNode
	servers []*server.Server
}

func startMigrateCluster(t *testing.T, nodes []migrateNode, confFor func(migrateNode) string) *migrateCluster {
	t.Helper()
	cluster := &migrateCluster{nodes: nodes}
	for _, n := range nodes {
		cluster.servers = append(cluster.servers, startNodeFromConf(t, n, confFor(n)))
	}
	for _, s := range cluster.servers {
		if !s.ReadyForConnections(10 * time.Second) {
			t.Fatal("server did not become ready for connections")
		}
	}
	return cluster
}

func startNodeFromConf(t *testing.T, n migrateNode, conf string) *server.Server {
	t.Helper()
	path := filepath.Join(t.TempDir(), n.name+".conf")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		t.Fatalf("write conf for %s: %v", n.name, err)
	}
	opts, err := server.ProcessConfigFile(path)
	if err != nil {
		t.Fatalf("process conf for %s: %v", n.name, err)
	}
	opts.NoLog, opts.NoSigs = true, true
	s, err := server.NewServer(opts)
	if err != nil {
		t.Fatalf("new server %s: %v", n.name, err)
	}
	s.Start()
	return s
}

func (c *migrateCluster) shutdown() {
	for _, s := range c.servers {
		s.Shutdown()
	}
	for _, s := range c.servers {
		s.WaitForShutdown()
	}
}

func (c *migrateCluster) clientURL() string {
	return fmt.Sprintf("nats://127.0.0.1:%d", c.nodes[0].clientPort)
}

// waitForJetStreamReady polls until the account's JetStream API answers,
// since ReadyForConnections only proves the listener is up, not that the
// clustered meta group has finished its first leader election.
func waitForJetStreamReady(t *testing.T, nc *nats.Conn, domain string) {
	t.Helper()
	subject := jsAPISubject(domain, "INFO")
	deadline := time.Now().Add(20 * time.Second)
	for {
		if _, err := nc.Request(subject, nil, 500*time.Millisecond); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("jetstream did not become ready on %s within 20s", subject)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func jsAPISubject(domain, suffix string) string {
	if domain == "" {
		return "$JS.API." + suffix
	}
	return "$JS." + domain + ".API." + suffix
}

type operatorFixture struct {
	ring        *aclKeyring
	operatorJWT string
	busJWT      string
	sysJWT      string
}

func newOperatorFixture(t *testing.T) *operatorFixture {
	t.Helper()
	acl := rehearsalACL()
	ring := newACLKeyring(t, acl)
	tokens := signedAccountJWTs(t, acl, ring)
	return &operatorFixture{ring: ring, operatorJWT: signedOperatorJWT(t, ring), busJWT: tokens["BUS"], sysJWT: tokens["SYS"]}
}

func signedOperatorJWT(t *testing.T, ring *aclKeyring) string {
	t.Helper()
	claims := jwt.NewOperatorClaims(publicKey(t, ring.operator))
	token, err := claims.Encode(ring.operator)
	if err != nil {
		t.Fatalf("encode operator jwt: %v", err)
	}
	return token
}

func (op *operatorFixture) busPub() string { return op.ring.keys.Accounts["BUS"] }
func (op *operatorFixture) sysPub() string { return op.ring.keys.Accounts["SYS"] }

func rehearsalACL() *natsacl.ACL {
	return &natsacl.ACL{
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
}

func operatorModeConf(n migrateNode, nodes []migrateNode, op *operatorFixture) string {
	return fmt.Sprintf(`
server_name: %s
listen: 127.0.0.1:%d
cluster {
  name: rehearsal
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
`, n.name, n.clientPort, n.clusterPort, op.busPub(), clusterRoutes(nodes), n.storeDir,
		op.operatorJWT, op.sysPub(),
		op.busPub(), op.busJWT, op.sysPub(), op.sysJWT)
}
