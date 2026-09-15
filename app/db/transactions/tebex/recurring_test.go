// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tebex

import (
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCancellationIntentSurvivesTimezoneLessDates(t *testing.T) {
	var payment RecurringPayment
	err := codec.Unmarshal([]byte(`{"reference":"99","status":{"active":1,"description":"Paused"},"interval":"P1M","next_payment_date":"2030-09-20T14:00:00Z","paused_until":"2030-12-20T14:00:00Z","cancelled_at":null,"cancellation_requested_at":"2024-07-25 14:01:03"}`), &payment)
	require.NoError(t, err)
	assert.True(t, payment.CancellationRequested)
	assert.Nil(t, payment.CancellationDate, "a timezone-less value cannot prove an absolute date")
	assert.False(t, payment.CanProtect("tbx-r-99"))
}

func TestUnknownCancellationAndDateRemainUnverified(t *testing.T) {
	var payment RecurringPayment
	err := codec.Unmarshal([]byte(`{"reference":"99","status":{"active":1,"description":"Active"},"interval":"P1M","next_payment_date":"2030-09-20T14:00:00"}`), &payment)
	require.NoError(t, err)
	assert.True(t, payment.Ambiguous)
	assert.Nil(t, payment.NextPaymentDate)
	assert.False(t, payment.CanProtect("tbx-r-99"))
}

func TestProtectionRequiresFullIntervalIdentityAndPausedState(t *testing.T) {
	next := time.Date(2030, 9, 20, 14, 0, 0, 0, time.UTC)
	until := next.AddDate(0, 3, 0)
	payment := RecurringPayment{Reference: "99", Status: "Paused", Interval: "P1M", NextPaymentDate: &next, PausedUntil: &until}
	assert.True(t, payment.ProtectedThrough("tbx-r-99", until))
	assert.False(t, payment.ProtectedThrough("tbx-r-100", until))
	assert.False(t, payment.ProtectedThrough("tbx-r-99", until.Add(time.Second)))
	payment.Status = "Active"
	assert.False(t, payment.ProtectedThrough("tbx-r-99", until))
	payment.Status, payment.Interval = "Paused", "P1Y"
	assert.False(t, payment.ProtectedThrough("tbx-r-99", until))
}
