// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/dbtest"
	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/enttest"
	"ItsBagelBot/app/db/users/ent/tokens"
	"ItsBagelBot/app/db/users/ent/user"
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

func TestDashboardConsentAcceptsPublishedTermsVersions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		code    domainrpc.Code
	}{
		{name: "September policy", version: "2026-09"},
		{name: "October policy", version: "2026-10"},
		{name: "current policy", version: consent.TermsVersion},
		{name: "missing version", code: domainrpc.CodeInvalid},
		{name: "unknown past policy", version: "1900-01", code: domainrpc.CodeInvalid},
		{name: "unknown future policy", version: "9999-12", code: domainrpc.CodeInvalid},
		{name: "malformed version", version: consent.TermsVersion + " ", code: domainrpc.CodeInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkDashboardConsentVersion(t, tc.version, tc.code)
		})
	}
}

func checkDashboardConsentVersion(t *testing.T, version string, code domainrpc.Code) {
	t.Helper()
	h := newConsentHarness(t, nil)
	dashboardLogin(t, h, "")
	req := dashboardConsentRequest(ownerContext)
	req.TermsVersion = version
	req.Marketing = false
	reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".consent_set", req)
	require.Equal(t, code, reply.Code)
	if code != domainrpc.CodeOK {
		requireDashboardConsent(t, h, "", consent.None)
		require.Zero(t, h.client.TermsAcceptance.Query().CountX(t.Context()))
		return
	}
	require.True(t, reply.OK, "%+v", reply.Refusal)
	requireDashboardConsent(t, h, version, consent.None)
	require.Equal(t, version, h.client.TermsAcceptance.Query().OnlyX(t.Context()).TermsVersion)
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

func TestDashboardStateMasksContactEmailOnRequest(t *testing.T) {
	h := newConsentHarness(t, nil)
	for _, tc := range []struct {
		name    string
		email   string
		include bool
		want    string
	}{
		{name: "placeholder is hidden", include: true},
		{name: "regular state has no email", email: "contact@example.test"},
		{name: "opt in reads mask", email: "contact@example.test", include: true, want: "c***@example.test"},
		{name: "one-character local part is fully hidden", email: "c@example.test", include: true, want: "***@example.test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dashboardLogin(t, h, tc.email)
			req := usersrpc.StateGetRequest{BroadcasterUserID: consentOwnerID, IncludeMaskedEmail: tc.include}
			state := call[usersrpc.StateGetReply](t, h, dashboardPrefix+".state_get", req)
			require.Empty(t, state.Error)
			require.Equal(t, tc.want, state.MaskedEmail)
		})
	}
}

func TestDashboardGrantAndTokenScopes(t *testing.T) {
	h := newConsentHarness(t, nil)
	dashboardLogin(t, h, "")
	const secondOwnerID = 1002
	secondOwner := usersrpc.UpsertUserRequest{UserID: strconv.FormatUint(secondOwnerID, 10), Username: "second_owner"}
	require.True(t, call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".upsert_user", secondOwner).OK)
	scopes := []string{"channel:bot", "user:read:chat"}
	otherScopes := []string{"channel:read:subscriptions"}
	empty := []string{}
	primaryID, err := strconv.ParseUint(consentOwnerID, 10, 64)
	require.NoError(t, err)

	for _, test := range []tokenScopesCase{
		{name: "first refresh with unknown scopes", ownerID: primaryID},
		{name: "grant supplies scopes", ownerID: primaryID, grant: true, save: scopes, want: scopes},
		{name: "second owner's grant has distinct scopes", ownerID: secondOwnerID, grant: true, save: otherScopes, want: otherScopes},
		{name: "omitted refresh scopes preserve grant scopes", ownerID: primaryID, want: scopes},
		{name: "omitted grant scopes clear the previous grant", ownerID: primaryID, grant: true},
		{name: "refresh can supply scopes", ownerID: primaryID, save: scopes, want: scopes},
		{name: "explicit empty remains empty on the wire", ownerID: primaryID, save: empty, want: empty},
		{name: "omitted refresh scopes preserve explicit empty", ownerID: primaryID, want: empty},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner := strconv.FormatUint(test.ownerID, 10)
			test.saveScopes(t, h, owner)
			get := usersrpc.TokensRequest{UserID: owner}
			reply := call[usersrpc.TokensReply](t, h, tokensPrefix+".get", get)
			require.Empty(t, reply.Error)
			require.Equal(t, test.want, reply.Scopes)
			missing := h.client.Tokens.Query().Where(tokens.HasUserWith(user.IDEQ(test.ownerID)), tokens.ScopesIsNil())
			require.Equal(t, test.want == nil, missing.ExistX(t.Context()))
		})
	}
}

type tokenScopesCase struct {
	name    string
	ownerID uint64
	grant   bool
	save    []string
	want    []string
}

func (test tokenScopesCase) saveScopes(t *testing.T, h harness, owner string) {
	t.Helper()
	if test.grant {
		req := usersrpc.GrantSaveRequest{BroadcasterUserID: owner, AccessToken: "grant-token", Scopes: test.save}
		reply := call[usersrpc.ConsentWriteReply](t, h, dashboardPrefix+".grant_save", req)
		require.True(t, reply.OK, "%+v", reply.Refusal)
		return
	}
	req := usersrpc.TokensRequest{UserID: owner, AccessToken: "refreshed-token", Scopes: test.save}
	require.Empty(t, call[usersrpc.TokensReply](t, h, tokensPrefix+".save", req).Error)
}
