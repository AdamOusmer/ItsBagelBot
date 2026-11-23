// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository_test

import (
	"testing"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/marketingconsent"
	"ItsBagelBot/app/db/users/ent/termsacceptance"
	"ItsBagelBot/app/db/users/repository"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/consent"

	"github.com/stretchr/testify/require"
	"github.com/tink-crypto/tink-go/v2/keyset"
	"github.com/tink-crypto/tink-go/v2/mac"
	"github.com/tink-crypto/tink-go/v2/tink"
)

const consentEmail = "consent@example.test"

var consentAccount = repository.Registration{ID: 42, Username: "owner", Email: "42@twitch.tv"}

type consentFixture struct {
	client *ent.Client
	repo   *repository.Users
	mac    tink.MAC
	config repository.UsersConfig
}

func setupConsent(t *testing.T) consentFixture {
	t.Helper()
	handle, err := keyset.NewHandle(mac.HMACSHA256Tag256KeyTemplate())
	require.NoError(t, err)
	addressMAC, err := mac.New(handle)
	require.NoError(t, err)
	cfg, _ := testUsersConfig(t)
	cfg.MarketingMAC = addressMAC
	repo := openTestUsers(t, cfg)
	require.NoError(t, repo.Register(t.Context(), consentAccount))
	return consentFixture{client: cfg.Client, repo: repo, mac: addressMAC, config: cfg}
}

func consentRequest(marketing bool) repository.TermsConsent {
	return repository.TermsConsent{
		TermsVersion: consent.TermsVersion, Marketing: marketing, Source: consent.Consent,
		Locale: "fr", RequestID: "consent-1",
	}
}

func TestConsentEvidenceAndErasure(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	require.NoError(t, f.repo.SetContactEmail(ctx, id, consentEmail))
	req := consentRequest(true)
	f.saveConsent(t, req)

	terms := f.client.TermsAcceptance.Query().OnlyX(ctx)
	require.Equal(t, termsacceptance.SourceConsent, terms.Source)
	require.Equal(t, []string{req.RequestID, req.TermsVersion, req.Locale},
		[]string{terms.RequestID, terms.TermsVersion, terms.Locale})

	grant := f.client.MarketingConsent.Query().OnlyX(ctx)
	require.Equal(t, []string{string(marketingconsent.SourceConsent), string(marketingconsent.BasisExpress),
		consent.MarketingPolicyVersion, consent.MarketingTextKey, req.Locale},
		[]string{string(grant.Source), string(grant.Basis), grant.PolicyVersion, grant.ConsentTextKey, grant.Locale})
	require.NoError(t, f.mac.VerifyMAC(grant.AddressHmac, []byte(consentEmail)))

	owner := f.client.User.GetX(ctx, id)
	incarnation := owner.CreatedAt.UnixMicro()
	require.Equal(t, []int64{incarnation, incarnation}, []int64{terms.AccountCreatedAt, grant.AccountCreatedAt})
	require.Equal(t, terms.AcceptedAt, grant.CreatedAt)
	require.True(t, owner.MarketingStatusAt != nil && grant.CreatedAt.Equal(*owner.MarketingStatusAt),
		"marketing status time %v differs from grant time %v", owner.MarketingStatusAt, grant.CreatedAt)

	err := f.repo.Delete(ctx, id)
	require.NoError(t, err)
	require.Zero(t, f.client.TermsAcceptance.Query().CountX(ctx))
	require.Zero(t, f.client.MarketingConsent.Query().CountX(ctx))

	err = f.repo.Register(ctx, consentAccount)
	require.NoError(t, err)
	f.requireProjection(t, "", consent.None)
}

func TestConsentRetriesAndOptionalMarketing(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	optIn := consentRequest(true)
	declined := consentRequest(false)
	declined.RequestID = "consent-2"

	require.NoError(t, f.repo.SetContactEmail(ctx, id, consentEmail))
	f.saveConsent(t, optIn)
	f.saveConsent(t, declined)
	f.requireProjection(t, declined.TermsVersion, consent.Granted)
	require.Equal(t, 1, f.client.MarketingConsent.Query().CountX(ctx))

	require.NoError(t, f.repo.SetMarketing(ctx, id, repository.MarketingChoice{Status: consent.Withdrawn, Source: consent.Settings}))
	f.saveConsent(t, optIn)
	f.requireProjection(t, declined.TermsVersion, consent.Withdrawn)
	require.Equal(t, 2, f.client.TermsAcceptance.Query().CountX(ctx))
	require.Equal(t, 2, f.client.MarketingConsent.Query().CountX(ctx))
}

func TestConsentKeepsLatestTermsProjection(t *testing.T) {
	const older = "2026-10"
	for _, tc := range []struct {
		name      string
		first     string
		submitted string
	}{
		{name: "newer submission", first: older, submitted: consent.TermsVersion},
		{name: "older submission during rollout", first: consent.TermsVersion, submitted: older},
		{name: "same version", first: consent.TermsVersion, submitted: consent.TermsVersion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupConsent(t)
			req := consentRequest(false)
			req.TermsVersion = tc.first
			f.saveConsent(t, req)

			req.TermsVersion = tc.submitted
			req.RequestID = "consent-2"
			f.saveConsent(t, req)

			f.requireProjection(t, consent.TermsVersion, consent.None)
			record := f.client.TermsAcceptance.Query().Where(termsacceptance.RequestIDEQ(req.RequestID)).OnlyX(t.Context())
			require.Equal(t, tc.submitted, record.TermsVersion)
		})
	}
}

func (f consentFixture) saveConsent(t *testing.T, req repository.TermsConsent) {
	t.Helper()
	skipped, err := f.repo.SetConsent(t.Context(), consentAccount.ID, req)
	require.NoError(t, err)
	require.Empty(t, skipped)
}

func (f consentFixture) requireProjection(t *testing.T, version string, status consent.MarketingStatus) {
	t.Helper()
	view, err := f.repo.Get(t.Context(), consentAccount.ID)
	require.NoError(t, err)
	require.Equal(t, version, view.TermsVersion)
	require.Equal(t, status, view.MarketingStatus)
}

func TestContactEmailChangeWithdrawsMarketing(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	require.NoError(t, f.repo.SetContactEmail(ctx, id, consentEmail))
	f.saveConsent(t, consentRequest(true))

	for _, tc := range []struct {
		name    string
		address string
		status  consent.MarketingStatus
		rows    int
	}{
		{"same email", consentEmail, consent.Granted, 1},
		{"new email", "new@example.test", consent.Withdrawn, 2},
		{"original email returns", consentEmail, consent.Withdrawn, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, f.repo.SetContactEmail(ctx, id, tc.address))
			f.requireProjection(t, consent.TermsVersion, tc.status)
			email, err := f.repo.ContactEmail(ctx, id)
			require.NoError(t, err)
			require.Equal(t, tc.address, email)
			require.Equal(t, tc.rows, f.client.MarketingConsent.Query().CountX(ctx))
			tally, err := f.repo.AnalyticsSeed(ctx)
			require.NoError(t, err)
			require.EqualValues(t, 1, tally[catalog.Field(catalog.AccountsMarketing, string(tc.status))])
		})
	}

	withdrawal := f.client.MarketingConsent.Query().Where(marketingconsent.StatusEQ(marketingconsent.StatusWithdrawn)).OnlyX(ctx)
	require.Equal(t, marketingconsent.SourceAddressChanged, withdrawal.Source)
	require.NoError(t, f.mac.VerifyMAC(withdrawal.AddressHmac, []byte(consentEmail)))
}

func TestContactEmailWithoutMACNeverWithdrawsMarketing(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	require.NoError(t, f.repo.SetContactEmail(ctx, id, consentEmail))
	f.saveConsent(t, consentRequest(true))

	cfg := f.config
	cfg.MarketingMAC = nil
	f.repo = openTestUsers(t, cfg)
	require.NoError(t, f.repo.SetContactEmail(ctx, id, "new@example.test"))
	f.requireProjection(t, consent.TermsVersion, consent.Granted)

	email, err := f.repo.ContactEmail(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "new@example.test", email)
	require.Equal(t, 1, f.client.MarketingConsent.Query().CountX(ctx))
}

func TestContactEmailChangeWithoutRecordedGrantIsNotBlocked(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	require.NoError(t, f.repo.SetContactEmail(ctx, id, consentEmail))
	f.saveConsent(t, consentRequest(true))
	f.client.MarketingConsent.Delete().ExecX(ctx)

	require.NoError(t, f.repo.SetContactEmail(ctx, id, "new@example.test"))
	email, err := f.repo.ContactEmail(ctx, id)
	require.NoError(t, err)
	require.Equal(t, "new@example.test", email)
	f.requireProjection(t, consent.TermsVersion, consent.Granted)
}
