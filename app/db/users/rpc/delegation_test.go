// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestDelegationRPCPreservesGrantReplies(t *testing.T) {
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(repository.UsersConfig{Client: client, Pub: bustest.NewPublisher(), Log: zap.NewNop()})
	t.Cleanup(func() { repo.Close(t.Context()) })
	nc := testnats.Connect(t)
	require.NoError(t, rpc.SubscribeDelegation(rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repo}, "delegation.test"))
	require.NoError(t, nc.Flush())
	h := harness{nc: nc}
	created := call[struct{ Token string }](t, h, "delegation.test.create", usersrpc.CreateDelegationRequest{OwnerUserID: "1001", OwnerLogin: "owner", Sections: []string{"commands"}})
	require.NotEmpty(t, created.Token)
	consumed := call[map[string]any](t, h, "delegation.test.consume", map[string]string{"token": created.Token, "delegate_user_id": "2001", "delegate_login": "delegate"})
	require.Equal(t, map[string]any{"ok": true, "owner_user_id": "1001", "owner_login": "owner", "sections": []any{"commands"}}, consumed)
	for _, tc := range []struct {
		verb string
		req  any
		want map[string]any
	}{
		{verb: "list", req: usersrpc.OwnerRequest{OwnerUserID: "1001"}, want: map[string]any{"grants": []any{map[string]any{"token": created.Token, "sections": []any{"commands"}, "delegate_login": "delegate", "consumed": true}}}},
		{verb: "access", req: usersrpc.AccessRequest{DelegateUserID: "2001"}, want: map[string]any{"grants": []any{map[string]any{"owner_user_id": "1001", "owner_login": "owner", "sections": []any{"commands"}}}}},
		{verb: "update", req: usersrpc.UpdateDelegationRequest{OwnerUserID: "1001", Token: created.Token, Sections: []string{"modules"}}, want: map[string]any{"ok": true, "delegate_user_id": "2001"}},
		{verb: "access", req: usersrpc.AccessRequest{DelegateUserID: "2001"}, want: map[string]any{"grants": []any{map[string]any{"owner_user_id": "1001", "owner_login": "owner", "sections": []any{"modules"}}}}},
		{verb: "revoke", req: usersrpc.RevokeDelegationRequest{OwnerUserID: "9999", Token: created.Token}, want: map[string]any{"ok": false, "error": "not found"}},
		{verb: "revoke", req: usersrpc.RevokeDelegationRequest{OwnerUserID: "1001", Token: created.Token}, want: map[string]any{"ok": true, "delegate_user_id": "2001"}},
		{verb: "access", req: usersrpc.AccessRequest{DelegateUserID: "2001"}, want: map[string]any{"grants": []any{}}},
	} {
		require.Equal(t, tc.want, call[map[string]any](t, h, "delegation.test."+tc.verb, tc.req), tc.verb)
	}
}
