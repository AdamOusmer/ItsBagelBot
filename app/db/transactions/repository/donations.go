// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package repository

import (
	"context"
	"time"

	"ItsBagelBot/app/db/transactions/ent"
	"ItsBagelBot/app/db/transactions/ent/kofidonation"
	"ItsBagelBot/app/db/transactions/kofi"
	"ItsBagelBot/pkg/db"
)

type Donations struct {
	client *ent.Client
}

func NewDonations(client *ent.Client) *Donations {
	return &Donations{client: client}
}

func (r *Donations) RecordDonation(ctx context.Context, donation kofi.Donation) error {
	err := db.WithExec(ctx, func(ctx context.Context) error {
		create := r.client.KofiDonation.Create().
			SetID(donation.TransactionID).
			SetMessageID(donation.MessageID).
			SetKind(kofidonation.Kind(donation.Kind)).
			SetAmountCents(donation.AmountCents).
			SetCurrency(donation.Currency).
			SetSubscriptionPayment(donation.Subscription).
			SetFirstSubscriptionPayment(donation.FirstSubscription).
			SetOccurredAt(donation.OccurredAt)
		if donation.TierName != "" {
			create.SetTierName(donation.TierName)
		}
		return create.Exec(ctx)
	})
	if ent.IsConstraintError(err) {
		return nil
	}
	return err
}

func (r *Donations) DonationsBetween(ctx context.Context, from, to time.Time) ([]kofi.Donation, error) {
	rows, err := db.WithQuery(ctx, func(ctx context.Context) ([]*ent.KofiDonation, error) {
		return r.client.KofiDonation.Query().
			Where(kofidonation.OccurredAtGTE(from), kofidonation.OccurredAtLT(to)).
			All(ctx)
	})
	if err != nil {
		return nil, err
	}
	out := make([]kofi.Donation, 0, len(rows))
	for _, row := range rows {
		out = append(out, kofi.Donation{
			TransactionID:     row.ID,
			MessageID:         row.MessageID,
			Kind:              kofi.Kind(row.Kind),
			AmountCents:       row.AmountCents,
			Currency:          row.Currency,
			Subscription:      row.SubscriptionPayment,
			FirstSubscription: row.FirstSubscriptionPayment,
			TierName:          row.TierName,
			OccurredAt:        row.OccurredAt.UTC(),
		})
	}
	return out, nil
}
