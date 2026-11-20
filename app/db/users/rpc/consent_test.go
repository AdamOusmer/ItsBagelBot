// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/domain/consent"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/internal/testdb"
	"ItsBagelBot/internal/testnats"
	"ItsBagelBot/pkg/bus"
	"ItsBagelBot/pkg/bus/bustest"

	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
	"go.uber.org/zap"
)

const (
	dashboardPrefix = "rpc.dashboard"
	tokensPrefix    = "rpc.tokens"
	consentOwnerID  = "1001"
)

var ownerContext = usersrpc.DashboardOwner{BroadcasterUserID: consentOwnerID, ActorID: consentOwnerID}

func newConsentHarness(t *testing.T) harness {
	t.Helper()
	handle, err := keyset.NewHandle(mac.HMACSHA256Tag256KeyTemplate())
	require.NoError(t, err)
	addressMAC, err := mac.New(handle)
	require.NoError(t, err)
	client := testdb.Open(t, testdb.Name(t.Name()), func(d, dsn string) *ent.Client { return enttest.Open(t, d, dsn) })
	repo := repository.NewUsers(repository.UsersConfig{
		Client: client, Packer: dbtest.NewPacker(t), MarketingMAC: addressMAC, Pub: bustest.NewPublisher(),
	})
	t.Cleanup(func() { repo.Close(context.Background()) })
	nc := testnats.Connect(t)
	wiring := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: nc, Log: zap.NewNop()}, Repo: repo}
	require.NoError(t, rpc.SubscribeDashboard(wiring, dashboardPrefix))
	require.NoError(t, rpc.SubscribeTokens(wiring, tokensPrefix))
	require.NoError(t, nc.Flush())

	return harness{nc: nc, client: client, repo: repo}
}

func dashboardLogin(t *testing.T, h harness, email string) {
	t.Helper()
	reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".upsert_user", usersrpc.UpsertUserRequest{
		UserID: consentOwnerID, Username: "owner", Email: email,
	})
	require.True(t, reply.OK, "%+v", reply.Refusal)
}

func dashboardConsentRequest(owner usersrpc.DashboardOwner) usersrpc.ConsentSetRequest {
	return usersrpc.ConsentSetRequest{
		DashboardOwner: owner, TermsVersion: consent.TermsVersion,
		Source: consent.Consent, Locale: "fr", RequestID: "dashboard-consent-1",
		Marketing: true,
	}
}

func TestDashboardConsentVerbsRejectNonOwners(t *testing.T) {
	h := newConsentHarness(t)
	dashboardLogin(t, h, "contact@example.test")

	for _, test := range []struct {
		name  string
		owner usersrpc.DashboardOwner
	}{
		{"missing actor", usersrpc.DashboardOwner{BroadcasterUserID: consentOwnerID}},
		{"different actor", usersrpc.DashboardOwner{BroadcasterUserID: consentOwnerID, ActorID: "1002"}},
		{"delegated session", usersrpc.DashboardOwner{BroadcasterUserID: consentOwnerID, ActorID: consentOwnerID, DelegateOf: "1002"}},
		{"impersonated session", usersrpc.DashboardOwner{BroadcasterUserID: consentOwnerID, ActorID: consentOwnerID, ImpersonatorID: "1002"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := dashboardConsentRequest(test.owner)
			marketing := usersrpc.MarketingSetRequest{DashboardOwner: test.owner, Status: consent.Granted, Source: consent.Settings}
			termsReply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".consent_set", req)
			marketingReply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".marketing_set", marketing)
			require.Equal(t, domainrpc.CodeForbidden, termsReply.Code)
			require.Equal(t, domainrpc.CodeForbidden, marketingReply.Code)
		})
	}

	require.Zero(t, h.client.TermsAcceptance.Query().CountX(t.Context()))
	require.Zero(t, h.client.MarketingConsent.Query().CountX(t.Context()))
}

func TestDashboardConsentRoundTrip(t *testing.T) {
	h := newConsentHarness(t)
	dashboardLogin(t, h, "")
	req := dashboardConsentRequest(ownerContext)
	missingAddress := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".consent_set", req)
	require.Equal(t, domainrpc.CodeInvalid, missingAddress.Code)
	requireDashboardConsent(t, h, "", consent.None)

	dashboardLogin(t, h, "contact@example.test")
	for range 2 {
		reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".consent_set", req)
		require.True(t, reply.OK, "%+v", reply.Refusal)
	}
	requireDashboardConsent(t, h, req.TermsVersion, consent.Granted)
	require.Equal(t, 1, h.client.TermsAcceptance.Query().CountX(t.Context()))
	require.Equal(t, 1, h.client.MarketingConsent.Query().CountX(t.Context()))

	dashboardLogin(t, h, "changed@example.test")
	requireDashboardConsent(t, h, req.TermsVersion, consent.Withdrawn)

	for _, status := range []consent.MarketingStatus{consent.Granted, consent.Withdrawn} {
		write := usersrpc.MarketingSetRequest{DashboardOwner: ownerContext, Status: status, Source: consent.Settings}
		reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".marketing_set", write)
		require.True(t, reply.OK, "%+v", reply.Refusal)
		requireDashboardConsent(t, h, req.TermsVersion, status)
	}
}

func requireDashboardConsent(t *testing.T, h harness, version string, status consent.MarketingStatus) {
	t.Helper()
	state := call[usersrpc.StateGetReply](t, h, dashboardPrefix+".state_get", usersrpc.StateGetRequest{BroadcasterUserID: consentOwnerID})
	require.Empty(t, state.Error)
	require.Equal(t, version, state.TermsVersion)
	require.Equal(t, status, state.MarketingStatus)
}

func TestDashboardGrantAndTokenScopes(t *testing.T) {
	h := newConsentHarness(t)
	dashboardLogin(t, h, "")
	scopes := []string{"channel:bot", "user:read:chat"}
	get := usersrpc.TokensRequest{UserID: consentOwnerID}
	firstSave := usersrpc.TokensRequest{UserID: consentOwnerID, AccessToken: "unknown-scope-token"}
	reply := call[usersrpc.TokensReply](t, h, tokensPrefix+".save", firstSave)
	require.Empty(t, reply.Error)
	require.Nil(t, call[usersrpc.TokensReply](t, h, tokensPrefix+".get", get).Scopes)

	grant := usersrpc.GrantSaveRequest{BroadcasterUserID: consentOwnerID, AccessToken: "grant-token", Scopes: scopes}
	saved := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".grant_save", grant)
	require.True(t, saved.OK, "%+v", saved.Refusal)
	require.Equal(t, scopes, call[usersrpc.TokensReply](t, h, tokensPrefix+".get", get).Scopes)

	for _, test := range []struct {
		name string
		save []string
		want []string
	}{
		{"omitted scopes keep grant scopes", nil, scopes},
		{"explicit empty remains empty on the wire", []string{}, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := usersrpc.TokensRequest{UserID: consentOwnerID, AccessToken: "refreshed-token", Scopes: test.save}
			require.Empty(t, call[usersrpc.TokensReply](t, h, tokensPrefix+".save", req).Error)
			require.Equal(t, test.want, call[usersrpc.TokensReply](t, h, tokensPrefix+".get", get).Scopes)
		})
	}
}
