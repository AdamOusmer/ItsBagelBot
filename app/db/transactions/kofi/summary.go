// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kofi

import (
	"cmp"
	"slices"
	"time"

	transactionsrpc "ItsBagelBot/internal/domain/rpc/transactions"
)

const (
	TrailingMonths = 12
	monthLayout    = "2006-01"
)

type Window struct {
	Start         time.Time
	End           time.Time
	PreviousStart time.Time
	MonthsStart   time.Time
}

func WindowFor(now time.Time, days int) Window {
	now = now.UTC()
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	start := end.AddDate(0, 0, -days)
	lastClosed := end.AddDate(0, 0, -1)
	months := time.Date(lastClosed.Year(), lastClosed.Month()-(TrailingMonths-1), 1, 0, 0, 0, 0, time.UTC)
	return Window{Start: start, End: end, PreviousStart: start.AddDate(0, 0, -days), MonthsStart: months}
}

func (w Window) From() time.Time {
	if w.MonthsStart.Before(w.PreviousStart) {
		return w.MonthsStart
	}
	return w.PreviousStart
}

type monthKey struct {
	month    string
	currency string
}

type summary struct {
	window Window
	totals map[string]*transactionsrpc.DonationTotal
	months map[monthKey]*transactionsrpc.DonationMonth
	kinds  map[Kind]int
	last   *time.Time
}

func Summarize(rows []Donation, w Window) transactionsrpc.DonationsReply {
	s := summary{
		window: w,
		totals: map[string]*transactionsrpc.DonationTotal{},
		months: map[monthKey]*transactionsrpc.DonationMonth{},
		kinds:  map[Kind]int{},
	}
	for _, row := range rows {
		s.add(row)
	}
	s.fillMonths()
	return transactionsrpc.DonationsReply{
		Start: w.Start, End: w.End, Totals: s.sortedTotals(), Months: s.sortedMonths(), Kinds: s.kindList(), LastPaymentAt: s.last,
	}
}

func within(at, from, to time.Time) bool {
	return !at.Before(from) && at.Before(to)
}

func (s *summary) add(row Donation) {
	if !within(row.OccurredAt, s.window.From(), s.window.End) {
		return
	}
	if within(row.OccurredAt, s.window.Start, s.window.End) {
		s.addCurrent(row)
	} else if within(row.OccurredAt, s.window.PreviousStart, s.window.Start) {
		s.total(row.Currency).PreviousGrossCents += row.AmountCents
		s.total(row.Currency).PreviousPayments++
	}
	if !row.OccurredAt.Before(s.window.MonthsStart) {
		s.addMonth(row)
	}
	if s.last == nil || row.OccurredAt.After(*s.last) {
		at := row.OccurredAt
		s.last = &at
	}
}

func (s *summary) addCurrent(row Donation) {
	total := s.total(row.Currency)
	total.GrossCents += row.AmountCents
	total.Payments++
	s.kinds[row.Kind]++
	if row.FirstSubscription {
		total.FirstSubscriptions++
	}
	if row.Subscription {
		total.RecurringCents += row.AmountCents
		total.RecurringPayments++
	}
}

func (s *summary) addMonth(row Donation) {
	month := s.month(monthKey{month: row.OccurredAt.Format(monthLayout), currency: row.Currency})
	month.GrossCents += row.AmountCents
	month.Payments++
}

func entry[K comparable, V any](entries map[K]*V, key K, fresh V) *V {
	value, ok := entries[key]
	if !ok {
		value = &fresh
		entries[key] = value
	}
	return value
}

func (s *summary) month(key monthKey) *transactionsrpc.DonationMonth {
	return entry(s.months, key, transactionsrpc.DonationMonth{Month: key.month, Currency: key.currency})
}

func (s *summary) fillMonths() {
	currencies := map[string]struct{}{}
	for key := range s.months {
		currencies[key.currency] = struct{}{}
	}
	for currency := range currencies {
		s.fillCurrency(currency)
	}
}

func (s *summary) fillCurrency(currency string) {
	for at := s.window.MonthsStart; at.Before(s.window.End); at = at.AddDate(0, 1, 0) {
		s.month(monthKey{month: at.Format(monthLayout), currency: currency})
	}
}

func (s *summary) total(currency string) *transactionsrpc.DonationTotal {
	return entry(s.totals, currency, transactionsrpc.DonationTotal{Currency: currency})
}

func sortedValues[K comparable, V any](entries map[K]*V, compare func(a, b V) int) []V {
	out := make([]V, 0, len(entries))
	for _, entry := range entries {
		out = append(out, *entry)
	}
	slices.SortFunc(out, compare)
	return out
}

func (s *summary) sortedTotals() []transactionsrpc.DonationTotal {
	return sortedValues(s.totals, func(a, b transactionsrpc.DonationTotal) int {
		return cmp.Or(cmp.Compare(b.GrossCents, a.GrossCents), cmp.Compare(a.Currency, b.Currency))
	})
}

func (s *summary) sortedMonths() []transactionsrpc.DonationMonth {
	return sortedValues(s.months, func(a, b transactionsrpc.DonationMonth) int {
		return cmp.Or(cmp.Compare(b.Month, a.Month), cmp.Compare(a.Currency, b.Currency))
	})
}

func (s *summary) kindList() []transactionsrpc.DonationKind {
	out := make([]transactionsrpc.DonationKind, 0, len(Kinds))
	for _, kind := range Kinds {
		out = append(out, transactionsrpc.DonationKind{Kind: string(kind), Payments: s.kinds[kind]})
	}
	return out
}
