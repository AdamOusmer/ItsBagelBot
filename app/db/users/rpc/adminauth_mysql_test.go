// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"sync"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/adminuser"
	_ "ItsBagelBot/app/db/users/ent/runtime"
	"ItsBagelBot/app/db/users/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const ownerRemovalRounds = 40

func newMySQLStaffHarness(t *testing.T) harness {
	t.Helper()
	raw := testdb.MySQL(t, "bagel_staff_")
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	nc := testnats.Connect(t)
	wiring := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}}
	require.NoError(t, rpc.SubscribeAdminAuth(wiring, client, authPrefix, auditPrefix))
	return harness{nc: nc, client: client}
}

func TestRemoveStaffTwoOwnersRemovingEachOtherLeavesAnOwner(t *testing.T) {
	h := newMySQLStaffHarness(t)
	h.staff(
		staffFixture{101, adminuser.RoleOwner, true},
		staffFixture{102, adminuser.RoleOwner, true},
	)

	for round := range ownerRemovalRounds {
		h.client.AdminUser.Update().SetActive(true).ExecX(t.Context())

		replies := make([]usersrpc.AuthReply, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i, pair := range [][2]string{{"101", "102"}, {"102", "101"}} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				replies[i] = h.auth(t, "remove", usersrpc.AuthRequest{ActorID: pair[0], UserID: pair[1]})
			}()
		}
		close(start)
		wg.Wait()

		owners := h.client.AdminUser.Query().Where(adminuser.RoleEQ(adminuser.RoleOwner), adminuser.ActiveEQ(true)).CountX(t.Context())
		assert.Equal(t, 1, owners, "round %d: replies %q / %q", round, replies[0].Error, replies[1].Error)
	}
}
