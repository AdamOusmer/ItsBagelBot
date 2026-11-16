// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/kofi"

	"github.com/stretchr/testify/assert"
)

type donationLog struct {
	saved []string
	err   error
}

func (d *donationLog) RecordDonation(_ context.Context, donation kofi.Donation) error {
	d.saved = append(d.saved, donation.TransactionID)
	return d.err
}

const tok = "kofi-webhook-token"

func kofiBody(token, transactionID string) string {
	return kofiBodyAt(token, transactionID, time.Now())
}

func kofiBodyAt(token, transactionID string, at time.Time) string {
	data := `{"verification_token":"` + token + `","message_id":"m-1","timestamp":"` + at.UTC().Format(time.RFC3339) + `",` +
		`"type":"Tip","amount":"3.00","currency":"USD","kofi_transaction_id":"` + transactionID + `"}`
	return url.Values{"data": {data}}.Encode()
}

func TestKofiWebhook(t *testing.T) {
	for name, tc := range map[string]struct {
		token  kofi.Token
		body   string
		err    error
		status int
		saved  []string
	}{
		"records a verified donation": {token: tok, body: kofiBody(tok, "t-1"), status: http.StatusOK, saved: []string{"t-1"}},
		"rejects a wrong token":       {token: tok, body: kofiBody("bad", "t-1"), status: http.StatusUnauthorized},
		"refuses when unconfigured":   {body: kofiBody("", "t-1"), status: http.StatusServiceUnavailable},
		"refuses a short token":       {token: "short", body: kofiBody("short", "t-1"), status: http.StatusServiceUnavailable},
		"acks a replayed old event": {
			token: tok, body: kofiBodyAt(tok, "t-1", time.Now().Add(-kofi.MaxEventAge-time.Minute)), status: http.StatusOK,
		},
		"rejects a malformed payload": {token: tok, body: "data=%7B", status: http.StatusBadRequest},
		"rejects an invalid amount": {
			token: tok, body: strings.Replace(kofiBody(tok, "t-1"), "3.00", "3.001", 1), status: http.StatusBadRequest,
		},
		"acks an unsupported type": {
			token: tok, body: strings.Replace(kofiBody(tok, "t-1"), "Tip", "Refund", 1), status: http.StatusOK,
		},
		"rejects an oversized body": {token: tok, body: "data=" + strings.Repeat("a", maxBodyBytes), status: http.StatusRequestEntityTooLarge},
		"acks the settings test":    {token: tok, body: kofiBody(tok, "00000000-1111-2222-3333-444444444444"), status: http.StatusOK},
		"asks for a retry on save failure": {
			token: tok, body: kofiBody(tok, "t-2"), err: errors.New("db down"), status: http.StatusInternalServerError, saved: []string{"t-2"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			ledger := &donationLog{err: tc.err}
			app := New(&fakeStore{}, Config{KofiToken: tc.token, Donations: ledger}, nil)
			req := httptest.NewRequest(http.MethodPost, "/webhooks/kofi", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.saved, ledger.saved)
		})
	}
}
