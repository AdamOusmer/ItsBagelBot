package web

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ItsBagelBot/app/transactions/repository"
	billingrpc "ItsBagelBot/internal/domain/rpc/billing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	events  []repository.WebhookEvent
	changes []billingrpc.ApplyRequest
}

func (f *fakeStore) SaveWebhookEvent(_ context.Context, event repository.WebhookEvent) error {
	f.events = append(f.events, event)
	return nil
}

func TestValidationWebhookEchoesIDAndStoresState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-validation","type":"validation.webhook","date":"2026-07-02T00:00:00+00:00","subject":{}}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"id":"evt-validation"}`, string(payload))
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookValidation, store.events[0].Status)
	assert.Empty(t, store.changes)
}

func TestWebhookAliasesExposeReachability(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)

	for _, path := range []string{"/tebex", "/tebex/", "/webhooks/tebex", "/webhooks/tebex/"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)

		resp := rec.Result()
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
	assert.Empty(t, store.changes)
	assert.Empty(t, store.events)
}

func TestPaymentCompletedActivatesAndStoresProcessedState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{
		"id":"evt-payment",
		"type":"payment.completed",
		"date":"2026-07-02T00:00:00+00:00",
		"subject":{
			"transaction_id":"tbx-1234",
			"custom":{"user_id":"1001"},
			"products":[]
		}
	}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.changes, 1)
	assert.Equal(t, billingrpc.ActionActivate, store.changes[0].Action)
	assert.Equal(t, uint64(1001), store.changes[0].UserID)
	// No expiry on the payment subject (one-time purchase): the activation
	// must still carry one, defaulted to a month after the event.
	require.NotNil(t, store.changes[0].ExpiresAt)
	assert.Equal(t, "2026-08-02", store.changes[0].ExpiresAt.UTC().Format("2006-01-02"))
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookProcessed, store.events[0].Status)
	assert.Equal(t, "tbx-1234", store.events[0].TransactionID)
	assert.Equal(t, uint64(1001), store.events[0].UserID)
}

func TestPaymentCompletedWithoutUserIDStoresFailedState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{
		"id":"evt-missing-user",
		"type":"payment.completed",
		"date":"2026-07-02T00:00:00+00:00",
		"subject":{"transaction_id":"tbx-1234","products":[]}
	}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnprocessableEntity, resp.StatusCode)
	assert.Empty(t, store.changes)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookFailed, store.events[0].Status)
	assert.Equal(t, "tbx-1234", store.events[0].TransactionID)
	assert.Contains(t, store.events[0].Error, "user id")
}

func TestRefundRevokesEntitlementAndStoresProcessedState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-refund","type":"payment.refunded","date":"2026-07-02T00:00:00+00:00","subject":{"transaction_id":"tbx-refund","custom":{"user_id":"1001"}}}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookProcessed, store.events[0].Status)
	require.Len(t, store.changes, 1)
	assert.Equal(t, billingrpc.ActionRevoke, store.changes[0].Action)
}

func TestBadSignatureIsRejectedBeforeStoringState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-bad-sig","type":"validation.webhook","date":"2026-07-02T00:00:00+00:00","subject":{}}`

	resp := doWebhook(t, app, body, testSecret, false)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Empty(t, store.changes)
	assert.Empty(t, store.events)
}

func TestRecurringRenewedUsesLastPayment(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{
		"id":"evt-renewed",
		"type":"recurring-payment.renewed",
		"date":"2026-07-02T00:00:00+00:00",
		"subject":{
			"reference":"tbx-r-1234",
			"last_payment":{
				"transaction_id":"tbx-renewal",
				"custom":{"broadcaster_user_id":1001}
			}
		}
	}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.changes, 1)
	assert.Equal(t, billingrpc.ActionActivate, store.changes[0].Action)
	assert.Equal(t, uint64(1001), store.changes[0].UserID)
	require.Len(t, store.events, 1)
	assert.Equal(t, "tbx-renewal", store.events[0].TransactionID)
}

func TestTrialStartedActivatesUntilTrialEnd(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{
		"id":"evt-trial-start",
		"type":"recurring-payment.trial.started",
		"date":"2026-07-02T00:00:00+00:00",
		"subject":{
			"reference":"tbx-r-trial",
			"next_payment_at":"2026-07-16T00:00:00+00:00",
			"initial_payment":{
				"transaction_id":"tbx-trial",
				"custom":{"user_id":"1001"}
			}
		}
	}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.changes, 1)
	assert.Equal(t, billingrpc.ActionActivate, store.changes[0].Action)
	assert.Equal(t, uint64(1001), store.changes[0].UserID)
	require.NotNil(t, store.changes[0].ExpiresAt)
	assert.Equal(t, "2026-07-16", store.changes[0].ExpiresAt.UTC().Format("2006-01-02"))
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookProcessed, store.events[0].Status)
}

func TestTrialCancelledMarksCancelPending(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{
		"id":"evt-trial-cancel",
		"type":"recurring-payment.trial.cancelled",
		"date":"2026-07-02T00:00:00+00:00",
		"subject":{
			"reference":"tbx-r-trial",
			"next_payment_at":"2026-07-16T00:00:00+00:00",
			"initial_payment":{
				"transaction_id":"tbx-trial",
				"custom":{"user_id":"1001"}
			}
		}
	}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.changes, 1)
	assert.Equal(t, billingrpc.ActionCancelRequested, store.changes[0].Action)
}

func TestTrialEndedIsAuditedWithoutEntitlementChange(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-trial-end","type":"recurring-payment.trial.ended","date":"2026-07-02T00:00:00+00:00","subject":{"reference":"tbx-r-trial"}}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, store.changes)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookIgnored, store.events[0].Status)
}

// A trial subject can arrive without any payment (nothing has been charged
// yet). Redelivery cannot fix attribution, so the webhook must be audited and
// acknowledged instead of erroring into a Tebex retry loop.
func TestTrialStartedWithoutPaymentIsAcknowledged(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-trial-bare","type":"recurring-payment.trial.started","date":"2026-07-02T00:00:00+00:00","subject":{"reference":"tbx-r-trial","next_payment_at":"2026-07-16T00:00:00+00:00"}}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Empty(t, store.changes)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookIgnored, store.events[0].Status)
	assert.Contains(t, store.events[0].Error, "no recordable")
}

// Informational types the store is subscribed to but that change no
// entitlement must be audited and acknowledged.
func TestInformationalEventsAreAuditedAsIgnored(t *testing.T) {

	for _, eventType := range []string{"payment.declined", "payment.dispute.closed", "recurring-payment.status.changed"} {
		store := &fakeStore{}
		app := newTestApp(store)
		body := `{"id":"evt-info","type":"` + eventType + `","date":"2026-07-02T00:00:00+00:00","subject":{"transaction_id":"tbx-1234","custom":{"user_id":"1001"}}}`

		resp := doWebhook(t, app, body, testSecret, true)
		resp.Body.Close()

		assert.Equal(t, http.StatusNoContent, resp.StatusCode, eventType)
		assert.Empty(t, store.changes, eventType)
		require.Len(t, store.events, 1, eventType)
		assert.Equal(t, repository.WebhookIgnored, store.events[0].Status, eventType)
	}
}

const testSecret = "webhook-secret"

func newTestApp(store *fakeStore) http.Handler {
	return New(store, Config{WebhookSecret: testSecret, ApplyBilling: applyFor(store)}, nil)
}

func applyFor(store *fakeStore) func(context.Context, billingrpc.ApplyRequest) error {
	return func(_ context.Context, req billingrpc.ApplyRequest) error {
		store.changes = append(store.changes, req)
		return nil
	}
}

func doWebhook(t *testing.T, app http.Handler, body string, secret string, validSignature bool) *http.Response {

	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tebex", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if validSignature {
		req.Header.Set("X-Signature", hex.EncodeToString(tebexSignature([]byte(body), secret)))
	} else {
		req.Header.Set("X-Signature", strings.Repeat("0", 64))
	}

	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec.Result()
}

func TestGiftedPaymentNotifiesRecipientOnce(t *testing.T) {

	store := &fakeStore{}
	var notices []GiftNotice
	app := New(store, Config{
		WebhookSecret: testSecret,
		ApplyBilling:  applyFor(store),
		NotifyGift: func(_ context.Context, n GiftNotice) error {
			notices = append(notices, n)
			return nil
		},
	}, nil)

	body := `{"id":"evt-gift","type":"payment.completed","date":"2026-07-02T00:00:00Z","subject":{"transaction_id":"tbx-gift-1","custom":{"user_id":"111","username":"recipient","gifted_by":"804932984","gifted_by_login":"mavey","gift_message":"happy streaming!"}}}`
	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.changes, 1)
	assert.Equal(t, uint64(111), store.changes[0].UserID)
	require.Len(t, notices, 1)
	assert.Equal(t, GiftNotice{
		WebhookID:     "evt-gift",
		RecipientID:   111,
		GiftedByID:    804932984,
		GiftedByLogin: "mavey",
		GiftMessage:   "happy streaming!",
	}, notices[0])
}

func TestGiftNotificationSkippedOnRenewalAndSelfPurchase(t *testing.T) {

	store := &fakeStore{}
	var notices []GiftNotice
	app := New(store, Config{
		WebhookSecret: testSecret,
		ApplyBilling:  applyFor(store),
		NotifyGift: func(_ context.Context, n GiftNotice) error {
			notices = append(notices, n)
			return nil
		},
	}, nil)

	// Renewal of a gifted subscription: entitlement recorded, no ping.
	renewal := `{"id":"evt-renew","type":"recurring-payment.renewed","date":"2026-07-02T00:00:00Z","subject":{"reference":"sub-1","last_payment":{"transaction_id":"tbx-gift-2","custom":{"user_id":"111","gifted_by":"804932984","gifted_by_login":"mavey"}}}}`
	resp := doWebhook(t, app, renewal, testSecret, true)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Self-purchase (no gifted_by): no ping.
	self := `{"id":"evt-self","type":"payment.completed","date":"2026-07-02T00:01:00Z","subject":{"transaction_id":"tbx-3","custom":{"user_id":"222"}}}`
	resp = doWebhook(t, app, self, testSecret, true)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Basket "gifted" to its own buyer collapses to a plain purchase: no ping.
	selfGift := `{"id":"evt-selfgift","type":"payment.completed","date":"2026-07-02T00:02:00Z","subject":{"transaction_id":"tbx-4","custom":{"user_id":"333","gifted_by":"333","gifted_by_login":"me"}}}`
	resp = doWebhook(t, app, selfGift, testSecret, true)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	assert.Len(t, store.changes, 3)
	assert.Empty(t, notices)
}

func TestGiftNotificationFailureDoesNotFailWebhook(t *testing.T) {

	store := &fakeStore{}
	app := New(store, Config{
		WebhookSecret: testSecret,
		ApplyBilling:  applyFor(store),
		NotifyGift: func(_ context.Context, _ GiftNotice) error {
			return context.DeadlineExceeded
		},
	}, nil)

	body := `{"id":"evt-gift-fail","type":"payment.completed","date":"2026-07-02T00:00:00Z","subject":{"transaction_id":"tbx-5","custom":{"user_id":"111","gifted_by":"804932984","gifted_by_login":"mavey"}}}`
	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.changes, 1)
}
