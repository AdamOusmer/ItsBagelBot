// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/adminauth"
	"ItsBagelBot/internal/domain/invalidate"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/cache"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestAdminDeleteInvalidatesCommittedAccount(t *testing.T) {
	h := newHarness(t)
	h.staff(staffFixture{id: 1, role: adminauth.Owner, active: true})
	h.users(1, time.Now().Add(-time.Hour))
	deleted := make(chan bool, 2)
	for _, scope := range []string{invalidate.ScopeStatus, invalidate.ScopeUser} {
		h.repo.Invalidations().Watch(scope, func(e cache.Event) {
			deleted <- !h.client.User.Query().ExistX(t.Context())
		})
	}
	reply := h.admin(t, "delete", 1, usersrpc.AdminRequest{UserID: "9000"})
	require.Empty(t, reply.Code)
	require.Eventually(t, func() bool { return len(deleted) == 2 }, time.Second, time.Millisecond)
	require.Equal(t, []bool{true, true}, []bool{<-deleted, <-deleted})
}

func TestActiveRPCsPublishStatusOnce(t *testing.T) {
	for _, verb := range []string{"set_active", "active_set"} {
		t.Run(verb, func(t *testing.T) {
			h := newHarness(t)
			h.staff(staffFixture{id: 1, role: adminauth.Owner, active: true})
			require.NoError(t, h.repo.Register(t.Context(), repository.Registration{ID: 1001, Username: "account", Email: "account@example.com"}))
			ids := make(chan uint64, 4)
			h.repo.Invalidations().Watch(invalidate.ScopeStatus, func(e cache.Event) { ids <- e.ID })
			saveActive(t, h, verb)
			require.Len(t, ids, 1)
			require.Equal(t, uint64(1001), <-ids)
		})
	}
}

func saveActive(t *testing.T, h harness, verb string) {
	t.Helper()
	if verb == "set_active" {
		reply := h.admin(t, verb, 1, usersrpc.AdminRequest{UserID: "1001", Active: false})
		require.Empty(t, reply.Code)
		return
	}
	w := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}, Repo: h.repo}
	require.NoError(t, rpc.SubscribeDashboard(w, "test.dashboard"))
	reply := call[map[string]any](t, h, "test.dashboard.active_set", usersrpc.ActiveSetRequest{BroadcasterUserID: "1001", Active: false})
	require.Equal(t, true, reply["ok"])
}
