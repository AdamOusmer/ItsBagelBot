// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"strconv"
	"testing"

	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/domain/consent"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func newMarketingHarness(t *testing.T) (harness, string) {
	t.Helper()
	h := newConsentHarness(t, newConsentMAC(t))
	require.NoError(t, rpc.SubscribeMarketing(rpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}, Repo: h.repo}, usersrpc.MarketingSubjectPrefix))
	require.NoError(t, h.nc.Flush())
	dashboardLogin(t, h, "contact@example.test")
	requireConsentReply(t, h, dashboardConsentRequest(ownerContext), "")
	id, err := strconv.ParseUint(consentOwnerID, 10, 64)
	require.NoError(t, err)
	token, err := h.repo.UnsubscribeToken(t.Context(), id)
	require.NoError(t, err)
	return h, token
}

func tokenStatus(t *testing.T, h harness, token string) usersrpc.MarketingTokenGetReply {
	t.Helper()
	return call[usersrpc.MarketingTokenGetReply](t, h, dashboardPrefix+".marketing_token_get", usersrpc.MarketingTokenGetRequest{Token: token})
}

func TestMarketingTokenVerbsRoundTrip(t *testing.T) {
	h, token := newMarketingHarness(t)
	got := tokenStatus(t, h, token)
	require.True(t, got.OK, "%+v", got.Refusal)
	require.Equal(t, consent.Granted, got.Status)
	require.Equal(t, "c***@example.test", got.MaskedEmail)

	for _, tc := range []struct {
		status consent.MarketingStatus
		source consent.Source
	}{
		{consent.Withdrawn, consent.OneClick},
		{consent.Withdrawn, consent.PreferencePage},
	} {
		set := usersrpc.MarketingTokenSetRequest{Token: token, Status: tc.status, Source: tc.source}
		reply := call[usersrpc.MarketingTokenSetReply](t, h, dashboardPrefix+".marketing_token_set", set)
		require.True(t, reply.OK, "%+v", reply.Refusal)
		require.Equal(t, tc.status, tokenStatus(t, h, token).Status)
	}
}

func TestMarketingTokenVerbsRefuseInvalid(t *testing.T) {
	h, token := newMarketingHarness(t)
	bad := token + "x"
	get := tokenStatus(t, h, bad)
	require.False(t, get.OK)
	require.Equal(t, domainrpc.CodeInvalid, get.Code)

	set := usersrpc.MarketingTokenSetRequest{Token: bad, Status: consent.Withdrawn, Source: consent.OneClick}
	reply := call[usersrpc.MarketingTokenSetReply](t, h, dashboardPrefix+".marketing_token_set", set)
	require.False(t, reply.OK)
	require.Equal(t, domainrpc.CodeInvalid, reply.Code)

	for _, set := range []usersrpc.MarketingTokenSetRequest{
		{Token: token, Status: consent.Withdrawn, Source: consent.Settings},
		{Token: token, Status: consent.Granted, Source: consent.PreferencePage},
		{Token: token, Status: consent.Granted, Source: consent.OneClick},
	} {
		reply = call[usersrpc.MarketingTokenSetReply](t, h, dashboardPrefix+".marketing_token_set", set)
		require.Equal(t, domainrpc.CodeInvalid, reply.Code, "%+v", set)
	}
	require.Equal(t, consent.Granted, tokenStatus(t, h, token).Status)
}

func TestMarketingSuppressAndAudienceRoundTrip(t *testing.T) {
	h, token := newMarketingHarness(t)
	audience := usersrpc.MarketingAudienceRequest{Limit: usersrpc.MarketingAudienceMaxPage}
	page := call[usersrpc.MarketingAudienceReply](t, h, usersrpc.MarketingAudienceSubject, audience)
	require.Empty(t, page.Error)
	require.Equal(t, []usersrpc.MarketingRecipient{{UserID: 1001, Locale: "en"}}, page.Recipients)

	suppress := usersrpc.MarketingSuppressRequest{Email: "contact@example.test", Reason: consent.SuppressComplaint}
	reply := call[usersrpc.MarketingSuppressReply](t, h, usersrpc.MarketingSuppressSubject, suppress)
	require.True(t, reply.OK, "%+v", reply.Refusal)
	require.Equal(t, consent.Withdrawn, tokenStatus(t, h, token).Status)

	page = call[usersrpc.MarketingAudienceReply](t, h, usersrpc.MarketingAudienceSubject, audience)
	require.Empty(t, page.Recipients)
}

func TestMarketingInternalVerbsRefuseInvalid(t *testing.T) {
	h, _ := newMarketingHarness(t)
	for _, reason := range []consent.SuppressionReason{consent.SuppressUnsubscribe, "other"} {
		suppress := usersrpc.MarketingSuppressRequest{Email: "contact@example.test", Reason: reason}
		reply := call[usersrpc.MarketingSuppressReply](t, h, usersrpc.MarketingSuppressSubject, suppress)
		require.Equal(t, domainrpc.CodeInvalid, reply.Code)
	}
	badEmail := usersrpc.MarketingSuppressRequest{Email: "nope", Reason: consent.SuppressComplaint}
	require.Equal(t, domainrpc.CodeInvalid, call[usersrpc.MarketingSuppressReply](t, h, usersrpc.MarketingSuppressSubject, badEmail).Code)

	for _, limit := range []int{0, usersrpc.MarketingAudienceMaxPage + 1} {
		page := call[usersrpc.MarketingAudienceReply](t, h, usersrpc.MarketingAudienceSubject, usersrpc.MarketingAudienceRequest{Limit: limit})
		require.Equal(t, domainrpc.CodeInvalid, page.Code)
	}
}
