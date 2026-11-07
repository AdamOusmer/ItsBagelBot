// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"errors"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/repository"

	"github.com/stretchr/testify/require"
)

func TestDeviceAuthRevokeAllKillsPendingCode(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5101)
	ctx := context.Background()

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5101, codeRequest(testVerifier))
	require.NoError(t, err)

	_, err = h.repo.RevokeAllDeviceGrants(ctx, 5101, "user")
	require.NoError(t, err)

	_, err = h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)

	grants, err := h.repo.ListDeviceGrants(ctx, 5101)
	require.NoError(t, err)
	require.Empty(t, grants, "a code issued before revoke-all must not mint a grant")
}

func TestDeviceAuthRevokeAllKeepsConsumedCodeForReplayDetection(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5102)
	ctx := context.Background()

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5102, codeRequest(testVerifier))
	require.NoError(t, err)
	_, err = h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.NoError(t, err)

	_, err = h.repo.RevokeAllDeviceGrants(ctx, 5102, "user")
	require.NoError(t, err)

	_, err = h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
}

func TestDeviceAuthOwnerLookupFailureIsNotInvalidGrant(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5103)
	ctx := context.Background()
	tokens := createAndExchange(t, h, 5103, testVerifier)

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5103, codeRequest(testVerifier))
	require.NoError(t, err)

	storageDown := errors.New("storage unavailable")
	h.client.User.Intercept(ent.InterceptFunc(func(ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(context.Context, ent.Query) (ent.Value, error) {
			return nil, storageDown
		})
	}))

	_, err = h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.ErrorIs(t, err, storageDown)
	require.NotErrorIs(t, err, repository.ErrDeviceInvalidGrant)

	_, err = h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.ErrorIs(t, err, storageDown)
	require.NotErrorIs(t, err, repository.ErrDeviceInvalidGrant)
}
