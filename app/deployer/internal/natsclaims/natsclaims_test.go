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
	sysJWT string
}

func buildOpChain(t *testing.T) opChain {
	t.Helper()
	opKp, err := nkeys.CreateOperator()
	if err != nil {
		t.Fatal(err)
	}
	opPub, err := opKp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	oc := jwt.NewOperatorClaims(opPub)
	opJWT, err := oc.Encode(opKp)
	if err != nil {
		t.Fatal(err)
	}
	sysKp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	sysPub, err := sysKp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	sysAC := jwt.NewAccountClaims(sysPub)
	sysAC.Name = "SYS"
	sysJWT, err := sysAC.Encode(opKp)
	if err != nil {
		t.Fatal(err)
	}
	return opChain{opKp: opKp, opJWT: opJWT, sysKp: sysKp, sysPub: sysPub, sysJWT: sysJWT}
}

func writeJWTFile(t *testing.T, dir, pub, token string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, pub+".jwt"), []byte(token), 0o644); err != nil {
		t.Fatal(err)
	}
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
	cfgText := fmt.Sprintf(clusterConfigTmpl, spec.name, chain.opJWT, chain.sysPub, dir, spec.routes)
	cfgPath := filepath.Join(t.TempDir(), spec.name+".conf")
	if err := os.WriteFile(cfgPath, []byte(cfgText), 0o644); err != nil {
		t.Fatal(err)
	}
	opts, err := server.ProcessConfigFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	opts.NoLog, opts.NoSigs = true, true
	srv, err := server.NewServer(opts)
	if err != nil {
		t.Fatal(err)
	}
	srv.Start()
	t.Cleanup(srv.Shutdown)
	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatalf("server %s never became ready", spec.name)
	}
	return srv
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// Route pooling means NumRoutes() is a multiple of peer count, not the peer
// count itself, so "at least one route per peer" is what formation means.
func waitClustered(t *testing.T, servers ...*server.Server) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		formed := true
		for _, s := range servers {
			if s.NumRoutes() < len(servers)-1 {
				formed = false
			}
		}
		if formed {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("cluster did not form in time")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func clearNATSTLSEnv(t *testing.T) {
	t.Helper()
	t.Setenv("NATS_CA_PEM", "")
	t.Setenv("NATS_CLIENT_CERT_FILE", "")
	t.Setenv("NATS_CLIENT_KEY_FILE", "")
}

func seedTestAccount(t *testing.T, chain opChain) (pub, token string) {
	t.Helper()
	testKp, err := nkeys.CreateAccount()
	if err != nil {
		t.Fatal(err)
	}
	testPub, err := testKp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	testAC := jwt.NewAccountClaims(testPub)
	testAC.Name = "TESTACC"
	testJWT, err := testAC.Encode(chain.opKp)
	if err != nil {
		t.Fatal(err)
	}
	return testPub, testJWT
}

// startCluster boots 3 clustered nodes, seeding the test account onto the
// first so the full resolver can sync it to the other two before any test runs.
func startCluster(t *testing.T, chain opChain, seedPub, seedJWT string) *server.Server {
	t.Helper()
	seed := t.TempDir()
	writeJWTFile(t, seed, seedPub, seedJWT)

	srvA := startNode(t, chain, nodeSpec{name: "srv-A", seedDir: seed})
	route := fmt.Sprintf("routes: [nats-route://127.0.0.1:%d]", srvA.ClusterAddr().Port)
	srvB := startNode(t, chain, nodeSpec{name: "srv-B", routes: route})
	srvC := startNode(t, chain, nodeSpec{name: "srv-C", routes: route})
	waitClustered(t, srvA, srvB, srvC)
	time.Sleep(500 * time.Millisecond) // let the full resolver sync the seeded account onto B and C
	return srvA
}

func connectAdapter(t *testing.T, url string, chain opChain) *Adapter {
	t.Helper()
	sysUserKp, err := nkeys.CreateUser()
	if err != nil {
		t.Fatal(err)
	}
	sysUserPub, err := sysUserKp.PublicKey()
	if err != nil {
		t.Fatal(err)
	}
	sysUserSeed, err := sysUserKp.Seed()
	if err != nil {
		t.Fatal(err)
	}
	sysUC := jwt.NewUserClaims(sysUserPub)
	sysUserJWT, err := sysUC.Encode(chain.sysKp)
	if err != nil {
		t.Fatal(err)
	}

	adapter, err := New(Config{HubURL: url, LeafURL: url, SysJWT: sysUserJWT, SysSeed: string(sysUserSeed)})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return adapter
}

func checkServersCount(t *testing.T, adapter *Adapter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	names, err := adapter.Servers(ctx, ports.ClusterHub)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 {
		t.Fatalf("Servers() = %v, want 3 names", names)
	}
}

func checkPushReplies(t *testing.T, replies []ports.ClaimsReply) {
	t.Helper()
	if len(replies) != 3 {
		t.Fatalf("Push() replies = %+v, want 3", replies)
	}
	for _, r := range replies {
		if r.Code != 200 {
			t.Fatalf("reply from %s: code = %d, want 200 (%s)", r.Server, r.Code, r.Message)
		}
		if r.Server == "" {
			t.Fatal("reply carries no server name")
		}
	}
}

func checkPushAndLookup(t *testing.T, adapter *Adapter, chain opChain, testPub string) {
	t.Helper()
	testAC := jwt.NewAccountClaims(testPub)
	testAC.Name = "TESTACC"
	testAC.Exports = jwt.Exports{{Subject: "svc.>", Type: jwt.Service}}
	pushJWT, err := testAC.Encode(chain.opKp)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	replies, err := adapter.Push(ctx, ports.ClusterHub, pushJWT, 3)
	if err != nil {
		t.Fatal(err)
	}
	checkPushReplies(t, replies)

	lookupCtx, lookupCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer lookupCancel()
	live, err := adapter.Lookup(lookupCtx, ports.ClusterHub, testPub)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jwt.DecodeAccountClaims(live)
	if err != nil {
		t.Fatalf("Lookup() returned an undecodable JWT: %v", err)
	}
	if len(decoded.Exports) != 1 {
		t.Fatalf("Lookup() claims = %+v, want the pushed export", decoded)
	}
}

// TestAdapterAgainstEmbeddedCluster runs a real 3 node operator-mode NATS
// cluster with the full resolver and drives Servers/Lookup/Push through it.
func TestAdapterAgainstEmbeddedCluster(t *testing.T) {
	clearNATSTLSEnv(t)
	chain := buildOpChain(t)
	testPub, testJWT := seedTestAccount(t, chain)
	srvA := startCluster(t, chain, testPub, testJWT)

	adapter := connectAdapter(t, srvA.ClientURL(), chain)
	defer adapter.Close()

	t.Run("Servers counts every node in the cluster", func(t *testing.T) { checkServersCount(t, adapter) })
	t.Run("Push reaches every server with code 200", func(t *testing.T) { checkPushAndLookup(t, adapter, chain, testPub) })
}
