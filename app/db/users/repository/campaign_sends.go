// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"errors"
	"fmt"

	"ItsBagelBot/app/db/users/ent"
	"ItsBagelBot/app/db/users/ent/marketingcampaign"
	"ItsBagelBot/app/db/users/ent/marketingsend"
	"ItsBagelBot/app/db/users/ent/user"
	"ItsBagelBot/internal/domain/consent"
	"ItsBagelBot/internal/domain/validate"
	"ItsBagelBot/pkg/db"
)

const maxResendIDLen = 64

var (
	ErrNotEligible = errors.New("recipient is not eligible for marketing email")
	ErrSendUnknown = errors.New("no marketing send matches the provider id")
)

type MarketingRecipient struct {
	UserID      uint64
	Locale      string
	Email       string
	AddressHMAC []byte
}

type SendTicket struct {
	ID     int
	Key    string
	Status consent.SendStatus
}

type SendClaim struct {
	CampaignID  int
	UserID      uint64
	AddressHMAC []byte
}

type SendOutcome struct {
	ID       int
	Status   consent.SendStatus
	ResendID string
}

func (r *Users) MarketingRecipient(ctx context.Context, userID uint64) (MarketingRecipient, error) {
	if err := validate.UserID(userID); err != nil {
		return MarketingRecipient{}, err
	}
	if r.marketingMAC == nil {
		return MarketingRecipient{}, ErrConsentMACUnavailable
	}
	owner, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.User, error) {
		return r.client.User.Query().Where(user.IDEQ(userID), user.MarketingStatusEQ(user.MarketingStatusGranted)).
			Select(user.FieldID, user.FieldLocale, user.FieldCreatedAt, user.FieldEmailEnc).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return MarketingRecipient{}, ErrNotEligible
	}
	if err != nil {
		return MarketingRecipient{}, err
	}
	candidates, err := r.audienceCandidates(ctx, []*ent.User{owner})
	if err != nil {
		return MarketingRecipient{}, err
	}
	suppressed, err := r.suppressedSet(ctx, candidates)
	if err != nil {
		return MarketingRecipient{}, err
	}
	if len(audienceRecipients(candidates, suppressed)) == 0 {
		return MarketingRecipient{}, ErrNotEligible
	}
	email, err := r.openContactEmail(owner.ID, owner.EmailEnc)
	if err != nil {
		return MarketingRecipient{}, err
	}
	return MarketingRecipient{UserID: owner.ID, Locale: owner.Locale, Email: email, AddressHMAC: candidates[0].hmacs[1]}, nil
}

func (r *Users) CampaignsRunnable(ctx context.Context, limit int) ([]int, error) {
	return db.WithQuery(ctx, func(ctx context.Context) ([]int, error) {
		return r.client.MarketingCampaign.Query().
			Where(marketingcampaign.StatusIn(campaignStatuses(consent.CampaignApproved, consent.CampaignSending)...)).
			Order(ent.Asc(marketingcampaign.FieldID)).Limit(limit).IDs(ctx)
	})
}

func (r *Users) AcquireCampaignLease(ctx context.Context, lease CampaignLease) (bool, error) {
	changed, err := db.WithQuery(ctx, func(ctx context.Context) (int, error) {
		return r.client.MarketingCampaign.Update().
			Where(
				marketingcampaign.IDEQ(lease.ID),
				marketingcampaign.StatusIn(campaignStatuses(consent.CampaignApproved, consent.CampaignSending)...),
				marketingcampaign.Or(
					marketingcampaign.LeaseUntilIsNil(),
					marketingcampaign.LeaseUntilLT(lease.Now),
					marketingcampaign.LeaseHolderEQ(lease.Holder),
				),
			).
			SetStatus(marketingcampaign.StatusSending).
			SetLeaseHolder(lease.Holder).SetLeaseUntil(lease.Now.Add(lease.TTL)).
			Save(ctx)
	})
	return changed > 0, err
}

func (r *Users) CampaignAdvance(ctx context.Context, id int, cursor uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.MarketingCampaign.UpdateOneID(id).SetAudienceCursor(cursor).Exec(ctx)
	})
}

func (r *Users) CampaignFinish(ctx context.Context, lease CampaignLease) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.MarketingCampaign.Update().
			Where(marketingcampaign.IDEQ(lease.ID), marketingcampaign.StatusEQ(marketingcampaign.StatusSending), marketingcampaign.LeaseHolderEQ(lease.Holder)).
			SetStatus(marketingcampaign.StatusDone).ClearLeaseHolder().ClearLeaseUntil().Save(ctx)
		return err
	})
}

func SendKey(campaignID int, userID uint64) string {
	return fmt.Sprintf("mkt-%d-%d", campaignID, userID)
}

func (r *Users) ClaimSend(ctx context.Context, claim SendClaim) (SendTicket, error) {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.MarketingSend, error) {
		return r.client.MarketingSend.Create().
			SetCampaignID(claim.CampaignID).SetUserID(claim.UserID).SetAddressHmac(claim.AddressHMAC).
			SetIdempotencyKey(SendKey(claim.CampaignID, claim.UserID)).Save(ctx)
	})
	if ent.IsConstraintError(err) {
		return r.existingSend(ctx, claim)
	}
	if err != nil {
		return SendTicket{}, err
	}
	return ticketFrom(row), nil
}

func ticketFrom(row *ent.MarketingSend) SendTicket {
	return SendTicket{ID: row.ID, Key: row.IdempotencyKey, Status: consent.SendStatus(row.Status)}
}

func (r *Users) existingSend(ctx context.Context, claim SendClaim) (SendTicket, error) {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.MarketingSend, error) {
		return r.client.MarketingSend.Query().
			Where(marketingsend.CampaignIDEQ(claim.CampaignID), marketingsend.UserIDEQ(claim.UserID)).Only(ctx)
	})
	if ent.IsNotFound(err) {
		return SendTicket{Status: consent.SendSkipped}, nil
	}
	if err != nil {
		return SendTicket{}, err
	}
	return ticketFrom(row), nil
}

func (r *Users) RecordAttempt(ctx context.Context, id int) (int, error) {
	row, err := db.WithQuery(ctx, func(ctx context.Context) (*ent.MarketingSend, error) {
		return r.client.MarketingSend.UpdateOneID(id).AddAttempts(1).Save(ctx)
	})
	if err != nil {
		return 0, err
	}
	return row.Attempts, nil
}

func (r *Users) SkipQueuedSend(ctx context.Context, campaignID int, userID uint64) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		_, err := r.client.MarketingSend.Update().
			Where(marketingsend.CampaignIDEQ(campaignID), marketingsend.UserIDEQ(userID), marketingsend.StatusEQ(marketingsend.StatusQueued)).
			SetStatus(marketingsend.StatusSkipped).Save(ctx)
		return err
	})
}

func (r *Users) RecordSend(ctx context.Context, outcome SendOutcome) error {
	return db.WithExec(ctx, func(ctx context.Context) error {
		update := r.client.MarketingSend.Update().
			Where(marketingsend.IDEQ(outcome.ID), marketingsend.StatusEQ(marketingsend.StatusQueued)).
			SetStatus(marketingsend.Status(outcome.Status))
		if outcome.ResendID != "" {
			update.SetResendID(outcome.ResendID)
		}
		_, err := update.Save(ctx)
		return err
	})
}

var sendRank = map[consent.SendStatus]int{
	consent.SendSent: 1, consent.SendDelivered: 2, consent.SendBounced: 3, consent.SendComplained: 4,
}

func validDelivery(resendID string, event consent.SendStatus) bool {
	if resendID == "" || len(resendID) > maxResendIDLen {
		return false
	}
	return sendRank[event] >= sendRank[consent.SendDelivered]
}

func (r *Users) RecordDelivery(ctx context.Context, resendID string, event consent.SendStatus) error {
	if !validDelivery(resendID, event) {
		return ErrConsentInvalid
	}
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.MarketingSend, error) {
		return r.client.MarketingSend.Query().Where(marketingsend.ResendIDEQ(resendID)).All(ctx)
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return ErrSendUnknown
	}
	for _, row := range rows {
		if sendRank[consent.SendStatus(row.Status)] >= sendRank[event] {
			continue
		}
		err := db.WithExec(ctx, func(ctx context.Context) error {
			return r.client.MarketingSend.UpdateOne(row).SetStatus(marketingsend.Status(event)).Exec(ctx)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *Users) sendCounts(ctx context.Context, campaignIDs ...int) (map[int]map[consent.SendStatus]int, error) {
	var rows []struct {
		CampaignID int    `json:"campaign_id"`
		Status     string `json:"status"`
		Count      int    `json:"count"`
	}
	err := db.WithExec(ctx, func(ctx context.Context) error {
		return r.client.MarketingSend.Query().Where(marketingsend.CampaignIDIn(campaignIDs...)).
			GroupBy(marketingsend.FieldCampaignID, marketingsend.FieldStatus).Aggregate(ent.Count()).Scan(ctx, &rows)
	})
	if err != nil {
		return nil, err
	}
	counts := make(map[int]map[consent.SendStatus]int, len(campaignIDs))
	for _, id := range campaignIDs {
		counts[id] = map[consent.SendStatus]int{}
	}
	for _, row := range rows {
		counts[row.CampaignID][consent.SendStatus(row.Status)] = row.Count
	}
	return counts, nil
}
