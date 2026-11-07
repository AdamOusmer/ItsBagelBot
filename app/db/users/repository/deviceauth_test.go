// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/authorizationcode"
	"ItsBagelBot/app/db/users/ent/devicegrant"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/tokensign"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type deviceHarness struct {
	client *ent.Client
	repo   *repository.Users
	nc     *nats.Conn
	pub    ed25519.PublicKey
}

func setupDeviceAuth(t *testing.T) deviceHarness {
	t.Helper()
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })

	nc := testnats.Connect(t)
	repo.SetInvalidationConn(nc)

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := tokensign.NewSigner(priv.Seed(), "test-kid")
	require.NoError(t, err)
	repo.SetDeviceAuthSigner(signer)

	return deviceHarness{client: client, repo: repo, nc: nc, pub: pub}
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

const (
	testRedirect = "http://127.0.0.1:51123/callback"
	testVerifier = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
)

func createTestUser(t *testing.T, h deviceHarness, id uint64) {
	t.Helper()
	require.NoError(t, h.repo.Register(context.Background(), id, "user", "User", fmt.Sprintf("user-%d@example.invalid", id)))
}

func createAndExchange(t *testing.T, h deviceHarness, userID uint64, verifier string) repository.DeviceTokens {
	t.Helper()
	ctx := context.Background()
	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, userID, pkceChallenge(verifier), testRedirect, "Test Mac", "macos")
	require.NoError(t, err)
	tokens, err := h.repo.ExchangeDeviceCode(ctx, code, verifier, testRedirect)
	require.NoError(t, err)
	return tokens
}

func watchRevoked(t *testing.T, h deviceHarness) *nats.Subscription {
	t.Helper()
	sub, err := h.nc.SubscribeSync("controls.revoked.>")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return sub
}

func TestDeviceAuthHappyPath(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5001)

	tokens := createAndExchange(t, h, 5001, testVerifier)
	require.NotEmpty(t, tokens.AccessToken)
	require.Contains(t, tokens.RefreshToken, "bbr_")
	require.NotEmpty(t, tokens.GrantID)
	require.NotEmpty(t, tokens.DeviceID)

	payload, err := tokensign.Verify(h.pub, tokens.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "5001", payload.Sub)
	require.Equal(t, tokens.DeviceID, payload.DeviceID)
	require.Equal(t, tokens.GrantID, payload.GrantID)
	require.Equal(t, tokensign.Issuer, payload.Iss)
	require.Equal(t, tokensign.Audience, payload.Aud)
}

func TestDeviceAuthWrongVerifierDoesNotBurnCode(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5002)
	ctx := context.Background()

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5002, pkceChallenge(testVerifier), testRedirect, "Test PC", "windows")
	require.NoError(t, err)

	_, err = h.repo.ExchangeDeviceCode(ctx, code, "wrong-verifier-wrong-verifier-wrong-verifier", testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)

	tokens, err := h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.NoError(t, err, "a wrong verifier must not consume the code")
	require.NotEmpty(t, tokens.AccessToken)
}

func TestDeviceAuthExpiredCode(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5003)
	ctx := context.Background()

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5003, pkceChallenge(testVerifier), testRedirect, "", "linux")
	require.NoError(t, err)

	row := h.client.AuthorizationCode.Query().OnlyX(ctx)
	h.client.AuthorizationCode.UpdateOneID(row.ID).SetExpiresAt(time.Now().UTC().Add(-time.Minute)).ExecX(ctx)

	_, err = h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
}

func TestDeviceAuthReplayedCodeRevokesProducedGrant(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5004)
	ctx := context.Background()
	sub := watchRevoked(t, h)

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5004, pkceChallenge(testVerifier), testRedirect, "Rig", "macos")
	require.NoError(t, err)
	tokens, err := h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.NoError(t, err)

	_, err = h.repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)

	msg, err := sub.NextMsg(2 * time.Second)
	require.NoError(t, err, "replay must publish controls.revoked")
	require.Equal(t, "controls.revoked."+tokens.DeviceID, msg.Subject)

	grant := h.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	require.NotNil(t, grant.RevokedAt)

	_, err = h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant, "a revoked grant must not refresh")
}

func TestDeviceAuthRefreshGraceWindowRetryDoesNotRevoke(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5005)
	ctx := context.Background()

	tokens := createAndExchange(t, h, 5005, testVerifier)
	rotated, err := h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.NoError(t, err)

	_, err = h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant, "a near-immediate retry of the rotated token must fail")

	grant := h.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	require.Nil(t, grant.RevokedAt, "a grace-window retry must not revoke")

	again, err := h.repo.RefreshDeviceGrant(ctx, rotated.RefreshToken)
	require.NoError(t, err, "the real current token must still work")
	require.NotEmpty(t, again.AccessToken)
}

func TestDeviceAuthReplayedRefreshBeyondGraceRevokes(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5006)
	ctx := context.Background()
	sub := watchRevoked(t, h)

	tokens := createAndExchange(t, h, 5006, testVerifier)
	_, err := h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.NoError(t, err)

	row := h.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	past := time.Now().UTC().Add(-time.Minute)
	h.client.DeviceGrant.UpdateOneID(row.ID).SetPrevRotatedAt(past).ExecX(ctx)

	_, err = h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)

	msg, err := sub.NextMsg(2 * time.Second)
	require.NoError(t, err)
	require.Equal(t, "controls.revoked."+tokens.DeviceID, msg.Subject)

	grant := h.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	require.NotNil(t, grant.RevokedAt)
}

func TestDeviceAuthBannedUserFailsExchangeAndRefresh(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5007)
	ctx := context.Background()

	tokens := createAndExchange(t, h, 5007, testVerifier)

	require.NoError(t, h.repo.SetBanned(ctx, 5007, true))

	_, err := h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant, "ban must revoke existing grants")

	_, err = h.repo.CreateDeviceAuthorizationCode(ctx, 5007, pkceChallenge(testVerifier), testRedirect, "", "macos")
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant, "a banned account cannot start a new authorization")
}

func TestDeviceAuthCrossUserRevokeRefused(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5008)
	createTestUser(t, h, 5009)
	ctx := context.Background()

	tokens := createAndExchange(t, h, 5008, testVerifier)

	_, err := h.repo.RevokeDeviceGrant(ctx, 5009, tokens.GrantID)
	require.ErrorIs(t, err, repository.ErrDeviceGrantNotFound)

	grant := h.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	require.Nil(t, grant.RevokedAt, "a refused cross-user revoke must not touch the grant")

	deviceID, err := h.repo.RevokeDeviceGrant(ctx, 5008, tokens.GrantID)
	require.NoError(t, err)
	require.Equal(t, tokens.DeviceID, deviceID)
}

func TestDeviceAuthSignOutEverywhereRevokesAllGrants(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5010)
	ctx := context.Background()
	sub := watchRevoked(t, h)

	first := createAndExchange(t, h, 5010, testVerifier)
	second := createAndExchange(t, h, 5010, "second0123456789abcdefghijklmnopqrstuvwxyz")

	deviceIDs, err := h.repo.RevokeAllDeviceGrants(ctx, 5010, "user")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{first.DeviceID, second.DeviceID}, deviceIDs)

	for range deviceIDs {
		_, err := sub.NextMsg(2 * time.Second)
		require.NoError(t, err)
	}

	grants, err := h.repo.ListDeviceGrants(ctx, 5010)
	require.NoError(t, err)
	require.Empty(t, grants, "revoked grants must not be listed")

	_, err = h.repo.RefreshDeviceGrant(ctx, first.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
}

func TestDeviceAuthErasureDeletesGrantsAndCodes(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5011)
	ctx := context.Background()
	sub := watchRevoked(t, h)

	tokens := createAndExchange(t, h, 5011, testVerifier)

	require.NoError(t, h.repo.Delete(ctx, 5011))

	msg, err := sub.NextMsg(2 * time.Second)
	require.NoError(t, err, "erasure must still publish a revoke before the row disappears")
	require.Equal(t, "controls.revoked."+tokens.DeviceID, msg.Subject)

	count, err := h.client.DeviceGrant.Query().Where(devicegrant.UserIDEQ(5011)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count, "erasure must delete device grants")

	codes, err := h.client.AuthorizationCode.Query().Where(authorizationcode.UserIDEQ(5011)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, codes, "erasure must delete authorization codes")

	_, err = h.repo.RefreshDeviceGrant(ctx, tokens.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
}

func TestDeviceAuthMissingSigningKeyFailsClosedButRestWorks(t *testing.T) {
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	ctx := context.Background()

	require.NoError(t, repo.Register(ctx, 5012, "nosign", "No Sign", "nosign@example.invalid"))

	code, err := repo.CreateDeviceAuthorizationCode(ctx, 5012, pkceChallenge(testVerifier), testRedirect, "", "macos")
	require.NoError(t, err, "code creation does not need the signing key")

	_, err = repo.ExchangeDeviceCode(ctx, code, testVerifier, testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceSigningKeyUnavailable)

	_, err = repo.RefreshDeviceGrant(ctx, "bbr_anything")
	require.ErrorIs(t, err, repository.ErrDeviceSigningKeyUnavailable)

	require.NoError(t, repo.SetLocale(ctx, 5012, "fr"), "the rest of Users must keep working")
	require.NoError(t, repo.SetActiveNow(ctx, 5012, false))
}

func TestDeviceAuthCleanupPrunesExpiredCodesAndStaleGrants(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 5013)
	ctx := context.Background()
	now := time.Now().UTC()

	code, err := h.repo.CreateDeviceAuthorizationCode(ctx, 5013, pkceChallenge(testVerifier), testRedirect, "", "macos")
	require.NoError(t, err)
	staleCodeRow := h.client.AuthorizationCode.Query().OnlyX(ctx)
	h.client.AuthorizationCode.UpdateOneID(staleCodeRow.ID).SetExpiresAt(now.Add(-time.Hour)).ExecX(ctx)

	tokens := createAndExchange(t, h, 5013, "another0123456789abcdefghijklmnopqrstuvwxyz")
	_, err = h.repo.RevokeDeviceGrant(ctx, 5013, tokens.GrantID)
	require.NoError(t, err)
	staleGrantRow := h.client.DeviceGrant.Query().Where(devicegrant.GrantIDEQ(tokens.GrantID)).OnlyX(ctx)
	h.client.DeviceGrant.UpdateOneID(staleGrantRow.ID).SetRevokedAt(now.Add(-8 * 24 * time.Hour)).ExecX(ctx)

	_ = code
	deleted, err := h.repo.CleanupDeviceAuth(ctx, now)
	require.NoError(t, err)
	require.Equal(t, 2, deleted)

	remainingCodes, err := h.client.AuthorizationCode.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, remainingCodes, "the second, still-unexpired consumed code must survive the sweep")

	remainingGrants, err := h.client.DeviceGrant.Query().Count(ctx)
	require.NoError(t, err)
	require.Zero(t, remainingGrants, "the long-revoked grant must be pruned")
}
