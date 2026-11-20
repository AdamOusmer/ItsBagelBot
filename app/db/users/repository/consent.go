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

func (c TermsConsent) valid() bool {
	if c.Source != consent.Welcome && c.Source != consent.Consent {
		return false
	}
	if strings.TrimSpace(c.TermsVersion) == "" || strings.TrimSpace(c.RequestID) == "" {
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

func (r *Users) SetConsent(ctx context.Context, id uint64, req TermsConsent) error {
	if !req.valid() {
		return ErrConsentInvalid
	}
	return r.withConsentOwner(ctx, id, func(w consentWrite) (bool, error) {
		replayed, err := w.acceptTerms(ctx, req)
		if err != nil || replayed {
			return false, err
		}
		update := w.userUpdate().SetTermsVersion(req.TermsVersion)
		if err := r.applyTermsOptIn(ctx, w, update, req); err != nil {
			return false, err
		}
		return true, update.Exec(ctx)
	})
}

func (r *Users) applyTermsOptIn(ctx context.Context, w consentWrite, update *ent.UserUpdateOne, req TermsConsent) error {
	if !req.Marketing {
		return nil
	}
	w.projectMarketing(update, consent.Granted)
	return r.applyMarketingChoice(ctx, w, MarketingChoice{Status: consent.Granted, Source: req.Source, Locale: req.Locale})
}

func (w consentWrite) acceptTerms(ctx context.Context, req TermsConsent) (bool, error) {
	prior, err := w.tx.TermsAcceptance.Query().Where(termsacceptance.RequestIDEQ(req.RequestID)).Only(ctx)
	switch {
	case err == nil:
		if prior.UserID != w.owner.ID || prior.AccountCreatedAt != w.owner.CreatedAt.UnixMicro() {
			return false, ErrConsentRequestUsed
		}
		return true, nil
	case !ent.IsNotFound(err):
		return false, err
	}
	err = w.tx.TermsAcceptance.Create().
		SetUserID(w.owner.ID).SetAccountCreatedAt(w.owner.CreatedAt.UnixMicro()).
		SetTermsVersion(req.TermsVersion).SetAcceptedAt(w.at).
		SetSource(termsacceptance.Source(req.Source)).SetLocale(req.Locale).SetRequestID(req.RequestID).
		Exec(ctx)
	if ent.IsConstraintError(err) {
		return false, ErrConsentRequestUsed
	}
	return false, err
}

func (r *Users) SetMarketing(ctx context.Context, id uint64, choice MarketingChoice) error {
	if !choice.valid() {
		return ErrConsentInvalid
	}
	return r.withConsentOwner(ctx, id, func(w consentWrite) (bool, error) {
		if err := r.applyMarketingChoice(ctx, w, choice); err != nil {
			return false, err
		}
		return true, w.projectMarketing(w.userUpdate(), choice.Status).Exec(ctx)
	})
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

func (r *Users) withConsentOwner(ctx context.Context, id uint64, apply func(consentWrite) (bool, error)) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	changed, err := db.WithQuery(ctx, func(ctx context.Context) (bool, error) {
		return db.RetryWithTxResult(ctx, r.client, func(tx *ent.Tx) (bool, error) {
			owner, err := tx.User.Query().Where(user.IDEQ(id), lockOwnerRow).Only(ctx)
			if err != nil {
				return false, err
			}
			return apply(consentWrite{tx: tx, owner: owner, at: time.Now().UTC()})
		})
	})
	if err != nil || !changed {
		return err
	}
	r.Invalidate(id)
	return r.publishChanged(ctx, id)
}
