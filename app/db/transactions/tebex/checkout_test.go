// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tebex

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"ItsBagelBot/pkg/codec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func response(body string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}
}

func fixtureResponse(state string, next time.Time, until *time.Time) *http.Response {
	body, _ := codec.Marshal(map[string]any{
		"reference": "99", "status": map[string]any{"active": 1, "description": state},
		"interval": "P1M", "next_payment_date": next, "paused_until": until,
		"cancelled_at": nil, "cancellation_requested_at": nil,
	})
	return response(string(body))
}

func checkoutFixture(t *testing.T, transport roundTripFunc) *CheckoutClient {
	t.Helper()
	client, err := NewCheckoutClient(CheckoutConfig{
		ProjectID: "123", PrivateKey: "fixture-only-key", BaseURL: "https://checkout.test", EnableMutations: true,
		HTTPClient: &http.Client{Transport: transport},
	})
	require.NoError(t, err)
	return client
}

func TestCheckoutReadUsesBasicAuthAndActualStatusObject(t *testing.T) {
	next := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	client := checkoutFixture(t, func(r *http.Request) (*http.Response, error) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/recurring-payments/tbx-r-99", r.URL.Path)
		user, key, ok := r.BasicAuth()
		assert.True(t, ok)
		assert.Equal(t, "123", user)
		assert.True(t, key == "fixture-only-key", "unexpected fixture authentication")
		return fixtureResponse("Active", next, nil), nil
	})
	payment, err := client.GetRecurring(context.Background(), "tbx-r-99")
	require.NoError(t, err)
	assert.Equal(t, "Active", payment.Status)
	assert.Equal(t, "P1M", payment.Interval)
	require.NotNil(t, payment.NextPaymentDate)
	assert.Equal(t, time.UTC, payment.NextPaymentDate.Location())
	assert.True(t, payment.CanProtect("tbx-r-99"))
}

func TestCheckoutMutationGateMakesNoRequest(t *testing.T) {
	client := checkoutFixture(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("disabled mutations must not make a provider request")
		return nil, nil
	})
	client.cfg.EnableMutations = false
	_, err := client.PauseRecurring(context.Background(), "tbx-r-99", time.Now())
	require.ErrorIs(t, err, ErrMutationDisabled)
}

func TestCheckoutPauseUsesAbsoluteBoundaryAndReadBack(t *testing.T) {
	next := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	until := next.AddDate(0, 3, 0)
	paused, writes, reads := false, 0, 0
	client := checkoutFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			writes++
			assert.Equal(t, "/api/recurring-payments/tbx-r-99/status", r.URL.Path)
			data, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var intent PauseRequest
			require.NoError(t, codec.Unmarshal(data, &intent))
			assert.Equal(t, "Paused", intent.Status)
			assert.True(t, until.Equal(intent.PausedUntil))
			paused = true
			return fixtureResponse("Paused", next, &until), nil
		}
		reads++
		if paused {
			return fixtureResponse("Paused", next, &until), nil
		}
		return fixtureResponse("Active", next, nil), nil
	})
	payment, err := client.PauseRecurring(context.Background(), "tbx-r-99", until)
	require.NoError(t, err)
	assert.True(t, payment.ProtectedThrough("tbx-r-99", until))
	assert.Equal(t, 2, reads)
	assert.Equal(t, 1, writes)
	_, err = client.PauseRecurring(context.Background(), "tbx-r-99", until)
	require.NoError(t, err)
	assert.Equal(t, 1, writes, "retry must recognize the existing absolute pause")
}

func TestCheckoutRecoversLostPauseReplyByReading(t *testing.T) {
	next := time.Now().UTC().Add(14 * 24 * time.Hour).Truncate(time.Second)
	until := next.AddDate(0, 2, 0)
	paused := false
	client := checkoutFixture(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPut {
			paused = true
			return nil, errors.New("simulated lost reply with private-request-detail")
		}
		if paused {
			return fixtureResponse("Paused", next, &until), nil
		}
		return fixtureResponse("Active", next, nil), nil
	})
	payment, err := client.PauseRecurring(context.Background(), "tbx-r-99", until)
	require.NoError(t, err)
	assert.True(t, payment.ProtectedThrough("tbx-r-99", until))
}

func TestCheckoutRejectsCancelledAndNearRenewal(t *testing.T) {
	for _, body := range []string{
		`{"reference":"99","status":{"active":0,"description":"Cancelled"},"interval":"P1M","next_payment_date":"2030-09-20T14:00:00Z","cancelled_at":"2024-07-25 14:01:03","cancellation_requested_at":null}`,
		`{"reference":"99","status":{"active":1,"description":"Active"},"interval":"P1M","next_payment_date":"2020-09-20T14:00:00Z","cancelled_at":null,"cancellation_requested_at":null}`,
	} {
		client := checkoutFixture(t, func(r *http.Request) (*http.Response, error) {
			assert.Equal(t, http.MethodGet, r.Method)
			return response(body), nil
		})
		_, err := client.PauseRecurring(context.Background(), "tbx-r-99", time.Now().AddDate(0, 3, 0))
		require.ErrorIs(t, err, ErrUnsafeProtection)
	}
}

func TestCheckoutDoesNotExposeProviderErrorData(t *testing.T) {
	client := checkoutFixture(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("customer@example.com private-request-detail")
	})
	_, err := client.GetRecurring(context.Background(), "tbx-r-99")
	require.ErrorIs(t, err, ErrCheckoutUnknown)
	assert.NotContains(t, err.Error(), "customer@example.com")
	assert.NotContains(t, err.Error(), "private-request-detail")
}
