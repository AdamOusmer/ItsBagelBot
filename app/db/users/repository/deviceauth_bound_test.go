// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"testing"
	"time"

	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/tokensign"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/dpop"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const boundTokenURL = "https://overlay.example/oauth/desktop/token"
const boundResourceURL = "https://overlay.example/bridge/v1/bind"

func boundCode(t *testing.T, h deviceHarness, key *dpop.Key) string {
	t.Helper()
	h.repo.SetDeviceAuthConfig(repository.DeviceAuthConfig{TokenURL: boundTokenURL, ResourceURLs: []string{boundResourceURL}})
	request := codeRequest(testVerifier)
	request.DPoPJKT = key.Thumbprint()
	code, err := h.repo.CreateDeviceAuthorizationCode(t.Context(), 7001, request)
	require.NoError(t, err)
	return code
}
func callBoundToken(t *testing.T, h deviceHarness, key *dpop.Key, request repository.DeviceOAuthRequest) repository.DeviceOAuthResult {
	t.Helper()
	request.ClientID = "itsbagelbot-desktop"
	request.URL = boundTokenURL
	proof, err := key.Proof("POST", boundTokenURL, "", "")
	require.NoError(t, err)
	request.Proof = proof
	result, err := h.repo.DeviceOAuth(t.Context(), request)
	require.NoError(t, err)
	require.Equal(t, "use_dpop_nonce", result.OAuthError)
	proof, err = key.Proof("POST", boundTokenURL, "", result.Nonce)
	require.NoError(t, err)
	request.Proof = proof
	result, err = h.repo.DeviceOAuth(t.Context(), request)
	require.NoError(t, err)
	return result
}
func exchangeBound(t *testing.T, h deviceHarness, key *dpop.Key, code string) repository.DeviceTokens {
	t.Helper()
	result := callBoundToken(t, h, key, repository.DeviceOAuthRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect})
	require.Empty(t, result.OAuthError)
	return result.Tokens
}
func authorizeBound(t *testing.T, h deviceHarness, key *dpop.Key, token string) (tokensign.Payload, error) {
	t.Helper()
	proof, err := key.Proof("POST", boundResourceURL, token, "")
	require.NoError(t, err)
	_, nonce, err := h.repo.AuthorizeDevice(t.Context(), token, proof, "POST", boundResourceURL)
	require.ErrorIs(t, err, repository.ErrDeviceNonce)
	proof, err = key.Proof("POST", boundResourceURL, token, nonce)
	require.NoError(t, err)
	claims, _, err := h.repo.AuthorizeDevice(t.Context(), token, proof, "POST", boundResourceURL)
	return claims, err
}
func TestBoundDeviceOAuthSealsAndRejectsBearerDowngrade(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 7001)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	code := boundCode(t, h, key)
	_, err = h.repo.ExchangeDeviceCode(t.Context(), code, testVerifier, testRedirect)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
	wrong, err := dpop.NewKey()
	require.NoError(t, err)
	rejected := callBoundToken(t, h, wrong, repository.DeviceOAuthRequest{GrantType: "authorization_code", Code: code, CodeVerifier: testVerifier, RedirectURI: testRedirect})
	require.Equal(t, "invalid_grant", rejected.OAuthError)
	tokens := exchangeBound(t, h, key, code)
	require.Equal(t, "DPoP", tokens.TokenType)
	require.EqualValues(t, 86400, tokens.ExpiresIn)
	require.Contains(t, tokens.AccessToken, "v2.")
	require.NotContains(t, tokens.AccessToken, "7001")
	_, err = tokensign.Verify(h.pub, tokens.AccessToken)
	require.Error(t, err)
	_, err = h.repo.RefreshDeviceGrant(t.Context(), tokens.RefreshToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
	claims, err := authorizeBound(t, h, key, tokens.AccessToken)
	require.NoError(t, err)
	require.Equal(t, "7001", claims.Sub)
	require.Equal(t, key.Thumbprint(), claims.Confirmation.JKT)
	_, err = authorizeBound(t, h, wrong, tokens.AccessToken)
	require.ErrorIs(t, err, repository.ErrDeviceProof)
}
func TestBoundDeviceRevocationBlocksReconnect(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 7001)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	tokens := exchangeBound(t, h, key, boundCode(t, h, key))
	_, err = h.repo.RevokeDeviceGrant(t.Context(), 7001, tokens.GrantID)
	require.NoError(t, err)
	_, err = authorizeBound(t, h, key, tokens.AccessToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
	refresh := callBoundToken(t, h, key, repository.DeviceOAuthRequest{GrantType: "refresh_token", RefreshToken: tokens.RefreshToken})
	require.Equal(t, "invalid_grant", refresh.OAuthError)
}
func TestBoundRefreshLostResponseRecovery(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 7001)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	tokens := exchangeBound(t, h, key, boundCode(t, h, key))
	request := repository.DeviceOAuthRequest{GrantType: "refresh_token", RefreshToken: tokens.RefreshToken}
	first := callBoundToken(t, h, key, request)
	require.Empty(t, first.OAuthError)
	retry := callBoundToken(t, h, key, request)
	require.Empty(t, retry.OAuthError)
	require.Equal(t, first.Tokens.RefreshToken, retry.Tokens.RefreshToken)
	require.Equal(t, first.Tokens.AccessToken, retry.Tokens.AccessToken)
	next := callBoundToken(t, h, key, repository.DeviceOAuthRequest{GrantType: "refresh_token", RefreshToken: retry.Tokens.RefreshToken})
	require.Empty(t, next.OAuthError)
	require.NotEqual(t, retry.Tokens.RefreshToken, next.Tokens.RefreshToken)
}
func TestBoundProofReplayAndFreshOwnerCheck(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 7001)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	tokens := exchangeBound(t, h, key, boundCode(t, h, key))
	proof, err := key.Proof("POST", boundResourceURL, tokens.AccessToken, "")
	require.NoError(t, err)
	_, nonce, err := h.repo.AuthorizeDevice(t.Context(), tokens.AccessToken, proof, "POST", boundResourceURL)
	require.ErrorIs(t, err, repository.ErrDeviceNonce)
	proof, err = key.Proof("POST", boundResourceURL, tokens.AccessToken, nonce)
	require.NoError(t, err)
	_, _, err = h.repo.AuthorizeDevice(t.Context(), tokens.AccessToken, proof, "POST", boundResourceURL)
	require.NoError(t, err)
	_, _, err = h.repo.AuthorizeDevice(t.Context(), tokens.AccessToken, proof, "POST", boundResourceURL)
	require.ErrorIs(t, err, repository.ErrDeviceProof)
	require.NoError(t, h.client.User.UpdateOneID(7001).SetBanned(true).Exec(context.Background()))
	_, err = authorizeBound(t, h, key, tokens.AccessToken)
	require.ErrorIs(t, err, repository.ErrDeviceInvalidGrant)
	_, err = h.repo.CleanupDeviceAuth(t.Context(), time.Now().UTC().Add(6*time.Minute))
	require.NoError(t, err)
	count, err := h.client.DeviceProof.Query().Count(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestBoundCodeLostResponseRecovery(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 7001)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	code := boundCode(t, h, key)
	first := exchangeBound(t, h, key, code)
	retry := exchangeBound(t, h, key, code)
	require.Equal(t, first.RefreshToken, retry.RefreshToken)
	require.Equal(t, first.AccessToken, retry.AccessToken)
	count, err := h.client.DeviceGrant.Query().Count(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
}

func TestBoundProofReceiptSurvivesRepositoryReplacement(t *testing.T) {
	h := setupDeviceAuth(t)
	createTestUser(t, h, 7001)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	tokens := exchangeBound(t, h, key, boundCode(t, h, key))
	proof, err := key.Proof("POST", boundResourceURL, tokens.AccessToken, "")
	require.NoError(t, err)
	_, nonce, err := h.repo.AuthorizeDevice(t.Context(), tokens.AccessToken, proof, "POST", boundResourceURL)
	require.ErrorIs(t, err, repository.ErrDeviceNonce)
	proof, err = key.Proof("POST", boundResourceURL, tokens.AccessToken, nonce)
	require.NoError(t, err)
	_, _, err = h.repo.AuthorizeDevice(t.Context(), tokens.AccessToken, proof, "POST", boundResourceURL)
	require.NoError(t, err)
	replacement := repository.NewUsers(h.client, h.packer, bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { replacement.Close(context.Background()) })
	replacement.SetDeviceAuthSigner(h.signer)
	replacement.SetDeviceAuthConfig(repository.DeviceAuthConfig{TokenURL: boundTokenURL, ResourceURLs: []string{boundResourceURL}})
	_, _, err = replacement.AuthorizeDevice(t.Context(), tokens.AccessToken, proof, "POST", boundResourceURL)
	require.ErrorIs(t, err, repository.ErrDeviceProof)
	fresh, err := key.Proof("POST", boundResourceURL, tokens.AccessToken, nonce)
	require.NoError(t, err)
	_, _, err = replacement.AuthorizeDevice(t.Context(), tokens.AccessToken, fresh, "POST", boundResourceURL)
	require.NoError(t, err)
}
