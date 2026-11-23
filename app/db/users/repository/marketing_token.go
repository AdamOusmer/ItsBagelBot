// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

const (
	unsubscribeTokenVersion = "v1"
	unsubscribeTokenScope   = "marketing"
	unsubscribeTokenParts   = 5
	unsubscribeTokenMaxLen  = 256
)

var ErrMarketingTokenInvalid = errors.New("invalid marketing token")

type MarketingTokenState struct {
	UserID uint64
	Status consent.MarketingStatus
	Locale string
}

type unsubscribeClaim struct {
	userID           uint64
	accountCreatedAt int64
}

func (r *Users) UnsubscribeToken(ctx context.Context, userID uint64) (string, error) {
	if err := validate.UserID(userID); err != nil {
		return "", err
	}
	if r.marketingMAC == nil {
		return "", ErrConsentMACUnavailable
	}
	owner, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(userID)).Select(user.FieldCreatedAt).Only(ctx)
	})
	if err != nil {
		return "", err
	}
	payload := strings.Join([]string{
		unsubscribeTokenVersion, strconv.FormatUint(userID, 10),
		strconv.FormatInt(owner.CreatedAt.UnixMicro(), 10), unsubscribeTokenScope,
	}, ".")
	tag, err := r.marketingMAC.ComputeMAC([]byte(payload))
	if err != nil {
		return "", err
	}
	return payload + "." + base64.RawURLEncoding.EncodeToString(tag), nil
}

func (r *Users) parseUnsubscribeToken(token string) (unsubscribeClaim, error) {
	if r.marketingMAC == nil {
		return unsubscribeClaim{}, ErrConsentMACUnavailable
	}
	token = strings.TrimSpace(token)
	split := strings.LastIndexByte(token, '.')
	if split < 0 || len(token) > unsubscribeTokenMaxLen {
		return unsubscribeClaim{}, ErrMarketingTokenInvalid
	}
	tag, err := base64.RawURLEncoding.DecodeString(token[split+1:])
	if err != nil || r.marketingMAC.VerifyMAC(tag, []byte(token[:split])) != nil {
		return unsubscribeClaim{}, ErrMarketingTokenInvalid
	}
	return claimFromPayload(token[:split])
}

func claimFromPayload(payload string) (unsubscribeClaim, error) {
	parts := strings.Split(payload, ".")
	if len(parts) != unsubscribeTokenParts-1 || parts[0] != unsubscribeTokenVersion || parts[3] != unsubscribeTokenScope {
		return unsubscribeClaim{}, ErrMarketingTokenInvalid
	}
	id, idErr := strconv.ParseUint(parts[1], 10, 64)
	created, createdErr := strconv.ParseInt(parts[2], 10, 64)
	if err := errors.Join(idErr, createdErr); err != nil {
		return unsubscribeClaim{}, ErrMarketingTokenInvalid
	}
	return unsubscribeClaim{userID: id, accountCreatedAt: created}, nil
}

func (r *Users) MarketingTokenState(ctx context.Context, token string) (MarketingTokenState, error) {
	claim, err := r.parseUnsubscribeToken(token)
	if err != nil {
		return MarketingTokenState{}, err
	}
	owner, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(claim.userID)).
			Select(user.FieldID, user.FieldCreatedAt, user.FieldMarketingStatus, user.FieldLocale).Only(ctx)
	})
	if ent.IsNotFound(err) || (err == nil && owner.CreatedAt.UnixMicro() != claim.accountCreatedAt) {
		return MarketingTokenState{}, ErrMarketingTokenInvalid
	}
	if err != nil {
		return MarketingTokenState{}, err
	}
	return MarketingTokenState{UserID: owner.ID, Status: consent.MarketingStatus(owner.MarketingStatus), Locale: owner.Locale}, nil
}

func (c MarketingChoice) validForToken() bool {
	if c.Status != consent.Granted && c.Status != consent.Withdrawn {
		return false
	}
	return c.Source == consent.OneClick || c.Source == consent.PreferencePage
}

func (r *Users) SetMarketingByToken(ctx context.Context, token string, choice MarketingChoice) error {
	if !choice.validForToken() {
		return ErrConsentInvalid
	}
	claim, err := r.parseUnsubscribeToken(token)
	if err != nil {
		return err
	}
	_, err = r.withConsentOwner(ctx, claim.userID, func(w consentWrite) (consentChange, error) {
		if w.owner.CreatedAt.UnixMicro() != claim.accountCreatedAt {
			return consentChange{}, ErrMarketingTokenInvalid
		}
		return r.applyTokenChoice(ctx, w, choice)
	})
	if ent.IsNotFound(err) {
		return ErrMarketingTokenInvalid
	}
	return err
}

func (r *Users) applyTokenChoice(ctx context.Context, w consentWrite, choice MarketingChoice) (consentChange, error) {
	if string(choice.Status) != string(w.owner.MarketingStatus) {
		return r.recordMarketing(ctx, w, choice)
	}
	if choice.Status != consent.Withdrawn {
		return consentChange{}, nil
	}
	addressHMAC, err := r.marketingAddress(ctx, w, choice.Status)
	if err != nil || len(addressHMAC) == 0 {
		return consentChange{}, err
	}
	suppressed, err := w.suppress(ctx, addressHMAC, consent.SuppressUnsubscribe)
	return consentChange{suppressed: suppressed}, err
}
