// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/repository"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/crypto"
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"go.uber.org/zap"
	"testing"
)

func TestBotTokenSaveWithoutStaffAccount(t *testing.T) {
	a, client := setupAdminRPCTest(t)
	ctx := context.Background()
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	require.NoError(t, err)
	buf := new(bytes.Buffer)
	require.NoError(t, insecurecleartextkeyset.Write(handle, keyset.NewJSONWriter(buf)))
	packer, err := crypto.NewCrypto(buf.Bytes())
	require.NoError(t, err)
	a.repo = repository.NewUsers(client, packer, nil, nil, zap.NewNop())
	t.Cleanup(func() { a.repo.Close(ctx) })
	a.gate = staffGate{db: client}
	a.botUserID = "1061740761"
	client.User.Create().SetID(1061740761).SetUsername("itsbagelbot").SetEmail("bot@example.invalid").ExecX(ctx)
	req := usersrpc.AdminRequest{ActorID: a.botUserID, UserID: a.botUserID, AccessToken: "test-access-token", RefreshToken: "test-refresh-token"}

	// No staff row is created; ordinary admin token writes still refuse this actor.
	regular := handlerFor(t, a, "token_set")(ctx, req)
	require.Equal(t, domainrpc.CodeForbidden, regular.Code)
	result := a.botTokenSet(ctx, req)
	require.Empty(t, result.Error)
	require.NotNil(t, result.Token)
	require.True(t, result.Token.Present)
	access, refresh, _, err := a.repo.Token(ctx, 1061740761, tokens.TypeUserToken, tokens.PlatformTwitch)
	require.NoError(t, err)
	require.Equal(t, req.AccessToken, string(access))
	require.Equal(t, req.RefreshToken, string(refresh))
	require.Zero(t, client.AdminUser.Query().CountX(ctx))
}

func TestBotTokenSaveRejectsOtherTargetsAndMissingConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, configured, actor, target string }{
		{"unset identity", "", "1061740761", "1061740761"},
		{"other target", "1061740761", "1061740761", "50728276"},
		{"other actor", "1061740761", "50728276", "1061740761"},
		{"missing actor", "1061740761", "", "1061740761"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &adminRPC{botUserID: tc.configured}
			reply := a.botTokenSet(context.Background(), usersrpc.AdminRequest{ActorID: tc.actor, UserID: tc.target})
			require.Equal(t, domainrpc.CodeForbidden, reply.Code)
		})
	}
}

func TestBotTokenSaveRejectsIncompleteGrant(t *testing.T) {
	a := &adminRPC{botUserID: "1061740761"}
	for _, req := range []usersrpc.AdminRequest{
		{ActorID: a.botUserID, UserID: a.botUserID, AccessToken: "test-access-token"},
		{ActorID: a.botUserID, UserID: a.botUserID, RefreshToken: "test-refresh-token"},
	} {
		require.Equal(t, domainrpc.CodeInvalid, a.botTokenSet(context.Background(), req).Code)
	}
}
