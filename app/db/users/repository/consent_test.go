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
}

func setupConsent(t *testing.T) consentFixture {
	t.Helper()
	handle, err := keyset.NewHandle(mac.HMACSHA256Tag256KeyTemplate())
	require.NoError(t, err)
	addressMAC, err := mac.New(handle)
	require.NoError(t, err)
	client, _, repo := setupWithMarketingMAC(t, addressMAC)
	require.NoError(t, repo.Register(t.Context(), consentAccount))
	return consentFixture{client: client, repo: repo, mac: addressMAC}
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
	require.NoError(t, f.repo.SetConsent(ctx, id, req))

	terms := f.client.TermsAcceptance.Query().OnlyX(ctx)
	grant := f.client.MarketingConsent.Query().OnlyX(ctx)
	owner := f.client.User.GetX(ctx, id)
	require.Equal(t, termsacceptance.SourceConsent, terms.Source)
	require.Equal(t, marketingconsent.SourceConsent, grant.Source)
	require.Equal(t, marketingconsent.BasisExpress, grant.Basis)

	require.Equal(t, []string{req.RequestID, req.TermsVersion, req.Locale},
		[]string{terms.RequestID, terms.TermsVersion, terms.Locale})
	require.Equal(t, []string{consent.MarketingPolicyVersion, consent.MarketingTextKey, req.Locale},
		[]string{grant.PolicyVersion, grant.ConsentTextKey, grant.Locale})
	require.NoError(t, f.mac.VerifyMAC(grant.AddressHmac, []byte(consentEmail)))

	incarnation := owner.CreatedAt.UnixMicro()
	require.Equal(t, []int64{incarnation, incarnation}, []int64{terms.AccountCreatedAt, grant.AccountCreatedAt})

	require.NotNil(t, owner.MarketingStatusAt)
	require.Equal(t, terms.AcceptedAt, grant.CreatedAt)
	require.True(t, grant.CreatedAt.Equal(*owner.MarketingStatusAt))

	require.NoError(t, f.repo.Delete(ctx, id))
	require.Zero(t, f.client.TermsAcceptance.Query().CountX(ctx))
	require.Zero(t, f.client.MarketingConsent.Query().CountX(ctx))

	require.NoError(t, f.repo.Register(ctx, consentAccount))
	f.requireProjection(t, "", consent.None)
}

func TestConsentRetriesAndOptionalMarketing(t *testing.T) {
	f := setupConsent(t)
	ctx, id := t.Context(), consentAccount.ID
	optIn := consentRequest(true)
	declined := repository.TermsConsent{TermsVersion: "2027-01", RequestID: "consent-2", Source: consent.Consent, Locale: "fr"}
	require.ErrorIs(t, f.repo.SetConsent(ctx, id, optIn), repository.ErrNoContactEmail)
	f.requireProjection(t, "", consent.None)
	require.Zero(t, f.client.TermsAcceptance.Query().CountX(ctx))

	require.NoError(t, f.repo.SetContactEmail(ctx, id, consentEmail))
	require.NoError(t, f.repo.SetConsent(ctx, id, optIn))
	require.NoError(t, f.repo.SetConsent(ctx, id, optIn))
	require.NoError(t, f.repo.SetConsent(ctx, id, declined))
	f.requireProjection(t, declined.TermsVersion, consent.Granted)
	require.Equal(t, 1, f.client.MarketingConsent.Query().CountX(ctx))

	require.NoError(t, f.repo.SetMarketing(ctx, id, repository.MarketingChoice{Status: consent.Withdrawn, Source: consent.Settings}))
	require.NoError(t, f.repo.SetConsent(ctx, id, optIn))
	f.requireProjection(t, declined.TermsVersion, consent.Withdrawn)
	require.Equal(t, 2, f.client.TermsAcceptance.Query().CountX(ctx))
	require.Equal(t, 2, f.client.MarketingConsent.Query().CountX(ctx))
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
	require.NoError(t, f.repo.SetConsent(ctx, id, consentRequest(true)))

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
