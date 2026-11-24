// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/emailsuppression"
	"ItsBagelBot/app/db/users/ent/marketingconsent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/app/db/users/repository"
	"ItsBagelBot/internal/domain/consent"

	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
)

const audienceLimit = repository.MaxAudiencePage

func (f consentFixture) optedIn(t *testing.T) string {
	t.Helper()
	require.NoError(t, f.repo.SetContactEmail(t.Context(), consentAccount.ID, consentEmail))
	f.saveConsent(t, consentRequest(true))
	token, err := f.repo.UnsubscribeToken(t.Context(), consentAccount.ID)
	require.NoError(t, err)
	return token
}

func (f consentFixture) setByToken(token string, status consent.MarketingStatus, source consent.Source) error {
	return f.repo.SetMarketingByToken(context.Background(), token, repository.MarketingChoice{Status: status, Source: source})
}

func (f consentFixture) suppressions(t *testing.T) []string {
	t.Helper()
	var reasons []string
	for _, row := range f.client.EmailSuppression.Query().AllX(t.Context()) {
		reasons = append(reasons, string(row.Reason))
	}
	return reasons
}

func (f consentFixture) tokenStatus(t *testing.T, token string) consent.MarketingStatus {
	t.Helper()
	state, err := f.repo.MarketingTokenState(t.Context(), token)
	require.NoError(t, err)
	return state.Status
}

func TestUnsubscribeTokenRoundTrip(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)
	require.True(t, strings.HasPrefix(token, "v1."+strconv.FormatUint(consentAccount.ID, 10)+"."))
	require.True(t, strings.Contains(token, ".marketing."))
	require.Equal(t, consent.Granted, f.tokenStatus(t, token))

	for range 2 {
		require.NoError(t, f.setByToken(token, consent.Withdrawn, consent.OneClick))
		require.Equal(t, consent.Withdrawn, f.tokenStatus(t, token))
	}
	require.Equal(t, []string{"unsubscribe"}, f.suppressions(t))
	require.Equal(t, 2, f.client.MarketingConsent.Query().CountX(t.Context()))

	withdrawal := f.client.MarketingConsent.Query().Where(marketingconsent.StatusEQ(marketingconsent.StatusWithdrawn)).OnlyX(t.Context())
	require.Equal(t, marketingconsent.SourceOneClick, withdrawal.Source)
	require.NoError(t, f.mac.VerifyMAC(f.client.EmailSuppression.Query().OnlyX(t.Context()).AddressHmac, []byte(consentEmail)))

	require.NoError(t, f.setByToken(token, consent.Granted, consent.PreferencePage))
	require.Equal(t, consent.Granted, f.tokenStatus(t, token))
	require.Empty(t, f.suppressions(t))
}

func TestMarketingWithdrawAndGrantMaintainSuppression(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	ctx, id := t.Context(), consentAccount.ID

	require.NoError(t, f.repo.SetMarketing(ctx, id, repository.MarketingChoice{Status: consent.Withdrawn, Source: consent.Settings}))
	require.Equal(t, []string{"unsubscribe"}, f.suppressions(t))

	require.NoError(t, f.repo.SetMarketing(ctx, id, repository.MarketingChoice{Status: consent.Granted, Source: consent.Settings}))
	require.Empty(t, f.suppressions(t))
}

func TestUnsubscribeTokenRejections(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)
	otherHandle, err := keyset.NewHandle(mac.HMACSHA256Tag256KeyTemplate())
	require.NoError(t, err)
	otherMAC, err := mac.New(otherHandle)
	require.NoError(t, err)
	foreign := f.config
	foreign.MarketingMAC = otherMAC
	foreign.Client = f.client

	lastDot := strings.LastIndexByte(token, '.')
	flipped := token[:lastDot+1] + "A" + token[lastDot+2:]
	if flipped == token {
		flipped = token[:lastDot+1] + "B" + token[lastDot+2:]
	}
	forgedID := strings.Replace(token, "v1."+strconv.FormatUint(consentAccount.ID, 10)+".", "v1.43.", 1)
	for name, bad := range map[string]string{
		"empty":           "",
		"garbage":         "not-a-token",
		"flipped mac":     flipped,
		"other user":      forgedID,
		"other version":   strings.Replace(token, "v1.", "v2.", 1),
		"other scope":     strings.Replace(token, ".marketing.", ".billing.", 1),
		"truncated":       token[:lastDot],
		"oversized":       token + strings.Repeat("A", 300),
		"foreign keyset":  mustToken(t, openTestUsers(t, foreign)),
		"unsigned suffix": token[:lastDot+1],
	} {
		t.Run(name, func(t *testing.T) {
			_, err := f.repo.MarketingTokenState(t.Context(), bad)
			require.ErrorIs(t, err, repository.ErrMarketingTokenInvalid)
			require.ErrorIs(t, f.setByToken(bad, consent.Withdrawn, consent.OneClick), repository.ErrMarketingTokenInvalid)
		})
	}
	require.Equal(t, consent.Granted, f.tokenStatus(t, token))
}

func mustToken(t *testing.T, repo *repository.Users) string {
	t.Helper()
	token, err := repo.UnsubscribeToken(t.Context(), consentAccount.ID)
	require.NoError(t, err)
	return token
}

func TestUnsubscribeTokenDiesWithAccountIncarnation(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)
	require.NoError(t, f.repo.Delete(t.Context(), consentAccount.ID))
	_, err := f.repo.MarketingTokenState(t.Context(), token)
	require.ErrorIs(t, err, repository.ErrMarketingTokenInvalid)

	require.NoError(t, f.repo.Register(t.Context(), consentAccount))
	_, err = f.repo.MarketingTokenState(t.Context(), token)
	require.ErrorIs(t, err, repository.ErrMarketingTokenInvalid)
	require.ErrorIs(t, f.setByToken(token, consent.Granted, consent.PreferencePage), repository.ErrMarketingTokenInvalid)
}

func TestSetMarketingByTokenValidatesChoice(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)
	for _, choice := range []repository.MarketingChoice{
		{Status: consent.None, Source: consent.OneClick},
		{Status: consent.Withdrawn, Source: consent.Settings},
		{Status: consent.Withdrawn, Source: consent.Complaint},
		{Status: "", Source: consent.PreferencePage},
	} {
		require.ErrorIs(t, f.repo.SetMarketingByToken(t.Context(), token, choice), repository.ErrConsentInvalid)
	}
	require.Equal(t, consent.Granted, f.tokenStatus(t, token))
}

func TestTokenRefusesWithoutMAC(t *testing.T) {
	f := setupConsent(t)
	cfg := f.config
	cfg.MarketingMAC = nil
	bare := openTestUsers(t, cfg)
	_, err := bare.UnsubscribeToken(t.Context(), consentAccount.ID)
	require.ErrorIs(t, err, repository.ErrConsentMACUnavailable)
	_, err = bare.MarketingTokenState(t.Context(), "v1.42.1.marketing.AAAA")
	require.ErrorIs(t, err, repository.ErrConsentMACUnavailable)
}

func TestReportedAddressWithdrawsConsentAndSticks(t *testing.T) {
	for _, reason := range []consent.SuppressionReason{consent.SuppressComplaint, consent.SuppressHardBounce} {
		t.Run(string(reason), func(t *testing.T) {
			f := setupConsent(t)
			token := f.optedIn(t)
			require.NoError(t, f.repo.SuppressAddress(t.Context(), strings.ToUpper(consentEmail), reason))

			require.Equal(t, consent.Withdrawn, f.tokenStatus(t, token))
			require.Equal(t, []string{string(reason)}, slices.Compact(f.suppressions(t)))
			last := f.client.MarketingConsent.Query().Order(ent.Desc(marketingconsent.FieldID)).FirstX(t.Context())
			require.Equal(t, marketingconsent.SourceComplaint, last.Source)

			require.NoError(t, f.repo.SetMarketing(t.Context(), consentAccount.ID, repository.MarketingChoice{Status: consent.Granted, Source: consent.Settings}))
			require.Contains(t, f.suppressions(t), string(reason))
			page, err := f.repo.MarketingAudience(t.Context(), 0, audienceLimit)
			require.NoError(t, err)
			require.Empty(t, page.Recipients)
		})
	}
}

func TestReportedAddressUpgradesUnsubscribeAndIsIdempotent(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)
	require.NoError(t, f.setByToken(token, consent.Withdrawn, consent.OneClick))
	require.Equal(t, []string{"unsubscribe"}, f.suppressions(t))

	for range 2 {
		require.NoError(t, f.repo.SuppressAddress(t.Context(), consentEmail, consent.SuppressComplaint))
	}
	require.Equal(t, []string{"complaint"}, f.suppressions(t))
	require.NoError(t, f.setByToken(token, consent.Granted, consent.PreferencePage))
	require.Equal(t, []string{"complaint"}, f.suppressions(t))
	require.Equal(t, 1, f.client.EmailSuppression.Query().Where(emailsuppression.ReasonEQ(emailsuppression.ReasonComplaint)).CountX(t.Context()))
}

func TestSuppressAddressValidation(t *testing.T) {
	f := setupConsent(t)
	require.ErrorIs(t, f.repo.SuppressAddress(t.Context(), consentEmail, consent.SuppressUnsubscribe), repository.ErrConsentInvalid)
	require.ErrorIs(t, f.repo.SuppressAddress(t.Context(), consentEmail, "spam"), repository.ErrConsentInvalid)
	require.Error(t, f.repo.SuppressAddress(t.Context(), "not an address", consent.SuppressComplaint))

	cfg := f.config
	cfg.MarketingMAC = nil
	bare := openTestUsers(t, cfg)
	require.ErrorIs(t, bare.SuppressAddress(t.Context(), consentEmail, consent.SuppressComplaint), repository.ErrConsentMACUnavailable)
	require.Zero(t, f.client.EmailSuppression.Query().CountX(t.Context()))
}

func TestSuppressedAddressForUnknownAccountIsKept(t *testing.T) {
	f := setupConsent(t)
	require.NoError(t, f.repo.SuppressAddress(t.Context(), "stranger@example.test", consent.SuppressHardBounce))
	require.Equal(t, []string{"hard_bounce"}, f.suppressions(t))
}

func TestSuppressionSurvivesErasure(t *testing.T) {
	f := setupConsent(t)
	token := f.optedIn(t)
	require.NoError(t, f.setByToken(token, consent.Withdrawn, consent.OneClick))
	require.NoError(t, f.repo.Delete(t.Context(), consentAccount.ID))
	require.Equal(t, []string{"unsubscribe"}, f.suppressions(t))
	require.Zero(t, f.client.MarketingConsent.Query().CountX(t.Context()))
}

func (f consentFixture) joinAudience(t *testing.T, id uint64, email string) {
	t.Helper()
	require.NoError(t, f.repo.Register(t.Context(), repository.Registration{ID: id, Username: "user" + strconv.FormatUint(id, 10), Email: strconv.FormatUint(id, 10) + "@twitch.tv"}))
	require.NoError(t, f.repo.SetContactEmail(t.Context(), id, email))
	skipped, err := f.repo.SetConsent(t.Context(), id, repository.TermsConsent{
		TermsVersion: consent.TermsVersion, Marketing: true, Source: consent.Consent,
		Locale: "fr", RequestID: "audience-" + strconv.FormatUint(id, 10),
	})
	require.NoError(t, err)
	require.Empty(t, skipped)
}

func audienceIDs(page repository.AudiencePage) []uint64 {
	ids := make([]uint64, len(page.Recipients))
	for i, recipient := range page.Recipients {
		ids[i] = recipient.UserID
	}
	return ids
}

func TestAudienceListsOnlyEligibleGrantedAddresses(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	f.joinAudience(t, 43, "second@example.test")
	f.joinAudience(t, 44, "third@example.test")
	f.joinAudience(t, 45, "fourth@example.test")
	f.joinAudience(t, 46, "fifth@example.test")
	ctx := t.Context()

	require.NoError(t, f.repo.SetMarketing(ctx, 43, repository.MarketingChoice{Status: consent.Withdrawn, Source: consent.Settings}))
	require.NoError(t, f.repo.SuppressAddress(ctx, "third@example.test", consent.SuppressHardBounce))
	require.NoError(t, f.repo.SetContactEmail(ctx, 45, "moved@example.test"))

	page, err := f.repo.MarketingAudience(ctx, 0, audienceLimit)
	require.NoError(t, err)
	require.Equal(t, []uint64{consentAccount.ID, 46}, audienceIDs(page))
	require.Equal(t, "en", page.Recipients[0].Locale)
	require.Zero(t, page.Next)
}

func TestAudiencePagesByUserID(t *testing.T) {
	f := setupConsent(t)
	f.optedIn(t)
	f.joinAudience(t, 43, "second@example.test")
	f.joinAudience(t, 44, "third@example.test")

	var seen []uint64
	var after uint64
	for pages := 0; pages < 5; pages++ {
		page, err := f.repo.MarketingAudience(t.Context(), after, 2)
		require.NoError(t, err)
		seen = append(seen, audienceIDs(page)...)
		if page.Next == 0 {
			break
		}
		after = page.Next
	}
	require.Equal(t, []uint64{consentAccount.ID, 43, 44}, seen)
}

func TestAudienceValidatesLimitAndKeyset(t *testing.T) {
	f := setupConsent(t)
	for _, limit := range []int{-1, 0, audienceLimit + 1} {
		_, err := f.repo.MarketingAudience(t.Context(), 0, limit)
		require.ErrorIs(t, err, repository.ErrConsentInvalid)
	}
	cfg := f.config
	cfg.MarketingMAC = nil
	_, err := openTestUsers(t, cfg).MarketingAudience(t.Context(), 0, 1)
	require.ErrorIs(t, err, repository.ErrConsentMACUnavailable)
}

func TestResubscribeKeepsSuppressionWhileASharingAccountIsWithdrawn(t *testing.T) {
	f := setupConsent(t)
	ctx := t.Context()
	tokenA := f.optedIn(t)
	f.joinAudience(t, 43, consentEmail)
	tokenB, err := f.repo.UnsubscribeToken(ctx, 43)
	require.NoError(t, err)

	require.NoError(t, f.setByToken(tokenB, consent.Withdrawn, consent.OneClick))
	require.NoError(t, f.setByToken(tokenA, consent.Withdrawn, consent.OneClick))
	require.NoError(t, f.setByToken(tokenA, consent.Granted, consent.PreferencePage))
	require.Equal(t, []string{"unsubscribe"}, f.suppressions(t))
	page, err := f.repo.MarketingAudience(ctx, 0, audienceLimit)
	require.NoError(t, err)
	require.Empty(t, page.Recipients)

	require.NoError(t, f.setByToken(tokenB, consent.Granted, consent.PreferencePage))
	require.Empty(t, f.suppressions(t))
}

func TestAddressCaseDoesNotSplitSuppression(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	require.NoError(t, f.repo.SetContactEmail(ctx, id, "Consent@Example.test"))
	f.saveConsent(t, consentRequest(true))
	grant := f.client.MarketingConsent.Query().OnlyX(ctx)
	require.NoError(t, f.mac.VerifyMAC(grant.AddressHmac, []byte(consentEmail)))

	page, err := f.repo.MarketingAudience(ctx, 0, audienceLimit)
	require.NoError(t, err)
	require.Equal(t, []uint64{id}, audienceIDs(page))

	require.NoError(t, f.repo.SuppressAddress(ctx, consentEmail, consent.SuppressComplaint))
	page, err = f.repo.MarketingAudience(ctx, 0, audienceLimit)
	require.NoError(t, err)
	require.Empty(t, page.Recipients)
}

func TestGrantHashedFromRawCasingStillVerifies(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	const mixed = "Consent@Example.test"
	require.NoError(t, f.repo.SetContactEmail(ctx, id, mixed))
	owner := f.client.User.GetX(ctx, id)
	legacy, err := f.mac.ComputeMAC([]byte(mixed))
	require.NoError(t, err)
	f.client.MarketingConsent.Create().
		SetUserID(id).SetAccountCreatedAt(owner.CreatedAt.UnixMicro()).SetStatus(marketingconsent.StatusGranted).
		SetSource(marketingconsent.SourceConsent).SetPolicyVersion(consent.MarketingPolicyVersion).
		SetConsentTextKey(consent.MarketingTextKey).SetLocale("en").SetAddressHmac(legacy).SetCreatedAt(owner.CreatedAt).ExecX(ctx)
	f.client.User.UpdateOneID(id).SetMarketingStatus(user.MarketingStatusGranted).ExecX(ctx)

	page, err := f.repo.MarketingAudience(ctx, 0, audienceLimit)
	require.NoError(t, err)
	require.Equal(t, []uint64{id}, audienceIDs(page))
	require.NoError(t, f.repo.SetContactEmail(ctx, id, mixed))
	f.requireProjection(t, "", consent.Granted)
}
