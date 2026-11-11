// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package messaging

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A 3-node operator-mode cluster with a JWT-issued BUS account must serve JetStream through the hub domain.
func TestOperatorModeSmoke(t *testing.T) {
	nodes := buildTopology(t, 3)
	op := newOperatorFixture(t)
	url := startCluster(t, nodes, func(n testNode) string { return op.clusterConf(n, nodes) })
	busJWT, busSeed := newRoleUser(t, op.ring, "BUS", "bus")
	nc, err := nats.Connect(url, nats.UserJWTAndSeed(busJWT, busSeed), nats.Timeout(5*time.Second))
	require.NoError(t, err, "connect as BUS role user")
	t.Cleanup(nc.Close)
	waitForJetStreamReady(t, nc, "hub")
	js, err := jetstream.NewWithDomain(nc, "hub")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = js.CreateStream(ctx, jetstream.StreamConfig{Name: "SMOKE", Subjects: []string{"smoke.>"}})

	assert.NoError(t, err, "create stream through operator mode BUS account")
}

// TestLiveACLPermissions compiles the real accounts.yaml with freshly generated keys, runs one operator-mode
// server preloaded with the result, and checks a sample of roles against real allowed and forbidden traffic.
func TestLiveACLPermissions(t *testing.T) {
	server := newLiveACLServer(t, committedACL(t))

	t.Run("forbids publishes outside a role's grants with a permissions violation", func(t *testing.T) {
		for _, probe := range []struct {
			role    roleEndpoint
			subject string
		}{
			{roleEndpoint{"BUS", "users_bus"}, "$JS.API.STREAM.DELETE.BAGEL_DATA"},
			{roleEndpoint{"BUS", "outgress_bus"}, "twitch.ingress.event.premium"},
		} {
			err := server.publishOutcome(t, probe.role, probe.subject, 500*time.Millisecond)

			require.Error(t, err, "%s publish %s: expected a permissions violation, saw none", probe.role.role, probe.subject)
			assert.ErrorContains(t, err, "Permissions Violation", "%s publish %s", probe.role.role, probe.subject)
		}
	})
	t.Run("allows publishes inside a role's grants", func(t *testing.T) {
		for _, probe := range []struct {
			role    roleEndpoint
			subject string
		}{
			{roleEndpoint{"BUS", "users_bus"}, "data.users.changed"},
			{roleEndpoint{"BUS", "outgress_bus"}, "data.twitch.clip.created"},
			{roleEndpoint{"BUS", "commands_bus"}, "dlq.data.users.deleted"},
			{roleEndpoint{"BUS", "modules_bus"}, "dlq.data.users.deleted"},
		} {
			assert.NoError(t, server.publishOutcome(t, probe.role, probe.subject, 50*time.Millisecond), "%s should publish %s", probe.role.role, probe.subject)
		}
	})
	t.Run("serves a request across an account import", func(t *testing.T) {
		for _, probe := range []crossAccountProbe{
			{roleEndpoint{"USERS_RPC", "users_rpc"}, roleEndpoint{"OUTGRESS_RPC", "outgress_rpc"}, "bagel.rpc.internal.tokens.get"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"USERS_RPC", "users_rpc"}, "bagel.rpc.admin.user.auth.check"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"USERS_RPC", "users_rpc"}, "bagel.rpc.internal.admin.audit.append"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"DASHBOARD_RPC", "dashboard_rpc"}, "bagel.rpc.admin.user.audit.append"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"ADMIN_RPC", "admin_rpc"}, "bagel.rpc.admin.analytics"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"USERS_RPC", "users_rpc"}, "bagel.rpc.internal.admin.staff.active"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"PROJECTOR_RPC", "projector_rpc"}, "bagel.rpc.health.admin"},
			{roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"}, roleEndpoint{"TRANSACTIONS_RPC", "transactions_rpc"}, "bagel.rpc.admin.user.auth.check"},
			{roleEndpoint{"USERS_RPC", "users_rpc"}, roleEndpoint{"ADMIN_RPC", "admin_rpc"}, "bagel.rpc.admin.user.test.set"},
		} {
			server.assertCrossAccountRequest(t, probe)
		}
	})
}

func TestAuditDeliveryCannotBeCalledByConsoles(t *testing.T) {
	server := newLiveACLServer(t, committedACL(t))
	responder := server.connectRole(t, roleEndpoint{"ADMIN_SERVICE_RPC", "admin_service_rpc"})
	_, err := responder.Subscribe("bagel.rpc.internal.admin.audit.>", func(msg *nats.Msg) { _ = msg.Respond([]byte("ok")) })
	require.NoError(t, err)
	require.NoError(t, responder.Flush())

	for _, caller := range []roleEndpoint{
		{"ADMIN_RPC", "admin_rpc"},
		{"DASHBOARD_RPC", "dashboard_rpc"},
		{"TRANSACTIONS_RPC", "transactions_rpc"},
	} {
		t.Run(caller.account, func(t *testing.T) {
			nc := server.connectRole(t, caller)
			for _, subject := range []string{"bagel.rpc.internal.admin.audit.append", "bagel.rpc.internal.admin.audit.append.node.n1"} {
				_, err := nc.Request(subject, nil, time.Second)
				require.ErrorIs(t, err, nats.ErrNoResponders)
			}
		})
	}
}
