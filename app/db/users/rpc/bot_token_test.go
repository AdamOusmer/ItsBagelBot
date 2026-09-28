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
	"errors"
	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
	"net/http"
	"net/http/httptest"
	"testing"
)

const testBotID = "1061740761"

func grantFor(userID string, scopes ...string) grantValidator {
	return func(context.Context, string) (twitchGrant, error) {
		return twitchGrant{UserID: userID, Scopes: scopes}, nil
	}
}

func unreachableValidator(t *testing.T) grantValidator {
	return func(context.Context, string) (twitchGrant, error) {
		t.Fatal("grant validated before the request identity was checked")
		return twitchGrant{}, nil
	}
}

func botRequest() usersrpc.AdminRequest {
	return usersrpc.AdminRequest{ActorID: testBotID, UserID: testBotID, AccessToken: "test-access-token", RefreshToken: "test-refresh-token"}
}

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
	a.botUserID = testBotID
	var presented string
	a.validateBotGrant = func(_ context.Context, accessToken string) (twitchGrant, error) {
		presented = accessToken
		return twitchGrant{UserID: testBotID, Scopes: append([]string{"chat:read"}, botRequiredScopes...)}, nil
	}
	client.User.Create().SetID(1061740761).SetUsername("itsbagelbot").SetEmail("bot@example.invalid").ExecX(ctx)
	req := usersrpc.AdminRequest{ActorID: a.botUserID, UserID: a.botUserID, AccessToken: "test-access-token", RefreshToken: "test-refresh-token"}

	// No staff row is created; ordinary admin token writes still refuse this actor.
	regular := handlerFor(t, a, "token_set")(ctx, req)
	require.Equal(t, domainrpc.CodeForbidden, regular.Code)
	result := a.botTokenSet(ctx, req)
	require.Empty(t, result.Error)
	require.NotNil(t, result.Token)
	require.True(t, result.Token.Present)
	require.Equal(t, req.AccessToken, presented)
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
			a := &adminRPC{botUserID: tc.configured, validateBotGrant: unreachableValidator(t), log: zap.NewNop()}
			reply := a.botTokenSet(context.Background(), usersrpc.AdminRequest{ActorID: tc.actor, UserID: tc.target})
			require.Equal(t, domainrpc.CodeForbidden, reply.Code)
		})
	}
}

func TestBotTokenSaveRejectsIncompleteGrant(t *testing.T) {
	a := &adminRPC{botUserID: testBotID, validateBotGrant: unreachableValidator(t), log: zap.NewNop()}
	for _, req := range []usersrpc.AdminRequest{
		{ActorID: a.botUserID, UserID: a.botUserID, AccessToken: "test-access-token"},
		{ActorID: a.botUserID, UserID: a.botUserID, RefreshToken: "test-refresh-token"},
	} {
		require.Equal(t, domainrpc.CodeInvalid, a.botTokenSet(context.Background(), req).Code)
	}
}

func TestBotTokenSaveRequiresTwitchToConfirmTheGrant(t *testing.T) {
	for _, tc := range []struct {
		name     string
		validate grantValidator
		code     domainrpc.Code
	}{
		{"other account's token", grantFor("50728276", botRequiredScopes...), domainrpc.CodeForbidden},
		{"missing chat scope", grantFor(testBotID, "user:bot", "user:read:chat"), domainrpc.CodeForbidden},
		{"no scopes reported", grantFor(testBotID), domainrpc.CodeForbidden},
		{"token rejected by Twitch", func(context.Context, string) (twitchGrant, error) {
			return twitchGrant{}, errBotGrantRejected
		}, domainrpc.CodeForbidden},
		{"Twitch unreachable", func(context.Context, string) (twitchGrant, error) {
			return twitchGrant{}, errors.Join(errBotGrantUnverified, context.DeadlineExceeded)
		}, domainrpc.CodeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &adminRPC{botUserID: testBotID, validateBotGrant: tc.validate, log: zap.NewNop()}
			reply := a.botTokenSet(context.Background(), botRequest())
			require.Equal(t, tc.code, reply.Code)
			require.Nil(t, reply.Token)
		})
	}
}

func TestTwitchGrantValidatorReadsTheTokenOwner(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"client_id":"c","login":"itsbagelbot","user_id":"1061740761","scopes":["user:bot","chat:read"],"expires_in":3600}`))
	}))
	t.Cleanup(srv.Close)

	grant, err := twitchGrantValidator(srv.Client(), srv.URL)(context.Background(), "test-access-token")
	require.NoError(t, err)
	require.Equal(t, "OAuth test-access-token", auth)
	require.Equal(t, twitchGrant{UserID: testBotID, Scopes: []string{"user:bot", "chat:read"}}, grant)
}

func TestTwitchGrantValidatorClassifiesFailures(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   error
	}{
		{http.StatusUnauthorized, `{"status":401,"message":"invalid access token"}`, errBotGrantRejected},
		{http.StatusInternalServerError, ``, errBotGrantUnverified},
		{http.StatusOK, `not json`, errBotGrantUnverified},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		_, err := twitchGrantValidator(srv.Client(), srv.URL)(context.Background(), "test-access-token")
		srv.Close()
		require.ErrorIs(t, err, tc.want, "status %d", tc.status)
	}

	_, err := twitchGrantValidator(http.DefaultClient, "http://127.0.0.1:0")(context.Background(), "test-access-token")
	require.ErrorIs(t, err, errBotGrantUnverified)
}

func TestLogBotIdentityWarnsWhenUnset(t *testing.T) {
	core, logs := observer.New(zapcore.InfoLevel)
	logBotIdentity(zap.New(core), "")
	require.Equal(t, 1, logs.FilterLevelExact(zapcore.WarnLevel).FilterMessageSnippet("TWITCH_BOT_USER_ID is unset").Len())

	core, logs = observer.New(zapcore.InfoLevel)
	logBotIdentity(zap.New(core), testBotID)
	require.Zero(t, logs.FilterLevelExact(zapcore.WarnLevel).Len())
}
