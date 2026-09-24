// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tebex

import (
	"strings"
	"time"

	"ItsBagelBot/pkg/codec"
)

type RecurringPayment struct {
	Reference             string
	Status                string
	Interval              string
	NextPaymentDate       *time.Time
	PausedUntil           *time.Time
	Cancelled             bool
	CancellationRequested bool
	CancellationDate      *time.Time
	Ambiguous             bool
}

type recurringWire struct {
	Reference string `json:"reference"`
	Interval  string `json:"interval"`
	Status    struct {
		Description string `json:"description"`
		Active      *int   `json:"active"`
	} `json:"status"`
	NextPaymentDate       codec.RawMessage `json:"next_payment_date"`
	PausedUntil           codec.RawMessage `json:"paused_until"`
	CancelledAt           codec.RawMessage `json:"cancelled_at"`
	CancellationRequested codec.RawMessage `json:"cancellation_requested_at"`
}

func (p *RecurringPayment) UnmarshalJSON(data []byte) error {
	var wire recurringWire
	if err := codec.Unmarshal(data, &wire); err != nil {
		return ErrCheckoutResponse
	}
	cancelled, cancelledKnown := cancellationPresence(wire.CancelledAt)
	requested, requestedKnown := cancellationPresence(wire.CancellationRequested)
	*p = RecurringPayment{
		Reference: wire.Reference, Status: wire.Status.Description, Interval: wire.Interval,
		NextPaymentDate: parseProviderTime(wire.NextPaymentDate), PausedUntil: parseProviderTime(wire.PausedUntil),
		Cancelled: cancelled || wire.Status.Description == "Cancelled", CancellationRequested: requested,
		CancellationDate: parseProviderTime(wire.CancellationRequested),
		Ambiguous:        !cancelledKnown || !requestedKnown,
	}
	return nil
}

func parseProviderTime(raw codec.RawMessage) *time.Time {
	var value string
	if codec.Unmarshal(raw, &value) != nil {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	utc := parsed.UTC()
	return &utc
}

func cancellationPresence(raw codec.RawMessage) (present, known bool) {
	if len(raw) == 0 {
		return false, false
	}
	if string(raw) == "null" {
		return false, true
	}
	var value string
	if codec.Unmarshal(raw, &value) != nil {
		return false, false
	}
	return strings.TrimSpace(value) != "", true
}

func normalizedReference(reference string) string {
	return strings.TrimPrefix(reference, "tbx-r-")
}

func (p RecurringPayment) MatchesReference(reference string) bool {
	return p.Reference != "" && normalizedReference(p.Reference) == normalizedReference(reference)
}

func (p RecurringPayment) HasCancellation() bool {
	return p.Cancelled || p.CancellationRequested || p.CancellationDate != nil
}

func (p RecurringPayment) CanProtect(reference string) bool {
	if p.Ambiguous || !p.MatchesReference(reference) {
		return false
	}
	if p.HasCancellation() {
		return false
	}
	if p.Interval != "P1M" || p.NextPaymentDate == nil {
		return false
	}
	return p.Status == "Active" || p.Status == "Paused"
}

func (p RecurringPayment) ProtectedThrough(reference string, until time.Time) bool {
	if until.IsZero() || !p.CanProtect(reference) {
		return false
	}
	if p.Status != "Paused" {
		return false
	}
	return p.PausedUntil != nil && !p.PausedUntil.Before(until)
}
