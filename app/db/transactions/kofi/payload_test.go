// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package kofi_test

import (
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/kofi"
	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const token kofi.Token = "kofi-token"

func payload(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	fields := map[string]any{
		"verification_token": string(token), "message_id": "3a1fac0c-f960-4506-a60e-824979a74e74",
		"timestamp": "2026-11-02T13:04:30Z", "type": "Subscription", "is_public": true, "from_name": "Jo",
		"message": "Good luck", "amount": "5.5", "url": "https://ko-fi.com/home/coffeeshop?txid=1",
		"email": "jo@example.com", "currency": "usd", "is_subscription_payment": true,
		"is_first_subscription_payment": true, "kofi_transaction_id": "1234-1234-1234-1234",
		"shop_items": nil, "tier_name": "Bagel tier", "shipping": nil,
	}
	for key, value := range overrides {
		fields[key] = value
	}
	data, err := codec.Marshal(fields)
	require.NoError(t, err)
	return data
}

func TestParseKeepsOnlyLedgerFields(t *testing.T) {
	donation, err := kofi.Parse(payload(t, nil), token)
	require.NoError(t, err)
	assert.Equal(t, kofi.Donation{
		TransactionID: "1234-1234-1234-1234", MessageID: "3a1fac0c-f960-4506-a60e-824979a74e74",
		Kind: kofi.KindSubscription, AmountCents: 550, Currency: "USD", Subscription: true, FirstSubscription: true,
		TierName: "Bagel tier", OccurredAt: time.Date(2026, 11, 2, 13, 4, 30, 0, time.UTC),
	}, donation)
}

func TestParseAmounts(t *testing.T) {
	for amount, cents := range map[string]int64{"3": 300, "3.00": 300, "12.07": 1207, "1000": 100000, "0.5": 50} {
		donation, err := kofi.Parse(payload(t, map[string]any{"amount": amount}), token)
		require.NoError(t, err, amount)
		assert.Equal(t, cents, donation.AmountCents, amount)
	}
}

func TestParseRejects(t *testing.T) {
	for name, tc := range map[string]struct {
		overrides map[string]any
		token     kofi.Token
		want      error
		reason    string
	}{
		"wrong token":       {overrides: map[string]any{"verification_token": "nope"}, token: token, want: kofi.ErrToken},
		"unconfigured":      {token: "", want: kofi.ErrToken},
		"unknown type":      {overrides: map[string]any{"type": "Refund"}, token: token, want: kofi.ErrUnsupported},
		"three decimals":    {overrides: map[string]any{"amount": "1.005"}, token: token, want: kofi.ErrPayload, reason: "amount"},
		"negative amount":   {overrides: map[string]any{"amount": "-3.00"}, token: token, want: kofi.ErrPayload, reason: "amount"},
		"grouped amount":    {overrides: map[string]any{"amount": "1,000.00"}, token: token, want: kofi.ErrPayload, reason: "amount"},
		"numeric amount":    {overrides: map[string]any{"amount": 3}, token: token, want: kofi.ErrMalformed},
		"bad currency":      {overrides: map[string]any{"currency": "US"}, token: token, want: kofi.ErrPayload, reason: "currency"},
		"bad timestamp":     {overrides: map[string]any{"timestamp": "yesterday"}, token: token, want: kofi.ErrPayload, reason: "timestamp"},
		"missing txn id":    {overrides: map[string]any{"kofi_transaction_id": ""}, token: token, want: kofi.ErrPayload, reason: "kofi_transaction_id"},
		"missing message":   {overrides: map[string]any{"message_id": nil}, token: token, want: kofi.ErrPayload, reason: "message_id"},
		"oversized txn id":  {overrides: map[string]any{"kofi_transaction_id": strings.Repeat("a", 65)}, token: token, want: kofi.ErrPayload, reason: "kofi_transaction_id"},
		"txn id with slash": {overrides: map[string]any{"kofi_transaction_id": "a/b"}, token: token, want: kofi.ErrPayload, reason: "kofi_transaction_id"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := kofi.Parse(payload(t, tc.overrides), tc.token)
			assert.ErrorIs(t, err, tc.want)
			assert.ErrorContains(t, err, tc.reason)
		})
	}
}

func TestParseRejectsMalformedJSON(t *testing.T) {
	_, err := kofi.Parse([]byte("{"), token)
	assert.ErrorIs(t, err, kofi.ErrMalformed)
}

func TestParseFlagsSettingsPageTest(t *testing.T) {
	donation, err := kofi.Parse(payload(t, map[string]any{"kofi_transaction_id": "00000000-1111-2222-3333-444444444444"}), token)
	require.NoError(t, err)
	assert.True(t, donation.Test())
}
