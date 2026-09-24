// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/internal/natsacl"

	"github.com/nats-io/nats.go"
)

type roleEndpoint struct {
	account string
	role    string
}

type crossAccountProbe struct {
	responder roleEndpoint
	requester roleEndpoint
	subject   string
}

// liveACLServer is the running single operator-mode server a live ACL
// check connects role users against.
type liveACLServer struct {
	url  string
	ring *aclKeyring
}

// TestLiveACLPermissions compiles the real accounts.yaml with freshly
// generated keys, runs one operator-mode server preloaded with the result,
// and checks a sample of roles against real allowed and forbidden traffic.
func TestLiveACLPermissions(t *testing.T) {
	acl, err := natsacl.LoadACL(accountsYAMLPath)
	if err != nil {
		t.Fatalf("load %s: %v", accountsYAMLPath, err)
	}
	server := newLiveACLServer(t, acl)

	t.Run("users_bus", func(t *testing.T) {
		server.assertRoleForbidden(t, roleEndpoint{"BUS", "users_bus"}, "$JS.API.STREAM.DELETE.BAGEL_DATA")
	})
	t.Run("outgress_bus", func(t *testing.T) {
		server.assertRoleForbidden(t, roleEndpoint{"BUS", "outgress_bus"}, "twitch.ingress.event.premium")
	})
	t.Run("users_bus_allowed", func(t *testing.T) {
		server.assertRoleAllowedPublish(t, roleEndpoint{"BUS", "users_bus"}, "data.users.changed")
	})
	t.Run("outgress_bus_allowed", func(t *testing.T) {
		server.assertRoleAllowedPublish(t, roleEndpoint{"BUS", "outgress_bus"}, "data.twitch.clip.created")
	})
	t.Run("outgress_rpc_imports_users_rpc", func(t *testing.T) {
		server.assertCrossAccountRequest(t, crossAccountProbe{
			responder: roleEndpoint{"USERS_RPC", "users_rpc"},
			requester: roleEndpoint{"OUTGRESS_RPC", "outgress_rpc"},
			subject:   "bagel.rpc.internal.tokens.get",
		})
	})
	t.Run("admin_rpc_imports_restricted_deployer_rpc_export", func(t *testing.T) {
		server.assertCrossAccountRequest(t, crossAccountProbe{
			responder: roleEndpoint{"DEPLOYER_RPC", "deployer_rpc"},
			requester: roleEndpoint{"ADMIN_RPC", "admin_rpc"},
			subject:   "bagel.rpc.admin.deploy.status",
		})
	})
}

func newLiveACLServer(t *testing.T, acl *natsacl.ACL) liveACLServer {
	t.Helper()
	ring := newACLKeyring(t, acl)
	tokens := signedAccountJWTs(t, acl, ring)
	node := buildMigrateTopology(t, 1)[0]
	conf := liveACLConf(node, ring, liveACLCredentials{operatorJWT: signedOperatorJWT(t, ring), accountJWTs: tokens})
	s := startNodeFromConf(t, node, conf)
	t.Cleanup(s.Shutdown)
	if !s.ReadyForConnections(5 * time.Second) {
		t.Fatal("live ACL server did not become ready")
	}
	return liveACLServer{url: s.ClientURL(), ring: ring}
}

type liveACLCredentials struct {
	operatorJWT string
	accountJWTs map[string]string
}

func liveACLConf(node migrateNode, ring *aclKeyring, creds liveACLCredentials) string {
	var preload strings.Builder
	for name, pub := range ring.keys.Accounts {
		preload.WriteString("  " + pub + ": " + quoteConf(creds.accountJWTs[name]) + "\n")
	}
	return fmt.Sprintf(`
server_name: %s
listen: 127.0.0.1:%d
operator: %s
system_account: %s
resolver: MEMORY
resolver_preload: {
%s}
`, node.name, node.clientPort, quoteConf(creds.operatorJWT), quoteConf(ring.keys.Accounts["SYS"]), preload.String())
}

func quoteConf(s string) string { return "\"" + s + "\"" }

func (s liveACLServer) connectRole(t *testing.T, ep roleEndpoint) *nats.Conn {
	t.Helper()
	userJWT, userSeed := newRoleUser(t, s.ring, ep.account, ep.role)
	nc, err := nats.Connect(s.url, nats.UserJWTAndSeed(userJWT, userSeed), nats.Timeout(5*time.Second))
	if err != nil {
		t.Fatalf("connect as %s/%s: %v", ep.account, ep.role, err)
	}
	return nc
}

func (s liveACLServer) connectRoleWithPermissionErrors(t *testing.T, ep roleEndpoint) (*nats.Conn, <-chan error) {
	t.Helper()
	userJWT, userSeed := newRoleUser(t, s.ring, ep.account, ep.role)
	permErr := make(chan error, 1)
	nc, err := nats.Connect(s.url,
		nats.UserJWTAndSeed(userJWT, userSeed), nats.Timeout(5*time.Second),
		nats.ErrorHandler(func(_ *nats.Conn, _ *nats.Subscription, err error) {
			select {
			case permErr <- err:
			default:
			}
		}),
	)
	if err != nil {
		t.Fatalf("connect as %s/%s: %v", ep.account, ep.role, err)
	}
	return nc, permErr
}

func (s liveACLServer) assertRoleAllowedPublish(t *testing.T, ep roleEndpoint, subject string) {
	t.Helper()
	nc, permErr := s.connectRoleWithPermissionErrors(t, ep)
	defer nc.Close()
	publishAndFlush(t, nc, ep, subject)
	select {
	case err := <-permErr:
		t.Fatalf("%s/%s should be allowed to publish %s: %v", ep.account, ep.role, subject, err)
	case <-time.After(50 * time.Millisecond):
	}
}

func (s liveACLServer) assertRoleForbidden(t *testing.T, ep roleEndpoint, subject string) {
	t.Helper()
	nc, permErr := s.connectRoleWithPermissionErrors(t, ep)
	defer nc.Close()
	publishAndFlush(t, nc, ep, subject)
	select {
	case err := <-permErr:
		if !strings.Contains(err.Error(), "Permissions Violation") {
			t.Fatalf("%s/%s publish %s: want a permissions violation, got: %v", ep.account, ep.role, subject, err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("%s/%s publish %s: expected a permissions violation, saw none", ep.account, ep.role, subject)
	}
}

func publishAndFlush(t *testing.T, nc *nats.Conn, ep roleEndpoint, subject string) {
	t.Helper()
	if err := nc.Publish(subject, nil); err != nil {
		t.Fatalf("%s/%s publish %s: %v", ep.account, ep.role, subject, err)
	}
	if err := nc.FlushTimeout(500 * time.Millisecond); err != nil {
		t.Fatalf("%s/%s flush after publish %s: %v", ep.account, ep.role, subject, err)
	}
}

// assertCrossAccountRequest proves a real request crosses an account
// import: it starts a responder on the exporting side and a requester on
// the importing side, so a restricted export's activation token is
// actually exercised, not just compiled.
func (s liveACLServer) assertCrossAccountRequest(t *testing.T, probe crossAccountProbe) {
	t.Helper()
	rnc := s.connectRole(t, probe.responder)
	defer rnc.Close()
	sub, err := rnc.Subscribe(probe.subject, func(msg *nats.Msg) { _ = msg.Respond([]byte("ok")) })
	if err != nil {
		t.Fatalf("%s/%s subscribe %s: %v", probe.responder.account, probe.responder.role, probe.subject, err)
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := rnc.Flush(); err != nil {
		t.Fatalf("%s/%s flush: %v", probe.responder.account, probe.responder.role, err)
	}

	qnc := s.connectRole(t, probe.requester)
	defer qnc.Close()
	reply, err := qnc.Request(probe.subject, nil, 3*time.Second)
	if err != nil {
		t.Fatalf("%s/%s request %s: %v", probe.requester.account, probe.requester.role, probe.subject, err)
	}
	if string(reply.Data) != "ok" {
		t.Fatalf("%s/%s request %s: got %q, want %q", probe.requester.account, probe.requester.role, probe.subject, reply.Data, "ok")
	}
}
