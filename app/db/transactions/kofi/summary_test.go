// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kofi_test

import (
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/kofi"
	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"

	"github.com/stretchr/testify/assert"
)

func day(month time.Month, d, hour int) time.Time {
	return time.Date(2026, month, d, hour, 0, 0, 0, time.UTC)
}

func TestWindowForUsesClosedUTCDays(t *testing.T) {
	assert.Equal(t, kofi.Window{
		Start: day(time.November, 9, 0), End: day(time.November, 16, 0),
		PreviousStart: day(time.November, 2, 0), MonthsStart: day(time.December, 1, 0).AddDate(-1, 0, 0),
	}, kofi.WindowFor(time.Date(2026, 11, 16, 18, 30, 0, 0, time.FixedZone("x", -5*3600)), 7))
}

func TestSummarizeSplitsWindowsCurrenciesAndMonths(t *testing.T) {
	window := kofi.WindowFor(day(time.November, 16, 12), 7)
	rows := []kofi.Donation{
		{Kind: kofi.KindDonation, AmountCents: 500, Currency: "USD", OccurredAt: day(time.November, 10, 1)},
		{Kind: kofi.KindSubscription, AmountCents: 300, Currency: "USD", Subscription: true, FirstSubscription: true, OccurredAt: day(time.November, 15, 23)},
		{Kind: kofi.KindDonation, AmountCents: 1000, Currency: "EUR", OccurredAt: day(time.November, 12, 4)},
		{Kind: kofi.KindDonation, AmountCents: 200, Currency: "USD", OccurredAt: day(time.November, 3, 4)},
		{Kind: kofi.KindShopOrder, AmountCents: 900, Currency: "USD", OccurredAt: day(time.October, 1, 4)},
		{Kind: kofi.KindDonation, AmountCents: 7700, Currency: "USD", OccurredAt: day(time.November, 16, 1)},
	}
	last := day(time.November, 15, 23)
	reply := kofi.Summarize(rows, window)
	months := reply.Months
	reply.Months = nil
	assert.Equal(t, transactionsrpc.DonationsReply{
		Start: window.Start, End: window.End,
		Totals: []transactionsrpc.DonationTotal{
			{Currency: "EUR", GrossCents: 1000, Payments: 1},
			{Currency: "USD", GrossCents: 800, Payments: 2, RecurringCents: 300, RecurringPayments: 1, FirstSubscriptions: 1, PreviousGrossCents: 200, PreviousPayments: 1},
		},
		Kinds: []transactionsrpc.DonationKind{
			{Kind: "donation", Payments: 2}, {Kind: "subscription", Payments: 1}, {Kind: "commission"}, {Kind: "shop_order"},
		},
		LastPaymentAt: &last,
	}, reply)
	assert.Equal(t, monthShape{
		rows: 2 * kofi.TrailingMonths, newest: "2026-11", oldest: "2025-12",
		paid: []transactionsrpc.DonationMonth{
			{Month: "2026-11", Currency: "EUR", GrossCents: 1000, Payments: 1},
			{Month: "2026-11", Currency: "USD", GrossCents: 1000, Payments: 3},
			{Month: "2026-10", Currency: "USD", GrossCents: 900, Payments: 1},
		},
	}, shapeOf(months))
}

type monthShape struct {
	rows           int
	newest, oldest string
	paid           []transactionsrpc.DonationMonth
}

func shapeOf(months []transactionsrpc.DonationMonth) monthShape {
	shape := monthShape{rows: len(months), newest: months[0].Month, oldest: months[len(months)-1].Month}
	for _, month := range months {
		if month.Payments > 0 {
			shape.paid = append(shape.paid, month)
		}
	}
	return shape
}

func TestSummarizeEmptyLedger(t *testing.T) {
	window := kofi.WindowFor(day(time.November, 16, 12), 30)
	reply := kofi.Summarize(nil, window)
	assert.Equal(t, [3]int{0, 0, len(kofi.Kinds)}, [3]int{len(reply.Totals), len(reply.Months), len(reply.Kinds)})
	assert.Nil(t, reply.LastPaymentAt)
}
