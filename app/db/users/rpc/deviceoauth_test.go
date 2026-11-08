// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"testing"
	"time"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/app/db/users/tokensign"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"
	"ItsBagelBot/pkg/dpop"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestNativeOAuthRPCIsSeparateFromDashboard(t *testing.T) {
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(client, dbtest.NewPacker(t), bustest.NewPublisher(), nil, zap.NewNop())
	t.Cleanup(func() { repo.Close(context.Background()) })
	signer, err := tokensign.NewSigner(make([]byte, 32), "test")
	require.NoError(t, err)
	repo.SetDeviceAuthSigner(signer)
	const tokenURL = "https://overlay.example/oauth/desktop/token"
	repo.SetDeviceAuthConfig(repository.DeviceAuthConfig{TokenURL: tokenURL})
	nc := testnats.Connect(t)
	wiring := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Queue: "device-auth", Log: zap.NewNop()}, Repo: repo}
	require.NoError(t, rpc.SubscribeDeviceAuth(wiring, "dashboard.device"))
	require.NoError(t, rpc.SubscribeDeviceOAuth(wiring, "bagel.rpc.overlay.device"))
	require.NoError(t, nc.Flush())
	_, err = nc.Request("dashboard.device.refresh", []byte(`{}`), time.Second)
	require.ErrorIs(t, err, nats.ErrNoResponders)
	key, err := dpop.NewKey()
	require.NoError(t, err)
	proof, err := key.Proof("POST", tokenURL, "", "")
	require.NoError(t, err)
	request := usersrpc.DeviceOAuthRequest{ClientID: "itsbagelbot-desktop", GrantType: "refresh_token", RefreshToken: "bbr_unknown", Proof: proof, URL: tokenURL}
	h := harness{nc: nc, client: client}
	challenge := call[usersrpc.DeviceOAuthReply](t, h, "bagel.rpc.overlay.device.token", request)
	require.Equal(t, "use_dpop_nonce", challenge.OAuthError)
	require.NotEmpty(t, challenge.Nonce)
	request.Proof, err = key.Proof("POST", tokenURL, "", challenge.Nonce)
	require.NoError(t, err)
	refusal := call[usersrpc.DeviceOAuthReply](t, h, "bagel.rpc.overlay.device.token", request)
	require.Equal(t, "invalid_grant", refusal.OAuthError)
	require.Empty(t, refusal.AccessToken)
	require.NoError(t, repo.Register(t.Context(), 9001, "device", "Device", "device@example.invalid"))
	verifier := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	hash := sha256.Sum256([]byte(verifier))
	code, err := repo.CreateDeviceAuthorizationCode(t.Context(), 9001, repository.DeviceCodeRequest{Challenge: base64.RawURLEncoding.EncodeToString(hash[:]), RedirectURI: "http://127.0.0.1:51123/callback", Platform: "macos", DPoPJKT: key.Thumbprint()})
	require.NoError(t, err)
	request.GrantType = "authorization_code"
	request.Code = code
	request.CodeVerifier = verifier
	request.RedirectURI = "http://127.0.0.1:51123/callback"
	request.RefreshToken = ""
	request.Proof, err = key.Proof("POST", tokenURL, "", challenge.Nonce)
	require.NoError(t, err)
	issued := call[usersrpc.DeviceOAuthReply](t, h, "bagel.rpc.overlay.device.token", request)
	require.Empty(t, issued.OAuthError)
	require.Empty(t, issued.Error)
	require.Equal(t, "DPoP", issued.TokenType)
	require.EqualValues(t, 86400, issued.ExpiresIn)
	require.Contains(t, issued.AccessToken, "v2.")
	require.NotEmpty(t, issued.RefreshToken)
}
