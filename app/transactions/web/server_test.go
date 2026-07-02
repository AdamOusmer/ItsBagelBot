package web

import (
	"context"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"

	"ItsBagelBot/app/transactions/repository"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	records []recordablePayment
	events  []repository.WebhookEvent
}

func (f *fakeStore) Record(_ context.Context, id string, userID uint64) error {
	f.records = append(f.records, recordablePayment{TransactionID: id, UserID: userID})
	return nil
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
	assert.Empty(t, store.records)
}

func TestWebhookAliasesExposeReachability(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)

	for _, path := range []string{"/tebex", "/tebex/", "/webhooks/tebex", "/webhooks/tebex/"} {
		req, err := http.NewRequest(http.MethodGet, path, nil)
		require.NoError(t, err)

		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode, path)
	}
	assert.Empty(t, store.records)
	assert.Empty(t, store.events)
}

func TestPaymentCompletedRecordsTransactionAndStoresProcessedState(t *testing.T) {

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
	require.Len(t, store.records, 1)
	assert.Equal(t, recordablePayment{TransactionID: "tbx-1234", UserID: 1001}, store.records[0])
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
	assert.Empty(t, store.records)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookFailed, store.events[0].Status)
	assert.Equal(t, "tbx-1234", store.events[0].TransactionID)
	assert.Contains(t, store.events[0].Error, "user id")
}

func TestUnsupportedActionableWebhookStoresFailedState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-refund","type":"payment.refunded","date":"2026-07-02T00:00:00+00:00","subject":{}}`

	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
	assert.Empty(t, store.records)
	require.Len(t, store.events, 1)
	assert.Equal(t, repository.WebhookFailed, store.events[0].Status)
	assert.Contains(t, store.events[0].Error, "not implemented")
}

func TestBadSignatureIsRejectedBeforeStoringState(t *testing.T) {

	store := &fakeStore{}
	app := newTestApp(store)
	body := `{"id":"evt-bad-sig","type":"validation.webhook","date":"2026-07-02T00:00:00+00:00","subject":{}}`

	resp := doWebhook(t, app, body, testSecret, false)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Empty(t, store.records)
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
	require.Len(t, store.records, 1)
	assert.Equal(t, recordablePayment{TransactionID: "tbx-renewal", UserID: 1001}, store.records[0])
}

const testSecret = "webhook-secret"

func newTestApp(store *fakeStore) *fiber.App {
	return New(store, Config{WebhookSecret: testSecret}, nil)
}

func doWebhook(t *testing.T, app *fiber.App, body string, secret string, validSignature bool) *http.Response {

	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/webhooks/tebex", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if validSignature {
		req.Header.Set("X-Signature", hex.EncodeToString(tebexSignature([]byte(body), secret)))
	} else {
		req.Header.Set("X-Signature", strings.Repeat("0", 64))
	}

	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp
}

func TestGiftedPaymentNotifiesRecipientOnce(t *testing.T) {

	store := &fakeStore{}
	var notices []GiftNotice
	app := New(store, Config{
		WebhookSecret: testSecret,
		NotifyGift: func(_ context.Context, n GiftNotice) error {
			notices = append(notices, n)
			return nil
		},
	}, nil)

	body := `{"id":"evt-gift","type":"payment.completed","subject":{"transaction_id":"tbx-gift-1","custom":{"user_id":"111","username":"recipient","gifted_by":"804932984","gifted_by_login":"mavey"}}}`
	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.records, 1)
	assert.Equal(t, uint64(111), store.records[0].UserID)
	require.Len(t, notices, 1)
	assert.Equal(t, GiftNotice{
		WebhookID:     "evt-gift",
		RecipientID:   111,
		GiftedByID:    804932984,
		GiftedByLogin: "mavey",
	}, notices[0])
}

func TestGiftNotificationSkippedOnRenewalAndSelfPurchase(t *testing.T) {

	store := &fakeStore{}
	var notices []GiftNotice
	app := New(store, Config{
		WebhookSecret: testSecret,
		NotifyGift: func(_ context.Context, n GiftNotice) error {
			notices = append(notices, n)
			return nil
		},
	}, nil)

	// Renewal of a gifted subscription: entitlement recorded, no ping.
	renewal := `{"id":"evt-renew","type":"recurring-payment.renewed","subject":{"reference":"sub-1","last_payment":{"transaction_id":"tbx-gift-2","custom":{"user_id":"111","gifted_by":"804932984","gifted_by_login":"mavey"}}}}`
	resp := doWebhook(t, app, renewal, testSecret, true)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Self-purchase (no gifted_by): no ping.
	self := `{"id":"evt-self","type":"payment.completed","subject":{"transaction_id":"tbx-3","custom":{"user_id":"222"}}}`
	resp = doWebhook(t, app, self, testSecret, true)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	// Basket "gifted" to its own buyer collapses to a plain purchase: no ping.
	selfGift := `{"id":"evt-selfgift","type":"payment.completed","subject":{"transaction_id":"tbx-4","custom":{"user_id":"333","gifted_by":"333","gifted_by_login":"me"}}}`
	resp = doWebhook(t, app, selfGift, testSecret, true)
	resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)

	assert.Len(t, store.records, 3)
	assert.Empty(t, notices)
}

func TestGiftNotificationFailureDoesNotFailWebhook(t *testing.T) {

	store := &fakeStore{}
	app := New(store, Config{
		WebhookSecret: testSecret,
		NotifyGift: func(_ context.Context, _ GiftNotice) error {
			return context.DeadlineExceeded
		},
	}, nil)

	body := `{"id":"evt-gift-fail","type":"payment.completed","subject":{"transaction_id":"tbx-5","custom":{"user_id":"111","gifted_by":"804932984","gifted_by_login":"mavey"}}}`
	resp := doWebhook(t, app, body, testSecret, true)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	require.Len(t, store.records, 1)
}
