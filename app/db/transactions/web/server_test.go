// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package web

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/app/db/transactions/repository"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testSecret = "webhook-secret"

type fakeStore struct {
	events    []repository.WebhookEvent
	changes   []billingrpc.ApplyRequest
	incidents []BillingIncident
}

func (f *fakeStore) SaveWebhookEvent(_ context.Context, event repository.WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func newTestApp(store *fakeStore) http.Handler {
	return New(store, Config{WebhookSecret: testSecret, ApplyBilling: applyFor(store)}, nil)
}

func applyFor(store *fakeStore) func(context.Context, billingrpc.ApplyRequest) error {
	return func(_ context.Context, req billingrpc.ApplyRequest) error {
		store.changes = append(store.changes, req)
		return nil
	}
}

func doWebhook(t *testing.T, app http.Handler, body string, validSignature bool) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tebex", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if validSignature {
		req.Header.Set("X-Signature", hex.EncodeToString(tebexSignature([]byte(body), testSecret)))
	} else {
		req.Header.Set("X-Signature", strings.Repeat("0", 64))
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec.Result()
}

func postWebhook(t *testing.T, app http.Handler, body string) int {
	t.Helper()
	resp := doWebhook(t, app, body, true)
	defer resp.Body.Close()
	return resp.StatusCode
}

type appliedChange struct {
	Action    billingrpc.Action
	UserID    uint64
	Expires   string
	Reference string
}

type recordedEvent struct {
	Status        repository.WebhookStatus
	TransactionID string
	UserID        uint64
}

func appliedChanges(changes []billingrpc.ApplyRequest) []appliedChange {
	var out []appliedChange
	for _, change := range changes {
		applied := appliedChange{Action: change.Action, UserID: change.UserID, Reference: change.RecurringReference}
		if change.ExpiresAt != nil {
			applied.Expires = change.ExpiresAt.UTC().Format("2006-01-02")
		}
		out = append(out, applied)
	}
	return out
}

func recordedEvents(events []repository.WebhookEvent) []recordedEvent {
	var out []recordedEvent
	for _, event := range events {
		out = append(out, recordedEvent{event.Status, event.TransactionID, event.UserID})
	}
	return out
}

func TestWebhookRoutesAnswerReachabilityProbes(t *testing.T) {
	store := &fakeStore{}
	app := newTestApp(store)
	for _, path := range []string{"/tebex", "/tebex/", "/webhooks/tebex", "/webhooks/tebex/"} {
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		assert.Equal(t, http.StatusOK, rec.Code, path)
	}
	assert.Empty(t, store.changes)
	assert.Empty(t, store.events)
}

type webhookCase struct {
	name         string
	body         string
	badSignature bool
	wantStatus   int
	wantBody     string
	wantChanges  []appliedChange
	wantEvents   []recordedEvent
	wantError    string
}

const subjectCustom = `"custom":{"user_id":"1001"}`

func webhookEvent(kind, subject string) string {
	return `{"id":"evt-1","type":"` + kind + `","date":"2026-07-02T00:00:00+00:00","subject":` + subject + `}`
}

func processedEvent(txn string) []recordedEvent {
	return []recordedEvent{{repository.WebhookProcessed, txn, 1001}}
}

var ignoredEvent = []recordedEvent{{Status: repository.WebhookIgnored}}

func paymentWebhookCases() []webhookCase {
	return []webhookCase{
		{
			name:       "echoes a validation webhook and stores it",
			body:       webhookEvent("validation.webhook", `{}`),
			wantStatus: http.StatusOK, wantBody: `{"id":"evt-1"}`,
			wantEvents: []recordedEvent{{Status: repository.WebhookValidation}},
		},
		{
			name:       "rejects a bad signature before storing anything",
			body:       webhookEvent("validation.webhook", `{}`),
			wantStatus: http.StatusUnauthorized, badSignature: true,
		},
		{
			name:        "activates premium for a completed payment with a bounded expiry",
			body:        webhookEvent("payment.completed", `{"transaction_id":"tbx-1234",`+subjectCustom+`,"products":[]}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionActivate, UserID: 1001, Expires: "2026-08-02"}},
			wantEvents:  processedEvent("tbx-1234"),
		},
		{
			name:        "keeps an explicit product expiry",
			body:        webhookEvent("payment.completed", `{"transaction_id":"tbx-1234",`+subjectCustom+`,"products":[{"expires_at":"2027-01-01T00:00:00+00:00"}]}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionActivate, UserID: 1001, Expires: "2027-01-01"}},
			wantEvents:  processedEvent("tbx-1234"),
		},
		{
			name:       "stores a failed event when the payment has no user id",
			body:       webhookEvent("payment.completed", `{"transaction_id":"tbx-1234","products":[]}`),
			wantStatus: http.StatusUnprocessableEntity,
			wantEvents: []recordedEvent{{Status: repository.WebhookFailed, TransactionID: "tbx-1234"}},
			wantError:  "user id",
		},
		{
			name:        "revokes the entitlement on a refund",
			body:        webhookEvent("payment.refunded", `{"transaction_id":"tbx-1234",`+subjectCustom+`}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionRevoke, UserID: 1001}},
			wantEvents:  processedEvent("tbx-1234"),
		},
	}
}

func recurringWebhookCases() []webhookCase {
	return []webhookCase{
		{
			name:        "bounds the expiry of a cancellation without a next payment",
			body:        webhookEvent("recurring-payment.cancellation.requested", `{"reference":"tbx-r-1234","initial_payment":{"transaction_id":"tbx-init",`+subjectCustom+`}}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionCancelRequested, UserID: 1001, Expires: "2026-08-02", Reference: "tbx-r-1234"}},
			wantEvents:  processedEvent("tbx-init"),
		},
		{
			name:        "reads a renewal from its last payment",
			body:        webhookEvent("recurring-payment.renewed", `{"reference":"tbx-r-1234","last_payment":{"transaction_id":"tbx-renewal","custom":{"broadcaster_user_id":1001}}}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionActivate, UserID: 1001, Expires: "2026-08-02", Reference: "tbx-r-1234"}},
			wantEvents:  processedEvent("tbx-renewal"),
		},
		{
			name:        "activates a trial until the trial ends",
			body:        webhookEvent("recurring-payment.trial.started", `{"reference":"tbx-r-trial","next_payment_at":"2026-07-16T00:00:00+00:00","initial_payment":{"transaction_id":"tbx-trial",`+subjectCustom+`}}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionActivate, UserID: 1001, Expires: "2026-07-16", Reference: "tbx-r-trial"}},
			wantEvents:  processedEvent("tbx-trial"),
		},
		{
			name:        "marks a cancelled trial as cancel pending",
			body:        webhookEvent("recurring-payment.trial.cancelled", `{"reference":"tbx-r-trial","next_payment_at":"2026-07-16T00:00:00+00:00","initial_payment":{"transaction_id":"tbx-trial",`+subjectCustom+`}}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionCancelRequested, UserID: 1001, Expires: "2026-07-16", Reference: "tbx-r-trial"}},
			wantEvents:  processedEvent("tbx-trial"),
		},
	}
}

func auditedWebhookCases() []webhookCase {
	return []webhookCase{
		{
			name:       "audits a trial end without changing the entitlement",
			body:       webhookEvent("recurring-payment.trial.ended", `{"reference":"tbx-r-trial"}`),
			wantStatus: http.StatusNoContent, wantEvents: ignoredEvent,
		},
		{
			name:       "acknowledges a trial start that has no payment",
			body:       webhookEvent("recurring-payment.trial.started", `{"reference":"tbx-r-trial","next_payment_at":"2026-07-16T00:00:00+00:00"}`),
			wantStatus: http.StatusNoContent, wantEvents: ignoredEvent, wantError: "no recordable",
		},
		{
			name:       "audits a declined payment as informational",
			body:       webhookEvent("payment.declined", `{"transaction_id":"tbx-1234",`+subjectCustom+`}`),
			wantStatus: http.StatusNoContent, wantEvents: []recordedEvent{{Status: repository.WebhookIgnored}},
		},
		{
			name:       "audits a closed dispute as informational",
			body:       webhookEvent("payment.dispute.closed", `{"transaction_id":"tbx-1234",`+subjectCustom+`}`),
			wantStatus: http.StatusNoContent, wantEvents: ignoredEvent,
		},
		{
			name:       "audits a provider status change as informational",
			body:       webhookEvent("recurring-payment.status.changed", `{"transaction_id":"tbx-1234",`+subjectCustom+`}`),
			wantStatus: http.StatusNoContent, wantEvents: ignoredEvent,
		},
		{
			name:        "maps a declined renewal to a payment failure",
			body:        webhookEvent("payment.declined", `{"transaction_id":"tbx-1","recurring_payment_reference":"tbx-r-9",`+subjectCustom+`}`),
			wantStatus:  http.StatusNoContent,
			wantChanges: []appliedChange{{Action: billingrpc.ActionPaymentFailed, UserID: 1001, Reference: "tbx-r-9"}},
			wantEvents:  processedEvent("tbx-1"),
		},
		{
			name:       "ignores a declined one-off payment",
			body:       webhookEvent("payment.declined", `{"transaction_id":"tbx-2",`+subjectCustom+`}`),
			wantStatus: http.StatusNoContent, wantEvents: []recordedEvent{{repository.WebhookIgnored, "", 0}},
		},
		{
			name:       "ignores a declined renewal without a user",
			body:       webhookEvent("payment.declined", `{"transaction_id":"tbx-3","recurring_payment_reference":"tbx-r-9"}`),
			wantStatus: http.StatusNoContent, wantEvents: []recordedEvent{{repository.WebhookIgnored, "", 0}},
		},
	}
}

func webhookCases() []webhookCase {
	return slices.Concat(paymentWebhookCases(), recurringWebhookCases(), auditedWebhookCases())
}

func TestWebhookProcessing(t *testing.T) {
	for _, tc := range webhookCases() {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			resp := doWebhook(t, newTestApp(store), tc.body, !tc.badSignature)
			defer resp.Body.Close()

			assert.Equal(t, tc.wantStatus, resp.StatusCode)
			if tc.wantBody != "" {
				payload, err := io.ReadAll(resp.Body)
				require.NoError(t, err)
				assert.JSONEq(t, tc.wantBody, string(payload))
			}
			assert.Equal(t, tc.wantChanges, appliedChanges(store.changes))
			assert.Equal(t, tc.wantEvents, recordedEvents(store.events))
			if tc.wantError != "" {
				assert.Contains(t, store.events[0].Error, tc.wantError)
			}
		})
	}
}

func TestDisputeWonOnOneTimePurchaseCarriesBoundedExpiry(t *testing.T) {
	store := &fakeStore{}
	app := newTestApp(store)

	assert.Equal(t, http.StatusNoContent, postWebhook(t, app, `{"id":"evt-dispute-open","type":"payment.dispute.opened","date":"2026-07-02T00:00:00+00:00","subject":{"transaction_id":"tbx-1234","custom":{"user_id":"1001"}}}`))
	assert.Equal(t, http.StatusNoContent, postWebhook(t, app, `{"id":"evt-dispute-won","type":"payment.dispute.won","date":"2026-07-05T00:00:00+00:00","subject":{"transaction_id":"tbx-1234","custom":{"user_id":"1001"},"products":[]}}`))

	assert.Equal(t, []appliedChange{
		{Action: billingrpc.ActionRevoke, UserID: 1001},
		{Action: billingrpc.ActionCancelAborted, UserID: 1001, Expires: "2026-08-05"},
	}, appliedChanges(store.changes), "a settled one-time payment must not reinstate premium with no expiry")
}

func TestBillingIncidentReceivesAllowlistedSummary(t *testing.T) {
	store := &fakeStore{}
	app := New(store, Config{
		WebhookSecret: testSecret,
		ApplyBilling:  applyFor(store),
		RecordBillingIncident: func(_ context.Context, incident BillingIncident) error {
			store.incidents = append(store.incidents, incident)
			return nil
		},
	}, nil)
	body := `{"id":"evt-incident","type":"payment.completed","date":"2026-07-02T00:00:00Z","subject":{"transaction_id":"tbx-incident","custom":{"user_id":"1001"}}}`

	resp := doWebhook(t, app, body, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.incidents, 1)
	assert.Equal(t, BillingIncident{EventID: "evt-incident", EventType: "payment.completed", Action: string(billingrpc.ActionActivate), TransactionID: "tbx-incident", UserID: 1001, OccurredAt: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)}, store.incidents[0])
}

func TestBillingIncidentFailureRetriesWebhook(t *testing.T) {
	store := &fakeStore{}
	app := New(store, Config{
		WebhookSecret: testSecret,
		ApplyBilling:  applyFor(store),
		RecordBillingIncident: func(context.Context, BillingIncident) error {
			return errors.New("incident store unavailable")
		},
	}, nil)
	body := `{"id":"evt-incident-fail","type":"payment.completed","date":"2026-07-02T00:00:00Z","subject":{"transaction_id":"tbx-incident-fail","custom":{"user_id":"1001"}}}`

	resp := doWebhook(t, app, body, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookFailed, store.events[0].Status)
	assert.Len(t, store.changes, 1, "the paid entitlement is applied before incident reconciliation")
}

func giftCase(id, kind, subject string) string {
	return `{"id":"` + id + `","type":"` + kind + `","date":"2026-07-02T00:00:00Z","subject":` + subject + `}`
}

func TestGiftNotifications(t *testing.T) {
	gift := giftCase("evt-gift", "payment.completed", `{"transaction_id":"tbx-gift-1","custom":{"user_id":"111","username":"recipient","gifted_by":"804932984","gifted_by_login":"mavey","gift_message":"happy streaming!"}}`)
	renewal := giftCase("evt-renew", "recurring-payment.renewed", `{"reference":"sub-1","last_payment":{"transaction_id":"tbx-gift-2","custom":{"user_id":"111","gifted_by":"804932984","gifted_by_login":"mavey"}}}`)
	self := giftCase("evt-self", "payment.completed", `{"transaction_id":"tbx-3","custom":{"user_id":"222"}}`)
	selfGift := giftCase("evt-selfgift", "payment.completed", `{"transaction_id":"tbx-4","custom":{"user_id":"333","gifted_by":"333","gifted_by_login":"me"}}`)
	failing := giftCase("evt-gift-fail", "payment.completed", `{"transaction_id":"tbx-5","custom":{"user_id":"111","gifted_by":"804932984","gifted_by_login":"mavey"}}`)
	for _, tc := range []struct {
		name        string
		bodies      []string
		notifyErr   error
		wantChanges int
		wantNotices []GiftNotice
	}{
		{
			name: "notifies the recipient once", bodies: []string{gift}, wantChanges: 1,
			wantNotices: []GiftNotice{{WebhookID: "evt-gift", RecipientID: 111, GiftedByID: 804932984, GiftedByLogin: "mavey", GiftMessage: "happy streaming!"}},
		},
		{name: "skips renewals and self purchases", bodies: []string{renewal, self, selfGift}, wantChanges: 3},
		{name: "does not fail the webhook when the notification fails", bodies: []string{failing}, notifyErr: context.DeadlineExceeded, wantChanges: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			var notices []GiftNotice
			app := New(store, Config{
				WebhookSecret: testSecret,
				ApplyBilling:  applyFor(store),
				NotifyGift: func(_ context.Context, n GiftNotice) error {
					if tc.notifyErr == nil {
						notices = append(notices, n)
					}
					return tc.notifyErr
				},
			}, nil)

			for _, body := range tc.bodies {
				assert.Equal(t, http.StatusNoContent, postWebhook(t, app, body))
			}

			assert.Len(t, store.changes, tc.wantChanges)
			assert.Equal(t, tc.wantNotices, notices)
		})
	}
}
