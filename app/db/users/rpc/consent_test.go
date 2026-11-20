// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/tokens"
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
	"github.com/tink-crypto/tink-go/v2/tink"
	"go.uber.org/zap"
)

const (
	dashboardPrefix = "rpc.dashboard"
	tokensPrefix    = "rpc.tokens"
	consentOwnerID  = "1001"
)

var ownerContext = usersrpc.DashboardOwner{BroadcasterUserID: consentOwnerID, ActorID: consentOwnerID}

func newConsentHarness(t *testing.T, addressMAC tink.MAC) harness {
	t.Helper()
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

func newConsentMAC(t *testing.T) tink.MAC {
	t.Helper()
	handle, err := keyset.NewHandle(mac.HMACSHA256Tag256KeyTemplate())
	require.NoError(t, err)
	addressMAC, err := mac.New(handle)
	require.NoError(t, err)
	return addressMAC
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
	h := newConsentHarness(t, nil)
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
	h := newConsentHarness(t, newConsentMAC(t))
	dashboardLogin(t, h, "contact@example.test")
	req := dashboardConsentRequest(ownerContext)
	for range 2 {
		requireConsentReply(t, h, req, "")
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

func TestDashboardConsentRecordsTermsWhenMarketingIsSkipped(t *testing.T) {
	addressMAC := newConsentMAC(t)
	for _, tc := range []struct {
		name  string
		email string
		mac   tink.MAC
		want  consent.MarketingSkipReason
	}{
		{name: "missing contact email", mac: addressMAC, want: consent.MarketingSkipNoEmail},
		{name: "missing keyset", email: "contact@example.test", want: consent.MarketingSkipUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newConsentHarness(t, tc.mac)
			dashboardLogin(t, h, tc.email)
			req := dashboardConsentRequest(ownerContext)
			requireConsentReply(t, h, req, tc.want)
			requireDashboardConsent(t, h, req.TermsVersion, consent.None)

			dashboardLogin(t, h, "contact@example.test")
			requireConsentReply(t, h, req, tc.want)
			require.Equal(t, 1, h.client.TermsAcceptance.Query().CountX(t.Context()))
			require.Zero(t, h.client.MarketingConsent.Query().CountX(t.Context()))
		})
	}
}

func TestDashboardConsentRejectsOtherTermsVersions(t *testing.T) {
	h := newConsentHarness(t, nil)
	dashboardLogin(t, h, "")
	for _, version := range []string{"", "1900-01", "9999-12", consent.TermsVersion + " "} {
		req := dashboardConsentRequest(ownerContext)
		req.TermsVersion = version
		reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".consent_set", req)
		require.Equal(t, domainrpc.CodeInvalid, reply.Code, "version %q", version)
	}
	requireDashboardConsent(t, h, "", consent.None)
	require.Zero(t, h.client.TermsAcceptance.Query().CountX(t.Context()))
}

func requireConsentReply(t *testing.T, h harness, req usersrpc.ConsentSetRequest, skipped consent.MarketingSkipReason) {
	t.Helper()
	reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".consent_set", req)
	require.Equal(t, usersrpc.ConsentWriteReply{OK: true, MarketingSkipped: skipped}, reply)
}

func requireDashboardConsent(t *testing.T, h harness, version string, status consent.MarketingStatus) {
	t.Helper()
	state := call[usersrpc.StateGetReply](t, h, dashboardPrefix+".state_get", usersrpc.StateGetRequest{BroadcasterUserID: consentOwnerID})
	require.Empty(t, state.Error)
	require.Equal(t, version, state.TermsVersion)
	require.Equal(t, status, state.MarketingStatus)
}

func TestDashboardGrantAndTokenScopes(t *testing.T) {
	h := newConsentHarness(t, nil)
	dashboardLogin(t, h, "")
	scopes := []string{"channel:bot", "user:read:chat"}
	empty := []string{}
	get := usersrpc.TokensRequest{UserID: consentOwnerID}

	for _, test := range []struct {
		name  string
		grant bool
		save  []string
		want  []string
	}{
		{name: "first refresh with unknown scopes"},
		{name: "grant supplies scopes", grant: true, save: scopes, want: scopes},
		{name: "omitted refresh scopes preserve grant scopes", want: scopes},
		{name: "omitted grant scopes clear the previous grant", grant: true},
		{name: "refresh can supply scopes", save: scopes, want: scopes},
		{name: "explicit empty remains empty on the wire", save: empty, want: empty},
		{name: "omitted refresh scopes preserve explicit empty", want: empty},
	} {
		t.Run(test.name, func(t *testing.T) {
			saveTokenScopes(t, h, test.grant, test.save)
			reply := call[usersrpc.TokensReply](t, h, tokensPrefix+".get", get)
			require.Empty(t, reply.Error)
			require.Equal(t, test.want, reply.Scopes)
			require.Equal(t, test.want == nil, h.client.Tokens.Query().Where(tokens.ScopesIsNil()).ExistX(t.Context()))
		})
	}
}

func saveTokenScopes(t *testing.T, h harness, grant bool, scopes []string) {
	t.Helper()
	if grant {
		req := usersrpc.GrantSaveRequest{BroadcasterUserID: consentOwnerID, AccessToken: "grant-token", Scopes: scopes}
		reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".grant_save", req)
		require.True(t, reply.OK, "%+v", reply.Refusal)
		return
	}
	req := usersrpc.TokensRequest{UserID: consentOwnerID, AccessToken: "refreshed-token", Scopes: scopes}
	require.Empty(t, call[usersrpc.TokensReply](t, h, tokensPrefix+".save", req).Error)
}
