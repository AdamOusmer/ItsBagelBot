// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHandleGetReplyCodes(t *testing.T) {
	client := testdb.Open(t, "tokens-get-codes", func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, nil, nil, nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	tr := &tokensRPC{repo: repo, log: zap.NewNop()}

	t.Run("missing token is not_found", func(t *testing.T) {
		reply, err := tr.handleGet(context.Background(), usersrpc.TokensRequest{}, 1001)
		require.NoError(t, err)
		require.Equal(t, domainrpc.CodeNotFound, reply.Code)
	})

	require.NoError(t, client.Close())
	t.Run("database failure is not not_found", func(t *testing.T) {
		reply, err := tr.handleGet(context.Background(), usersrpc.TokensRequest{}, 1002)
		require.NoError(t, err)
		require.NotEqual(t, domainrpc.CodeNotFound, reply.Code)
		require.NotEmpty(t, reply.Code)
	})
}
