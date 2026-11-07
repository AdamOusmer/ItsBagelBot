// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"sync"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/tokensign"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/pkg/bus/bustest"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const deviceAuthConcurrentCallers = 16

func newMySQLDeviceHarness(t *testing.T, prefix string) (*ent.Client, *repository.Users) {
	t.Helper()
	raw := testdb.MySQL(t, prefix)
	client := ent.NewClient(ent.Driver(entsql.OpenDB("mysql", raw)))
	require.NoError(t, client.Schema.Create(t.Context()))
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := tokensign.NewSigner(priv.Seed(), "mysql-test-kid")
	require.NoError(t, err)
	repo.SetDeviceAuthSigner(signer)
	return client, repo
}

// TestDeviceAuthMySQLConcurrentCodeExchangeNoDoubleSpend proves a single
// authorization code mints exactly one grant under real concurrent exchange,
// with every other racer reported as a replay.
func TestDeviceAuthMySQLConcurrentCodeExchangeNoDoubleSpend(t *testing.T) {
	client, repo := newMySQLDeviceHarness(t, "bagel_device_exchange_")
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 6001, "racer", "Racer", "racer@example.invalid"))

	code, err := repo.CreateDeviceAuthorizationCode(ctx, 6001, pkceChallenge(testVerifier), testRedirect, "Race Rig", "macos")
	require.NoError(t, err)

	var wg sync.WaitGroup
	successes := make([]bool, deviceAuthConcurrentCallers)
	errs := make([]error, deviceAuthConcurrentCallers)
	start := time.Now()
	for i := range deviceAuthConcurrentCallers {
		wg.Go(func() {
			tokens, err := repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
			successes[i] = err == nil && tokens.AccessToken != ""
			errs[i] = err
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("device code exchange: %d concurrent callers on local MySQL in %s", deviceAuthConcurrentCallers, elapsed)

	won := 0
	for i := range deviceAuthConcurrentCallers {
		if successes[i] {
			won++
			continue
		}
		require.ErrorIs(t, errs[i], repository.ErrDeviceInvalidGrant)
	}
	assert.Equal(t, 1, won, "exactly one concurrent exchange of the same code may succeed")

	count, err := client.DeviceGrant.Query().Count(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "a replayed code must never mint a second grant")

	grant := client.DeviceGrant.Query().OnlyX(ctx)
	assert.NotNil(t, grant.RevokedAt, "the replay must be detected and the produced grant revoked")
}

// TestDeviceAuthMySQLConcurrentRefreshNoDoubleSpend proves presenting the
// same still-current refresh token concurrently rotates exactly once; every
// other racer lands in the grace window and the grant stays usable.
func TestDeviceAuthMySQLConcurrentRefreshNoDoubleSpend(t *testing.T) {
	client, repo := newMySQLDeviceHarness(t, "bagel_device_refresh_")
	ctx := t.Context()
	require.NoError(t, repo.Register(ctx, 6002, "racer2", "Racer2", "racer2@example.invalid"))

	code, err := repo.CreateDeviceAuthorizationCode(ctx, 6002, pkceChallenge(testVerifier), testRedirect, "Race Rig 2", "windows")
	require.NoError(t, err)
	tokens, err := repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.NoError(t, err)

	var wg sync.WaitGroup
	rotated := make([]repository.DeviceTokens, deviceAuthConcurrentCallers)
	errs := make([]error, deviceAuthConcurrentCallers)
	start := time.Now()
	for i := range deviceAuthConcurrentCallers {
		wg.Go(func() {
			rotated[i], errs[i] = repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
		})
	}
	wg.Wait()
	elapsed := time.Since(start)
	t.Logf("device refresh rotation: %d concurrent callers on local MySQL in %s", deviceAuthConcurrentCallers, elapsed)

	won := 0
	var winner repository.DeviceTokens
	for i := range deviceAuthConcurrentCallers {
		if errs[i] == nil {
			won++
			winner = rotated[i]
			continue
		}
		require.ErrorIs(t, errs[i], repository.ErrDeviceInvalidGrant)
	}
	assert.Equal(t, 1, won, "exactly one concurrent refresh of the same token may rotate")

	grant := client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	assert.Nil(t, grant.RevokedAt, "a concurrent race on the current token is the grace window, not a reuse revoke")

	again, err := repo.RefreshDeviceGrant(ctx, winner.RefreshToken)
	require.NoError(t, err, "the surviving rotated token must still work")
	require.NotEmpty(t, again.AccessToken)
}
