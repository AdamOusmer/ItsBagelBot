// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc_test

import (
	"context"
	"strconv"
	"testing"

	"ItsBagelBot/app/db/users/rpc"
	"ItsBagelBot/internal/adminauth"
	"ItsBagelBot/internal/domain/consent"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	usersrpc "ItsBagelBot/internal/domain/rpc/users"
	"ItsBagelBot/pkg/bus"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

const campaignPrefix = "admin.marketing"

type recordingTester struct {
	campaign int
	user     uint64
}

func (r *recordingTester) SendTest(_ context.Context, campaignID int, userID uint64) error {
	r.campaign, r.user = campaignID, userID
	return nil
}

func campaignHarness(t *testing.T, tester rpc.CampaignTester) harness {
	t.Helper()
	h := newHarness(t)
	h.staff(
		staffFixture{modActor, adminauth.Moderator, true},
		staffFixture{adminActor, adminauth.Admin, true},
		staffFixture{ownerActor, adminauth.Owner, true},
	)
	wiring := rpc.Wiring{RPCWiring: bus.RPCWiring{NC: h.nc, Log: zap.NewNop()}, Repo: h.repo, Campaigns: tester}
	require.NoError(t, rpc.SubscribeCampaigns(wiring, rpc.CampaignConfig{AuthPrefix: authPrefix, Prefix: campaignPrefix}))
	return h
}

func (h harness) campaign(t *testing.T, verb string, actor uint64, req usersrpc.CampaignRequest) usersrpc.CampaignReply {
	t.Helper()
	req.ActorID = strconv.FormatUint(actor, 10)
	return call[usersrpc.CampaignReply](t, h, campaignPrefix+"."+verb, req)
}

var rpcContent = map[string]consent.CampaignContent{"en": {Subject: "News", HTML: "<p>Hi</p>", Text: "Hi"}}

func TestCampaignVerbsRequireTheirMinimumRole(t *testing.T) {
	h := campaignHarness(t, &recordingTester{})
	for _, tc := range []struct {
		verb string
		min  int
	}{
		{"campaign_list", 1}, {"campaign_get", 1}, {"campaign_save", 1}, {"campaign_preview", 1},
		{"campaign_test", 1}, {"campaign_approve", 2}, {"campaign_pause", 1}, {"campaign_resume", 1},
	} {
		for rank, actor := range []uint64{modActor, adminActor, ownerActor} {
			reply := h.campaign(t, tc.verb, actor, usersrpc.CampaignRequest{ID: 999})
			require.Equal(t, rank < tc.min, reply.Code == domainrpc.CodeForbidden, "%s actor %d: %s", tc.verb, actor, reply.Error)
		}
	}
}

func TestCampaignReviewFlowOverRPC(t *testing.T) {
	h := campaignHarness(t, &recordingTester{})
	saved := h.campaign(t, "campaign_save", adminActor, usersrpc.CampaignRequest{Name: "Launch", Content: rpcContent})
	require.True(t, saved.OK, saved.Error)
	id := saved.Campaign.ID

	stale := h.campaign(t, "campaign_approve", ownerActor, usersrpc.CampaignRequest{ID: id, Revision: saved.Campaign.Revision + 1})
	require.Equal(t, domainrpc.CodeConflict, stale.Code)
	approved := h.campaign(t, "campaign_approve", ownerActor, usersrpc.CampaignRequest{ID: id, Revision: saved.Campaign.Revision})
	require.Equal(t, consent.CampaignApproved, approved.Campaign.Status)
	edit := h.campaign(t, "campaign_save", adminActor, usersrpc.CampaignRequest{ID: id, Name: "Late", Content: rpcContent})
	require.Equal(t, domainrpc.CodeConflict, edit.Code)

	listed := h.campaign(t, "campaign_list", adminActor, usersrpc.CampaignRequest{})
	require.Len(t, listed.Campaigns, 1)
	missing := h.campaign(t, "campaign_get", adminActor, usersrpc.CampaignRequest{ID: id + 1})
	require.Equal(t, domainrpc.CodeNotFound, missing.Code)
}

func TestCampaignTestForwardsToSender(t *testing.T) {
	tester := &recordingTester{}
	h := campaignHarness(t, tester)
	saved := h.campaign(t, "campaign_save", adminActor, usersrpc.CampaignRequest{Name: "Launch", Content: rpcContent})

	sent := h.campaign(t, "campaign_test", adminActor, usersrpc.CampaignRequest{ID: saved.Campaign.ID})
	require.True(t, sent.OK, sent.Error)
	require.Equal(t, saved.Campaign.ID, tester.campaign)
	require.Equal(t, adminActor, tester.user)
}

func TestCampaignTestUnavailableWithoutSender(t *testing.T) {
	h := campaignHarness(t, nil)
	reply := h.campaign(t, "campaign_test", adminActor, usersrpc.CampaignRequest{ID: 1})
	require.Equal(t, domainrpc.CodeUnavailable, reply.Code)
}

func TestMarketingDeliveryRPCRetriesUnknownAndRejectsInvalid(t *testing.T) {
	h := campaignHarness(t, nil)
	unknown := call[usersrpc.MarketingDeliveryReply](t, h, usersrpc.MarketingDeliverySubject, usersrpc.MarketingDeliveryRequest{
		ResendID: "re_unknown", Event: consent.SendDelivered,
	})
	require.Equal(t, domainrpc.CodeUnavailable, unknown.Code)
	invalid := call[usersrpc.MarketingDeliveryReply](t, h, usersrpc.MarketingDeliverySubject, usersrpc.MarketingDeliveryRequest{ResendID: "re_x", Event: consent.SendSent})
	require.Equal(t, domainrpc.CodeInvalid, invalid.Code)
}
