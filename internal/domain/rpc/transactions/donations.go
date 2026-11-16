// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package transactionsrpc

import (
	"time"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	VerbDonations    = "donations"
	DonationsSubject = AdminPremiumPrefix + "." + VerbDonations

	MaxDonationDays = 400
)

type DonationsRequest struct {
	ActorID string `json:"actor_id"`
	Days    int    `json:"days"`
}

type DonationTotal struct {
	Currency           string `json:"currency"`
	GrossCents         int64  `json:"gross_cents"`
	Payments           int    `json:"payments"`
	RecurringCents     int64  `json:"recurring_cents"`
	RecurringPayments  int    `json:"recurring_payments"`
	FirstSubscriptions int    `json:"first_subscriptions"`
	PreviousGrossCents int64  `json:"previous_gross_cents"`
	PreviousPayments   int    `json:"previous_payments"`
}

type DonationMonth struct {
	Month      string `json:"month"`
	Currency   string `json:"currency"`
	GrossCents int64  `json:"gross_cents"`
	Payments   int    `json:"payments"`
}

type DonationKind struct {
	Kind     string `json:"kind"`
	Payments int    `json:"payments"`
}

type DonationsReply struct {
	Start         time.Time       `json:"start"`
	End           time.Time       `json:"end"`
	Totals        []DonationTotal `json:"totals"`
	Months        []DonationMonth `json:"months"`
	Kinds         []DonationKind  `json:"kinds"`
	LastPaymentAt *time.Time      `json:"last_payment_at,omitempty"`
	rpc.Refusal
}
