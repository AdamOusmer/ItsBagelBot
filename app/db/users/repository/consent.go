// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/marketingconsent"
	"ItsBagelBot/app/db/users/ent/termsacceptance"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/i18n"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

var (
	ErrConsentInvalid        = errors.New("invalid consent request")
	ErrConsentRequestUsed    = errors.New("consent request ID already used")
	ErrConsentMACUnavailable = errors.New("marketing address HMAC unavailable")
)

type TermsConsent struct {
	TermsVersion string
	Marketing    bool
	Source       consent.Source
	Locale       string
	RequestID    string
}

type MarketingChoice struct {
	Status consent.MarketingStatus
	Source consent.Source
	Locale string
}

type consentWrite struct {
	tx    *ent.Tx
	owner *ent.User
	at    time.Time
}

type consentChange struct {
	changed bool
	skipped consent.MarketingSkipReason
}

func (c TermsConsent) valid() bool {
	if c.Source != consent.Welcome && c.Source != consent.Consent {
		return false
	}
	if !consent.IsPublishedTermsVersion(c.TermsVersion) || strings.TrimSpace(c.RequestID) == "" {
		return false
	}
	return i18n.Supported(c.Locale)
}

func (c MarketingChoice) valid() bool {
	if c.Status != consent.Granted && c.Status != consent.Withdrawn {
		return false
	}
	switch c.Source {
	case consent.Welcome, consent.Consent, consent.Settings, consent.Prompt, consent.PreferencePage:
		return c.Locale == "" || i18n.Supported(c.Locale)
	default:
		return false
	}
}

func (r *Users) SetConsent(ctx context.Context, id uint64, req TermsConsent) (consent.MarketingSkipReason, error) {
	if !req.valid() {
		return "", ErrConsentInvalid
	}
	change, err := r.withConsentOwner(ctx, id, func(w consentWrite) (consentChange, error) {
		prior, err := w.tx.TermsAcceptance.Query().Where(termsacceptance.RequestIDEQ(req.RequestID)).Only(ctx)
		switch {
		case err == nil:
			return w.replayTerms(prior)
		case ent.IsNotFound(err):
			return r.recordConsent(ctx, w, req)
		default:
			return consentChange{}, err
		}
	})
	return change.skipped, err
}

func (r *Users) recordConsent(ctx context.Context, w consentWrite, req TermsConsent) (consentChange, error) {
	var skipped consent.MarketingSkipReason
	if req.Marketing {
		skipped = r.marketingSkipReason(w.owner)
	}
	if err := w.acceptTerms(ctx, req, skipped); err != nil {
		return consentChange{}, err
	}
	update := w.userUpdate().SetTermsVersion(req.TermsVersion)
	if !req.Marketing || skipped != "" {
		return consentChange{changed: true, skipped: skipped}, update.Exec(ctx)
	}
	choice := MarketingChoice{Status: consent.Granted, Source: req.Source, Locale: req.Locale}
	if err := r.applyMarketingChoice(ctx, w, choice); err != nil {
		return consentChange{}, err
	}
	return consentChange{changed: true}, w.projectMarketing(update, choice.Status).Exec(ctx)
}

func (r *Users) marketingSkipReason(owner *ent.User) consent.MarketingSkipReason {
	if len(owner.EmailEnc) == 0 {
		return consent.MarketingSkipNoEmail
	}
	if r.marketingMAC == nil {
		return consent.MarketingSkipUnavailable
	}
	return ""
}

func (w consentWrite) replayTerms(prior *ent.TermsAcceptance) (consentChange, error) {
	if prior.UserID != w.owner.ID || prior.AccountCreatedAt != w.owner.CreatedAt.UnixMicro() {
		return consentChange{}, ErrConsentRequestUsed
	}
	return consentChange{skipped: consent.MarketingSkipReason(prior.MarketingSkipped)}, nil
}

func (w consentWrite) acceptTerms(ctx context.Context, req TermsConsent, skipped consent.MarketingSkipReason) error {
	err := w.tx.TermsAcceptance.Create().
		SetUserID(w.owner.ID).SetAccountCreatedAt(w.owner.CreatedAt.UnixMicro()).
		SetTermsVersion(req.TermsVersion).SetAcceptedAt(w.at).
		SetSource(termsacceptance.Source(req.Source)).SetLocale(req.Locale).SetRequestID(req.RequestID).
		SetMarketingSkipped(string(skipped)).
		Exec(ctx)
	if ent.IsConstraintError(err) {
		return ErrConsentRequestUsed
	}
	return err
}

func (r *Users) SetMarketing(ctx context.Context, id uint64, choice MarketingChoice) error {
	if !choice.valid() {
		return ErrConsentInvalid
	}
	_, err := r.withConsentOwner(ctx, id, func(w consentWrite) (consentChange, error) {
		if err := r.applyMarketingChoice(ctx, w, choice); err != nil {
			return consentChange{}, err
		}
		return consentChange{changed: true}, w.projectMarketing(w.userUpdate(), choice.Status).Exec(ctx)
	})
	return err
}

func (r *Users) applyMarketingChoice(ctx context.Context, w consentWrite, choice MarketingChoice) error {
	if choice.Locale == "" {
		choice.Locale = w.owner.Locale
	}
	addressHMAC, err := r.marketingAddress(ctx, w, choice.Status)
	if err != nil {
		return err
	}
	record := marketingRecord{
		MarketingChoice: choice,
		policyVersion:   consent.MarketingPolicyVersion,
		textKey:         consent.MarketingTextKey,
		addressHMAC:     addressHMAC,
	}
	return w.appendMarketing(ctx, record)
}

func (r *Users) marketingAddress(ctx context.Context, w consentWrite, status consent.MarketingStatus) ([]byte, error) {
	if status == consent.Withdrawn && w.owner.MarketingStatus == user.MarketingStatusGranted {
		return w.grantedAddressHMAC(ctx)
	}
	if status == consent.Withdrawn && !r.canHashContactEmail(w.owner) {
		return nil, nil
	}
	email, err := r.openContactEmail(w.owner.ID, w.owner.EmailEnc)
	if err != nil {
		return nil, err
	}
	if r.marketingMAC == nil {
		return nil, ErrConsentMACUnavailable
	}
	return r.marketingMAC.ComputeMAC([]byte(email))
}

func (r *Users) canHashContactEmail(owner *ent.User) bool {
	return len(owner.EmailEnc) > 0 && r.marketingMAC != nil
}

func (w consentWrite) grantedAddressHMAC(ctx context.Context) ([]byte, error) {
	grant, err := w.latestMarketingGrant(ctx)
	if err != nil {
		return nil, err
	}
	return grant.AddressHmac, nil
}

func (w consentWrite) latestMarketingGrant(ctx context.Context) (*ent.MarketingConsent, error) {
	return w.tx.MarketingConsent.Query().
		Where(marketingconsent.UserIDEQ(w.owner.ID), marketingconsent.AccountCreatedAtEQ(w.owner.CreatedAt.UnixMicro()),
			marketingconsent.StatusEQ(marketingconsent.StatusGranted)).
		Order(ent.Desc(marketingconsent.FieldID)).
		First(ctx)
}

type marketingRecord struct {
	MarketingChoice
	policyVersion string
	textKey       string
	addressHMAC   []byte
}

func (w consentWrite) appendMarketing(ctx context.Context, record marketingRecord) error {
	return w.tx.MarketingConsent.Create().
		SetUserID(w.owner.ID).SetAccountCreatedAt(w.owner.CreatedAt.UnixMicro()).
		SetStatus(marketingconsent.Status(record.Status)).SetSource(marketingconsent.Source(record.Source)).
		SetPolicyVersion(record.policyVersion).SetConsentTextKey(record.textKey).
		SetLocale(record.Locale).SetAddressHmac(record.addressHMAC).SetCreatedAt(w.at).
		Exec(ctx)
}

func (w consentWrite) userUpdate() *ent.UserUpdateOne {
	return w.tx.User.UpdateOneID(w.owner.ID)
}

func (w consentWrite) projectMarketing(update *ent.UserUpdateOne, status consent.MarketingStatus) *ent.UserUpdateOne {
	return update.SetMarketingStatus(user.MarketingStatus(status)).SetMarketingStatusAt(w.at)
}

func (r *Users) withConsentOwner(ctx context.Context, id uint64, apply func(consentWrite) (consentChange, error)) (consentChange, error) {
	if err := validate.UserID(id); err != nil {
		return consentChange{}, err
	}
	change, err := db.WithQuery(ctx, func(ctx context.Context) (consentChange, error) {
		return db.RetryWithTxResult(ctx, r.client, func(tx *ent.Tx) (consentChange, error) {
			owner, err := tx.User.Query().Where(user.IDEQ(id), lockOwnerRow).Only(ctx)
			if err != nil {
				return consentChange{}, err
			}
			return apply(consentWrite{tx: tx, owner: owner, at: time.Now().UTC()})
		})
	})
	if err != nil || !change.changed {
		return change, err
	}
	r.Invalidate(id)
	return change, r.publishChanged(ctx, id)
}
