// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/users/ent/marketingsend"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/consent"

	"github.com/stretchr/testify/require"
)

var campaignContent = map[string]consent.CampaignContent{
	"en": {Subject: "News", HTML: "<p>Hello</p>", Text: "Hello"},
}

func (f consentFixture) draftCampaign(t *testing.T) repository.Campaign {
	t.Helper()
	campaign, err := f.repo.CampaignSave(t.Context(), repository.CampaignDraft{Name: "Launch", Content: campaignContent, ActorID: "1"})
	require.NoError(t, err)
	return campaign
}

func (f consentFixture) claim(t *testing.T, campaignID int) repository.SendTicket {
	t.Helper()
	recipient, err := f.repo.MarketingRecipient(t.Context(), consentAccount.ID)
	require.NoError(t, err)
	ticket, err := f.repo.ClaimSend(t.Context(), repository.SendClaim{
		CampaignID: campaignID, UserID: consentAccount.ID, AddressHMAC: recipient.AddressHMAC,
	})
	require.NoError(t, err)
	return ticket
}

func TestCampaignLifecycleGuards(t *testing.T) {
	f := setupConsent(t)
	ctx := t.Context()
	campaign := f.draftCampaign(t)

	saved, err := f.repo.CampaignSave(ctx, repository.CampaignDraft{ID: campaign.ID, Name: "Renamed", Content: campaignContent, ActorID: "1"})
	require.NoError(t, err)
	_, err = f.repo.CampaignPause(ctx, campaign.ID)
	require.ErrorIs(t, err, repository.ErrCampaignState)
	approval := repository.CampaignApproval{ID: campaign.ID, Revision: saved.Revision, ActorID: "2"}
	_, err = f.repo.CampaignApprove(ctx, approval)
	require.NoError(t, err)

	for name, run := range map[string]func() error{
		"edit after approval": func() error {
			_, err := f.repo.CampaignSave(ctx, repository.CampaignDraft{ID: campaign.ID, Name: "Late", Content: campaignContent, ActorID: "1"})
			return err
		},
		"approve twice":  func() error { _, err := f.repo.CampaignApprove(ctx, approval); return err },
		"resume running": func() error { _, err := f.repo.CampaignResume(ctx, campaign.ID); return err },
	} {
		require.ErrorIs(t, run(), repository.ErrCampaignState, name)
	}

	paused, err := f.repo.CampaignPause(ctx, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, consent.CampaignPaused, paused.Status)
	resumed, err := f.repo.CampaignResume(ctx, campaign.ID)
	require.NoError(t, err)
	require.Equal(t, consent.CampaignApproved, resumed.Status)
}

func TestCampaignApprovalNeedsCompleteEnglishContent(t *testing.T) {
	f := setupConsent(t)
	for name, content := range map[string]map[string]consent.CampaignContent{
		"no english":      {"fr": campaignContent["en"]},
		"incomplete body": {"en": {Subject: "News", HTML: "<p>Hello</p>"}},
	} {
		campaign, err := f.repo.CampaignSave(t.Context(), repository.CampaignDraft{Name: name, Content: content, ActorID: "1"})
		require.NoError(t, err)
		_, err = f.repo.CampaignApprove(t.Context(), repository.CampaignApproval{ID: campaign.ID, Revision: campaign.Revision, ActorID: "2"})
		require.ErrorIs(t, err, repository.ErrCampaignInvalid, name)
	}
}

func TestCampaignApprovalPinsTheReviewedRevision(t *testing.T) {
	f := setupConsent(t)
	ctx := t.Context()
	reviewed := f.draftCampaign(t)
	edited, err := f.repo.CampaignSave(ctx, repository.CampaignDraft{ID: reviewed.ID, Name: "Swapped", Content: campaignContent, ActorID: "1"})
	require.NoError(t, err)
	require.Greater(t, edited.Revision, reviewed.Revision)

	_, err = f.repo.CampaignApprove(ctx, repository.CampaignApproval{ID: reviewed.ID, Revision: reviewed.Revision, ActorID: "2"})
	require.ErrorIs(t, err, repository.ErrCampaignState)
	require.EqualValues(t, consent.CampaignDraft, f.client.MarketingCampaign.GetX(ctx, reviewed.ID).Status)

	approved, err := f.repo.CampaignApprove(ctx, repository.CampaignApproval{ID: reviewed.ID, Revision: edited.Revision, ActorID: "2"})
	require.NoError(t, err)
	require.Equal(t, consent.CampaignApproved, approved.Status)
}

func TestCampaignDraftValidation(t *testing.T) {
	f := setupConsent(t)
	for name, draft := range map[string]repository.CampaignDraft{
		"empty name":     {Content: campaignContent, ActorID: "1"},
		"no actor":       {Name: "x", Content: campaignContent},
		"upper locale":   {Name: "x", ActorID: "1", Content: map[string]consent.CampaignContent{"EN": campaignContent["en"]}},
		"oversized body": {Name: "x", ActorID: "1", Content: map[string]consent.CampaignContent{"en": {Subject: "s", HTML: string(make([]byte, consent.CampaignBodyMax+1)), Text: "t"}}},
	} {
		_, err := f.repo.CampaignSave(t.Context(), draft)
		require.ErrorIs(t, err, repository.ErrCampaignInvalid, name)
	}
}

func TestClaimSendIsIdempotentAndCounted(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	campaign := f.draftCampaign(t)

	first := f.claim(t, campaign.ID)
	again := f.claim(t, campaign.ID)
	require.Equal(t, first, again)
	require.Equal(t, repository.SendKey(campaign.ID, consentAccount.ID), first.Key)

	require.NoError(t, f.repo.RecordSend(t.Context(), repository.SendOutcome{ID: first.ID, Status: consent.SendSent, ResendID: "re_1"}))
	require.Equal(t, consent.SendSent, f.claim(t, campaign.ID).Status)
	loaded, err := f.repo.CampaignGet(t.Context(), campaign.ID)
	require.NoError(t, err)
	require.Equal(t, map[consent.SendStatus]int{consent.SendSent: 1}, loaded.Sends)
}

func TestClaimSendNeverDoublesAUserOrAnAddress(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	f.joinAudience(t, 43, consentEmail)
	campaign := f.draftCampaign(t)
	first := f.claim(t, campaign.ID)

	moved, err := f.repo.ClaimSend(t.Context(), repository.SendClaim{CampaignID: campaign.ID, UserID: consentAccount.ID, AddressHMAC: []byte("changed-address")})
	require.NoError(t, err)
	require.Equal(t, first, moved)

	recipient, err := f.repo.MarketingRecipient(t.Context(), 43)
	require.NoError(t, err)
	shared, err := f.repo.ClaimSend(t.Context(), repository.SendClaim{CampaignID: campaign.ID, UserID: 43, AddressHMAC: recipient.AddressHMAC})
	require.NoError(t, err)
	require.Equal(t, consent.SendSkipped, shared.Status)
	require.Equal(t, 1, f.client.MarketingSend.Query().CountX(t.Context()))
}

func TestSendAttemptsCountAndQueuedSendsCanBeSkipped(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	campaign := f.draftCampaign(t)
	ticket := f.claim(t, campaign.ID)

	for want := 1; want <= 2; want++ {
		attempts, err := f.repo.RecordAttempt(t.Context(), ticket.ID)
		require.NoError(t, err)
		require.Equal(t, want, attempts)
	}
	require.NoError(t, f.repo.SkipQueuedSend(t.Context(), campaign.ID, consentAccount.ID))
	require.Equal(t, consent.SendSkipped, f.claim(t, campaign.ID).Status)
	require.NoError(t, f.repo.RecordSend(t.Context(), repository.SendOutcome{ID: ticket.ID, Status: consent.SendSent}))
	require.NoError(t, f.repo.SkipQueuedSend(t.Context(), campaign.ID, consentAccount.ID))
	require.Equal(t, consent.SendSkipped, f.claim(t, campaign.ID).Status)
}

func TestRecordDeliveryIsIdempotentAndNeverRegresses(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	campaign := f.draftCampaign(t)
	ticket := f.claim(t, campaign.ID)
	require.NoError(t, f.repo.RecordSend(t.Context(), repository.SendOutcome{ID: ticket.ID, Status: consent.SendSent, ResendID: "re_9"}))

	for _, step := range []struct {
		event consent.SendStatus
		want  marketingsend.Status
	}{
		{consent.SendDelivered, marketingsend.StatusDelivered},
		{consent.SendDelivered, marketingsend.StatusDelivered},
		{consent.SendComplained, marketingsend.StatusComplained},
		{consent.SendBounced, marketingsend.StatusComplained},
		{consent.SendDelivered, marketingsend.StatusComplained},
	} {
		require.NoError(t, f.repo.RecordDelivery(t.Context(), "re_9", step.event))
		require.Equal(t, step.want, f.client.MarketingSend.GetX(t.Context(), ticket.ID).Status)
	}
	require.ErrorIs(t, f.repo.RecordDelivery(t.Context(), "re_unknown", consent.SendDelivered), repository.ErrSendUnknown)
	require.ErrorIs(t, f.repo.RecordDelivery(t.Context(), "re_9", consent.SendSent), repository.ErrConsentInvalid)
}

func TestMarketingRecipientRechecksConsentAndSuppression(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)

	recipient, err := f.repo.MarketingRecipient(t.Context(), consentAccount.ID)
	require.NoError(t, err)
	require.Equal(t, consentEmail, recipient.Email)

	require.NoError(t, f.setByToken(token, consent.Withdrawn, consent.OneClick))
	_, err = f.repo.MarketingRecipient(t.Context(), consentAccount.ID)
	require.ErrorIs(t, err, repository.ErrNotEligible)

	require.NoError(t, f.setByToken(token, consent.Granted, consent.PreferencePage))
	require.NoError(t, f.repo.SuppressAddress(t.Context(), consentEmail, consent.SuppressHardBounce))
	_, err = f.repo.MarketingRecipient(t.Context(), consentAccount.ID)
	require.ErrorIs(t, err, repository.ErrNotEligible)
}

func TestCampaignLeaseExcludesOtherHolders(t *testing.T) {
	f := setupConsent(t)
	campaign := f.draftCampaign(t)
	_, err := f.repo.CampaignApprove(t.Context(), repository.CampaignApproval{ID: campaign.ID, Revision: campaign.Revision, ActorID: "2"})
	require.NoError(t, err)
	now := f.client.MarketingCampaign.GetX(t.Context(), campaign.ID).CreatedAt

	lease := repository.CampaignLease{ID: campaign.ID, Holder: "a", Now: now, TTL: time.Minute}
	for _, step := range []struct {
		holder string
		at     int64
		want   bool
	}{{"a", 0, true}, {"b", 30, false}, {"a", 30, true}, {"b", 200, true}} {
		lease.Holder, lease.Now = step.holder, now.Add(time.Duration(step.at)*time.Second)
		held, err := f.repo.AcquireCampaignLease(t.Context(), lease)
		require.NoError(t, err)
		require.Equal(t, step.want, held, "%s at +%ds", step.holder, step.at)
	}
}

func TestErasureRemovesSendRows(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	campaign := f.draftCampaign(t)
	f.claim(t, campaign.ID)

	require.NoError(t, f.repo.Delete(t.Context(), consentAccount.ID))
	require.Zero(t, f.client.MarketingSend.Query().CountX(t.Context()))
	require.NotNil(t, f.client.MarketingCampaign.GetX(t.Context(), campaign.ID))
}
