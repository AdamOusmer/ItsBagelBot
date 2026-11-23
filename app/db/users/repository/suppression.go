// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"bytes"
	"context"
	"strings"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/emailsuppression"
	"ItsBagelBot/app/db/users/ent/marketingconsent"
	"ItsBagelBot/app/db/users/ent/user"
	catalog "ItsBagelBot/internal/domain/analytics"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

const MaxAudiencePage = 500

type AudienceRecipient struct {
	UserID uint64
	Locale string
}

type AudiencePage struct {
	Recipients []AudienceRecipient
	Next       uint64
}

type audienceCandidate struct {
	owner *ent.User
	hmacs [][]byte
}

func (w consentWrite) reconcileSuppression(ctx context.Context, record marketingRecord) (consent.SuppressionReason, error) {
	if len(record.addressHMAC) == 0 || !record.Source.Suppresses() {
		return "", nil
	}
	switch record.Status {
	case consent.Withdrawn:
		return w.suppress(ctx, record.addressHMAC, consent.SuppressUnsubscribe)
	case consent.Granted:
		return "", w.liftUnsubscribe(ctx, record.addressHMAC)
	}
	return "", nil
}

func (w consentWrite) suppress(ctx context.Context, addressHMAC []byte, reason consent.SuppressionReason) (consent.SuppressionReason, error) {
	changed, err := addSuppression(ctx, w.tx.EmailSuppression, addressHMAC, reason)
	if !changed {
		return "", err
	}
	return reason, err
}

func (w consentWrite) sharedWithdrawal(ctx context.Context, addressHMAC []byte) (bool, error) {
	return w.tx.MarketingConsent.Query().
		Where(
			marketingconsent.AddressHmacEQ(addressHMAC),
			marketingconsent.UserIDNEQ(w.owner.ID),
			marketingconsent.HasUserWith(user.MarketingStatusEQ(user.MarketingStatusWithdrawn)),
		).Exist(ctx)
}

func (w consentWrite) liftUnsubscribe(ctx context.Context, addressHMAC []byte) error {
	shared, err := w.sharedWithdrawal(ctx, addressHMAC)
	if err != nil || shared {
		return err
	}
	_, err = w.tx.EmailSuppression.Delete().
		Where(emailsuppression.AddressHmacEQ(addressHMAC), emailsuppression.ReasonEQ(emailsuppression.ReasonUnsubscribe)).
		Exec(ctx)
	return err
}

func addSuppression(ctx context.Context, suppressions *ent.EmailSuppressionClient, addressHMAC []byte, reason consent.SuppressionReason) (bool, error) {
	err := suppressions.Create().SetAddressHmac(addressHMAC).SetReason(emailsuppression.Reason(reason)).Exec(ctx)
	if !ent.IsConstraintError(err) {
		return err == nil, err
	}
	if !reason.Reported() {
		return false, nil
	}
	upgraded, err := suppressions.Update().
		Where(emailsuppression.AddressHmacEQ(addressHMAC), emailsuppression.ReasonEQ(emailsuppression.ReasonUnsubscribe)).
		SetReason(emailsuppression.Reason(reason)).Save(ctx)
	return upgraded > 0, err
}

func (r *Users) recordConsentAnalytics(change consentChange) {
	if change.marketing != nil {
		r.analytics.Add(catalog.MarketingConsents, 1, string(change.marketing.Status), string(change.marketing.Source))
	}
	if change.suppressed != "" {
		r.analytics.Add(catalog.MarketingSuppressions, 1, string(change.suppressed))
	}
}

func normalizeAddress(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func addressForms(email string) []string {
	forms := []string{normalizeAddress(email)}
	if trimmed := strings.TrimSpace(email); trimmed != forms[0] {
		forms = append(forms, trimmed)
	}
	return forms
}

func (r *Users) addressMAC(email string) ([]byte, error) {
	return r.marketingMAC.ComputeMAC([]byte(normalizeAddress(email)))
}

func (r *Users) addressMatches(addressHMAC []byte, email string) bool {
	if r.marketingMAC.VerifyMAC(addressHMAC, []byte(normalizeAddress(email))) == nil {
		return true
	}
	return r.marketingMAC.VerifyMAC(addressHMAC, []byte(email)) == nil
}

func (r *Users) addressHMACs(email string) ([][]byte, error) {
	forms := addressForms(email)
	hmacs := make([][]byte, 0, len(forms))
	for _, form := range forms {
		sum, err := r.marketingMAC.ComputeMAC([]byte(form))
		if err != nil {
			return nil, err
		}
		hmacs = append(hmacs, sum)
	}
	return hmacs, nil
}

func (r *Users) SuppressAddress(ctx context.Context, email string, reason consent.SuppressionReason) error {
	if !reason.Reported() {
		return ErrConsentInvalid
	}
	if err := validate.Email(strings.TrimSpace(email)); err != nil {
		return err
	}
	if r.marketingMAC == nil {
		return ErrConsentMACUnavailable
	}
	hmacs, err := r.addressHMACs(email)
	if err != nil {
		return err
	}
	for _, addressHMAC := range hmacs {
		if err := r.suppressAddressHMAC(ctx, addressHMAC, reason); err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) suppressAddressHMAC(ctx context.Context, addressHMAC []byte, reason consent.SuppressionReason) error {
	changed, err := db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return addSuppression(ctx, r.client.EmailSuppression, addressHMAC, reason)
	})
	if err != nil {
		return err
	}
	if changed {
		r.analytics.Add(catalog.MarketingSuppressions, 1, string(reason))
	}
	return r.withdrawMatchingGrants(ctx, addressHMAC)
}

func (r *Users) withdrawMatchingGrants(ctx context.Context, addressHMAC []byte) error {
	grants, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.MarketingConsent, error) {
		return r.client.MarketingConsent.Query().
			Where(marketingconsent.AddressHmacEQ(addressHMAC), marketingconsent.StatusEQ(marketingconsent.StatusGranted)).
			Select(marketingconsent.FieldUserID).All(ctx)
	})
	if err != nil {
		return err
	}
	seen := make(map[uint64]struct{}, len(grants))
	for _, grant := range grants {
		if _, done := seen[grant.UserID]; done {
			continue
		}
		seen[grant.UserID] = struct{}{}
		if err := r.withdrawForReport(ctx, grant.UserID, addressHMAC); err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) withdrawForReport(ctx context.Context, id uint64, addressHMAC []byte) error {
	_, err := r.withConsentOwner(ctx, id, func(w consentWrite) (consentChange, error) {
		if w.owner.MarketingStatus != user.MarketingStatusGranted {
			return consentChange{}, nil
		}
		grant, err := w.latestMarketingGrant(ctx)
		if err != nil || !bytes.Equal(grant.AddressHmac, addressHMAC) {
			return consentChange{}, err
		}
		choice := MarketingChoice{Status: consent.Withdrawn, Source: consent.Complaint, Locale: grant.Locale}
		return r.recordMarketing(ctx, w, choice)
	})
	return err
}

func (r *Users) MarketingAudience(ctx context.Context, afterID uint64, limit int) (AudiencePage, error) {
	if limit < 1 || limit > MaxAudiencePage {
		return AudiencePage{}, ErrConsentInvalid
	}
	if r.marketingMAC == nil {
		return AudiencePage{}, ErrConsentMACUnavailable
	}
	owners, err := r.grantedOwners(ctx, afterID, limit)
	if err != nil || len(owners) == 0 {
		return AudiencePage{}, err
	}
	candidates, err := r.audienceCandidates(ctx, owners)
	if err != nil {
		return AudiencePage{}, err
	}
	suppressed, err := r.suppressedSet(ctx, candidates)
	if err != nil {
		return AudiencePage{}, err
	}
	page := AudiencePage{Recipients: audienceRecipients(candidates, suppressed)}
	if len(owners) == limit {
		page.Next = owners[len(owners)-1].ID
	}
	return page, nil
}

func (r *Users) grantedOwners(ctx context.Context, afterID uint64, limit int) ([]*ent.User, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]*ent.User, error) {
		return r.client.User.Query().
			Where(user.MarketingStatusEQ(user.MarketingStatusGranted), user.IDGT(afterID)).
			Order(ent.Asc(user.FieldID)).Limit(limit).
			Select(user.FieldID, user.FieldLocale, user.FieldCreatedAt, user.FieldEmailEnc).
			All(ctx)
	})
}

func (r *Users) audienceCandidates(ctx context.Context, owners []*ent.User) ([]audienceCandidate, error) {
	ids := make([]uint64, len(owners))
	for i, owner := range owners {
		ids[i] = owner.ID
	}
	grants, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.MarketingConsent, error) {
		return r.client.MarketingConsent.Query().
			Where(marketingconsent.UserIDIn(ids...), marketingconsent.StatusEQ(marketingconsent.StatusGranted)).
			Order(ent.Asc(marketingconsent.FieldID)).All(ctx)
	})
	if err != nil {
		return nil, err
	}
	latest := make(map[uint64]*ent.MarketingConsent, len(owners))
	for _, grant := range grants {
		latest[grant.UserID] = grant
	}
	candidates := make([]audienceCandidate, 0, len(owners))
	for _, owner := range owners {
		hmacs := r.currentGrantHMACs(owner, latest[owner.ID])
		if hmacs != nil {
			candidates = append(candidates, audienceCandidate{owner: owner, hmacs: hmacs})
		}
	}
	return candidates, nil
}

func (r *Users) currentGrantHMACs(owner *ent.User, grant *ent.MarketingConsent) [][]byte {
	if grant == nil || grant.AccountCreatedAt != owner.CreatedAt.UnixMicro() {
		return nil
	}
	email, err := r.openContactEmail(owner.ID, owner.EmailEnc)
	if err != nil || !r.addressMatches(grant.AddressHmac, email) {
		return nil
	}
	current, err := r.addressMAC(email)
	if err != nil {
		return nil
	}
	return [][]byte{grant.AddressHmac, current}
}

func (r *Users) suppressedSet(ctx context.Context, candidates []audienceCandidate) (map[string]struct{}, error) {
	var hmacs [][]byte
	for _, candidate := range candidates {
		hmacs = append(hmacs, candidate.hmacs...)
	}
	if len(hmacs) == 0 {
		return nil, nil
	}
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.EmailSuppression, error) {
		return r.client.EmailSuppression.Query().Where(emailsuppression.AddressHmacIn(hmacs...)).All(ctx)
	})
	if err != nil {
		return nil, err
	}
	set := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		set[string(row.AddressHmac)] = struct{}{}
	}
	return set, nil
}

func audienceRecipients(candidates []audienceCandidate, suppressed map[string]struct{}) []AudienceRecipient {
	recipients := make([]AudienceRecipient, 0, len(candidates))
	for _, candidate := range candidates {
		if !anySuppressed(candidate.hmacs, suppressed) {
			recipients = append(recipients, AudienceRecipient{UserID: candidate.owner.ID, Locale: candidate.owner.Locale})
		}
	}
	return recipients
}

func anySuppressed(hmacs [][]byte, suppressed map[string]struct{}) bool {
	for _, addressHMAC := range hmacs {
		if _, hit := suppressed[string(addressHMAC)]; hit {
			return true
		}
	}
	return false
}
