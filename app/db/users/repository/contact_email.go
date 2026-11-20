// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/consent"
	domaincrypto "ItsBagelBot/internal/domain/crypto"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

var ErrNoContactEmail = errors.New("no contact email on record")

// Plaintext must never reach the database or logs.
func (r *Users) SetContactEmail(ctx context.Context, id uint64, email string) error {
	if err := validate.UserID(id); err != nil {
		return err
	}
	if err := validate.Email(email); err != nil {
		return err
	}

	sealed, err := r.packer.Pack([]byte(email), contactEmailAAD(id))
	if err != nil {
		return err
	}

	return r.withConsentOwner(ctx, id, func(w consentWrite) (bool, error) {
		withdrawn, err := r.withdrawChangedAddress(ctx, w, email)
		if err != nil {
			return false, err
		}
		update := w.userUpdate().SetEmailEnc(sealed.Ciphertext)
		if withdrawn {
			w.projectMarketing(update, consent.Withdrawn)
		}
		return withdrawn, update.Exec(ctx)
	})
}

func (r *Users) withdrawChangedAddress(ctx context.Context, w consentWrite, email string) (bool, error) {
	if w.owner.MarketingStatus != user.MarketingStatusGranted {
		return false, nil
	}
	grant, err := w.latestMarketingGrant(ctx)
	if err != nil {
		return false, err
	}
	if r.marketingMAC == nil {
		return false, ErrConsentMACUnavailable
	}
	if r.marketingMAC.VerifyMAC(grant.AddressHmac, []byte(email)) == nil {
		return false, nil
	}
	record := marketingRecord{
		MarketingChoice: MarketingChoice{Status: consent.Withdrawn, Source: consent.AddressChanged, Locale: grant.Locale},
		policyVersion:   grant.PolicyVersion,
		textKey:         grant.ConsentTextKey,
		addressHMAC:     grant.AddressHmac,
	}
	return true, w.appendMarketing(ctx, record)
}

func (r *Users) ContactEmail(ctx context.Context, id uint64) (string, error) {

	if err := validate.UserID(id); err != nil {
		return "", err
	}

	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().
			Where(user.IDEQ(id)).
			Select(user.FieldEmailEnc).
			Only(ctx)
	})
	if err != nil {
		return "", err
	}
	return r.openContactEmail(id, row.EmailEnc)
}

func (r *Users) openContactEmail(id uint64, ciphertext []byte) (string, error) {
	if len(ciphertext) == 0 {
		return "", ErrNoContactEmail
	}

	plain, err := r.packer.Unpack(domaincrypto.SecureEnvelope{
		Ciphertext:   ciphertext,
		AttachedData: contactEmailAAD(id),
	})
	if err != nil {
		return "", err
	}

	return string(plain), nil
}

func contactEmailAAD(userID uint64) []byte {

	aad := make([]byte, 0, 20+len("|contact_email"))

	aad = strconv.AppendUint(aad, userID, 10)
	aad = append(aad, "|contact_email"...)

	return aad
}
